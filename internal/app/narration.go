package app

import (
	"context"
	"sync"

	"squid-os/internal/voice"
)

// Narrator wires spoken output into the assistant stream: one speech group
// per assistant message, sentence-chunked deltas fed to the voice
// coordinator while tokens are still generating.
//
// Group lifecycle is tied to the MESSAGE, not to the tool/turn context:
// UIStreamState.reset() between tool phases must not end the group, and
// already-emitted chunks keep playing during tool calls (the server has no
// pause concept). Voice mute never cancels the model stream or tool
// execution — the coordinator uses its own contexts, independent of the
// turn context.
type Narrator struct {
	coord *voice.Coordinator

	mu          sync.Mutex
	groupID     int // current message's group; 0 = none active
	lastGroupID int // last group started, survives endMessage while its tail drains
	chunker     *voice.SentenceChunker

	// Alert state (task 3.3): alertActive is the recursion guard — true while
	// any alert group is in flight; alertGids tracks which groups are alerts
	// so the drain hook can clear the flag when the last one finishes.
	alertActive bool
	alertGids   map[int]struct{}
}

// newNarrator builds a Narrator over the given coordinator. A nil coord
// yields an inert narrator (all methods no-op) so callers need no guards.
func newNarrator(coord *voice.Coordinator) *Narrator {
	n := &Narrator{coord: coord, alertGids: map[int]struct{}{}}
	if coord != nil {
		// Track group drain so cancelActive can keep silencing the audible
		// tail after endMessage (EndGroup closes admission, not playback).
		// The hook fires from the coordinator's lock-free path and re-enters
		// n.mu from the asynchronous notification worker.
		coord.SetGroupDrainedHook(n.onAnyGroupDrained)
	}
	return n
}

// onAnyGroupDrained dispatches the coordinator's per-group drain callback to
// the narration-tail tracker and the alert-guard tracker.
func (n *Narrator) onAnyGroupDrained(gid int) {
	n.onGroupDrained(gid)
	n.onAlertDrained(gid)
}

// onGroupDrained is the coordinator's per-group drain callback: once a group
// stops being current (tail drained or killed), forget it if it is still the
// tracked last group. Group identity guards against stale callbacks clearing
// a newer group's tracking (same pattern as the coordinator's own guards).
//
// Hooks are delivered asynchronously outside the coordinator lock. Taking
// n.mu is safe and ensures the exactly-once drain notification is not lost.
func (n *Narrator) onGroupDrained(gid int) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.lastGroupID == gid {
		n.lastGroupID = 0
	}
}

// beginMessage allocates a fresh speech group at assistant message start.
// Any in-flight group from a previous message is ended first (its buffer is
// flushed so nothing is lost). No-op when voice is disabled.
func (n *Narrator) beginMessage(enabled bool) {
	if n == nil || n.coord == nil || !enabled {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.groupID != 0 {
		n.endLocked(context.Background())
	}
	n.groupID = n.coord.StartGroup()
	// The previous message's draining tail (if any) is superseded: it keeps
	// playing but is no longer the cancellable "active" group. A drained one
	// was already cleared by the drain hook; this assignment covers both.
	n.lastGroupID = n.groupID
	gid := n.groupID // capture by value: the timer goroutine must not read n.groupID live (it is written under mu elsewhere)
	n.chunker = voice.NewNarrationChunker()
	// Hold-timer expiry emits the held fragment from the timer goroutine;
	// Emit is safe from any goroutine.
	n.chunker.ArmHoldTimer(func(text string) {
		n.emit(gid, text)
	})
}

// feedDelta appends one rendered text delta to the chunker and emits every
// completed chunk to the coordinator as they become ready, without waiting for full generation. Muted groups drop chunks
// silently inside the coordinator; the producer keeps feeding (chunker
// state stays consistent) and stops once the group ends.
func (n *Narrator) feedDelta(delta string) {
	if n == nil || n.coord == nil || delta == "" {
		return
	}
	n.mu.Lock()
	gid := n.groupID
	c := n.chunker
	n.mu.Unlock()
	if gid == 0 || c == nil {
		return
	}
	for _, chunk := range c.Feed(delta) {
		n.emit(gid, chunk)
	}
}

// endMessage flushes the remaining buffer and closes the group. It is the
// terminal signal for the CURRENT message only; it must NOT be called on
// UIStreamState.reset() between tool phases (that reset happens mid-message
// and would truncate the utterance). Idempotent.
func (n *Narrator) endMessage(ctx context.Context) {
	if n == nil || n.coord == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.endLocked(ctx)
}

// endLocked ends the active group: final flush + EndGroup. Caller holds mu.
func (n *Narrator) endLocked(ctx context.Context) {
	if n.groupID == 0 {
		return
	}
	gid := n.groupID
	c := n.chunker
	n.groupID = 0
	n.chunker = nil
	if c != nil {
		if rest := c.Flush(); rest != "" {
			n.emit(gid, rest)
		}
	}
	n.coord.EndGroup(gid)
}

// cancelActive kills the in-flight group without flushing its buffer
// (stream aborted / session switch / shutdown). The coordinator mutes the
// group so late-arriving chunks are dropped. Does not touch the model
// stream or tool worker. Returns true when a playing or draining group was
// cancelled, false when nothing was playing.
//
// It targets the allocated message group even before audio starts, then falls
// back to lastGroupID after endMessage while already-emitted chunks drain.
// The draining-tail identity check prevents cancellation of a newer group.
func (n *Narrator) cancelActive() bool {
	if n == nil || n.coord == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	// Cancel an allocated group even before its first chunk is emitted.
	// Current() is still zero during the chunker's hold period.
	gid := n.groupID
	if gid == 0 {
		gid = n.lastGroupID
		if gid == 0 || n.coord.Current() != gid {
			n.lastGroupID = 0
			return false
		}
	}
	n.groupID = 0
	n.lastGroupID = 0
	if n.chunker != nil {
		n.chunker.Cancel()
		n.chunker = nil
	}
	n.coord.CancelGroup(gid)
	return true
}

// emit admits one chunk of group gid without waiting for playback.
// Playback failures are handled asynchronously as best-effort voice work;
// they never surface as a TUI error or interrupt the model stream.
func (n *Narrator) emit(gid int, text string) {
	if n == nil || n.coord == nil || gid == 0 || text == "" {
		return
	}
	_ = n.coord.Emit(context.Background(), gid, text)
}

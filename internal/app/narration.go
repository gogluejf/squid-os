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

	mu      sync.Mutex
	groupID int    // current message's group; 0 = none active
	chunker *voice.SentenceChunker
}

// newNarrator builds a Narrator over the given coordinator. A nil coord
// yields an inert narrator (all methods no-op) so callers need no guards.
func newNarrator(coord *voice.Coordinator) *Narrator {
	return &Narrator{coord: coord}
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
	gid := n.groupID // capture by value: the timer goroutine must not read n.groupID live (it is written under mu elsewhere)
	n.chunker = voice.NewSentenceChunker(0, 0, 0, nil)
	// Hold-timer expiry emits the held fragment from the timer goroutine;
	// Emit is safe from any goroutine.
	n.chunker.ArmHoldTimer(func(text string) {
		n.emit(gid, text)
	})
}

// feedDelta appends one rendered text delta to the chunker and emits every
// completed chunk to the coordinator immediately — first audio arrives ~2s
// into the response, not after full generation. Muted groups drop chunks
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
// stream or tool worker.
func (n *Narrator) cancelActive() {
	if n == nil || n.coord == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.groupID == 0 {
		return
	}
	gid := n.groupID
	n.groupID = 0
	if n.chunker != nil {
		n.chunker.Cancel()
		n.chunker = nil
	}
	n.coord.CancelGroup(gid)
}

// emit speaks one chunk of group gid through the coordinator. Speak errors
// (server down, spawn failure) are swallowed: voice is best-effort and must
// never surface as a TUI error or interrupt the stream.
func (n *Narrator) emit(gid int, text string) {
	if n == nil || n.coord == nil || gid == 0 || text == "" {
		return
	}
	_ = n.coord.Emit(context.Background(), gid, text)
}

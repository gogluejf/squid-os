package voice

import (
	"context"
	"sync"

	"squid-os/internal/config"
)

// Coordinator owns speech-group state and arbitrates groups.
//
// A group is one logical utterance (an assistant message, or an alert).
// Chunks of the same group play back-to-back; a chunk from another group
// bumps the playing group through the onPreempt policy hook (default:
// cancel). Muted groups drop every chunk that arrives for them, including
// chunks emitted after cancellation.
//
// The coordinator never cancels the model stream or tool execution — voice
// lifecycle is independent of the turn context.
type Coordinator struct {
	speaker *Speaker
	cfg     *config.Voice

	emitMu    sync.Mutex // serialize admission across the out-of-lock policy hook
	mu        sync.Mutex
	nextGroup int
	current   int // playing group ID, 0 = none
	groups    map[int]*groupState
	onPreempt func(oldGroup int) // default: CancelGroup; future: enqueue

	speakingHook func(on bool) // UI indicator hook (nil = no reporting)
	speaking     bool          // last reported state of the hook

	groupDrainedHook GroupDrainedFunc // per-group drain callback (nil = none)
}

type groupState struct {
	id      int
	muted   bool
	ended   bool
	player  *Process // currently playing chunk (nil between chunks)
	waiters []func() // queued-chunk spawners blocked on the current chunk
}

// NewCoordinator builds a Coordinator over sp with the given voice config.
// cfg must be normalized (see Settings.NormalizeVoice).
func NewCoordinator(sp *Speaker, cfg *config.Voice) *Coordinator {
	c := &Coordinator{
		speaker:   sp,
		cfg:       cfg,
		nextGroup: 1,
		current:   0,
		groups:    make(map[int]*groupState),
	}
	// Default bump policy: kill the old group, mute it so buffered and
	// future chunks are dropped. If UX later prefers stacking, this body
	// becomes "enqueue" — no other coordinator change.
	c.onPreempt = c.CancelGroup
	return c
}

// SetOnPreempt swaps the cross-group bump policy. Must be called before any
// Emit; not safe to swap mid-playback.
func (c *Coordinator) SetOnPreempt(fn func(oldGroup int)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onPreempt = fn
}

// SetSpeakingHook installs a callback fired on every transition of the
// "audio is playing" state: on=true when the first chunk starts, on=false
// when playback ends or is killed. The hook runs OUTSIDE the coordinator
// lock and may re-enter it (it must not block). A nil hook disables
// reporting. Safe to install at any time; transitions already in flight are
// reported as they happen, so a late install never sees a missed start.
func (c *Coordinator) SetSpeakingHook(fn func(on bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.speakingHook = fn
}

// GroupDrainedFunc fires exactly once when a group stops being current:
// either its playback fully drained (last accepted chunk finished) or it was
// cancelled. It runs OUTSIDE the coordinator lock and may re-enter it (it
// must not block). A nil hook disables reporting. Safe to install at any
// time; groups that already stopped before installation are not reported.
type GroupDrainedFunc func(groupID int)

// SetGroupDrainedHook installs the per-group drain callback. It complements
// SetSpeakingHook, which only reports aggregate playing/stopped transitions
// and cannot identify WHICH group stopped — this one does.
func (c *Coordinator) SetGroupDrainedHook(fn GroupDrainedFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groupDrainedHook = fn
}

// StartGroup allocates a new group ID. It does not speak anything by itself.
func (c *Coordinator) StartGroup() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextGroup
	c.nextGroup++
	c.groups[id] = &groupState{id: id}
	return id
}

// Emit speaks one chunk of the given group, enforcing the group invariants:
//   - muted or ended group → dropped silently (even if the chunk arrives
//     after cancellation);
//   - unknown group → dropped;
//   - no group playing → speak now;
//   - same group playing → speak back-to-back after the current chunk;
//   - different group playing → onPreempt(playingGroup), then speak now.
//
// Speak errors (server down, spawn failure) are returned; they do not mute
// the group, so a transient failure can be retried by the caller.
func (c *Coordinator) Emit(ctx context.Context, groupID int, text string) error {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.mu.Lock()
	g, ok := c.groups[groupID]
	if !ok || g.muted || g.ended {
		c.mu.Unlock()
		return nil // drop silently
	}

	if c.current != 0 && c.current != groupID {
		old, preempt := c.current, c.onPreempt
		c.mu.Unlock()
		preempt(old)
		c.mu.Lock()
		if g.muted || g.ended {
			c.mu.Unlock()
			return nil
		}
	}
	defer c.mu.Unlock()
	c.current = groupID
	if g.player != nil {
		g.waiters = append(g.waiters, func() { c.startChunkLocked(g, ctx, text) })
		return nil
	}
	return c.startChunkLocked(g, ctx, text)
}

// startChunkLocked starts one already-admitted chunk. EndGroup closes
// admission, not playback: accepted chunks still drain after producer EOF.
func (c *Coordinator) startChunkLocked(g *groupState, ctx context.Context, text string) error {
	p, err := c.speaker.Speak(ctx, text, c.cfg, "")
	if err != nil {
		c.finishChunkLocked(g)
		return err
	}
	g.player = p
	go func() {
		<-p.Done()
		c.playerFinished(g.id, p)
	}()
	c.setSpeakingLocked(true) // first chunk of this group started playing
	// A group can be muted between Speak and here (Emit re-checks g.muted
	// after the out-of-lock preempt hook); a late kill must never leave an
	// orphaned process behind.
	if g.muted {
		p.Kill()
	}
	return nil
}

// playerFinished is also a deterministic test seam for delayed exit callbacks.
func (c *Coordinator) playerFinished(groupID int, p *Process) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.groups[groupID]
	if g == nil || g.player != p {
		return
	}
	g.player = nil
	c.finishChunkLocked(g)
}

func (c *Coordinator) finishChunkLocked(g *groupState) {
	if c.current != g.id || g.muted {
		return
	}
	if len(g.waiters) > 0 {
		next := g.waiters[0]
		g.waiters[0] = nil
		g.waiters = g.waiters[1:]
		next() // Exactly one successor, FIFO; no parked goroutines to leak.
	} else if g.ended {
		c.clearCurrentLocked(g.id)
	}
}

// EndGroup marks end-of-group: no more chunks will come. If a chunk is still
// playing it plays out; the group stops being "current" once it finishes.
// Calling on a muted/unknown group is a no-op.
func (c *Coordinator) EndGroup(groupID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g, ok := c.groups[groupID]
	if !ok || g.muted || g.ended {
		return
	}
	g.ended = true
	if g.player == nil && c.current == groupID {
		c.clearCurrentLocked(groupID)
	}
}

// CancelGroup mutes the group: kills the playing chunk's process group,
// drops queued chunks, and drops every future chunk arriving for this ID.
// Not sticky across groups: other groups speak normally. Safe to call on
// unknown or already-muted groups (no-op).
func (c *Coordinator) CancelGroup(groupID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g, ok := c.groups[groupID]
	if !ok {
		return
	}
	g.muted = true
	g.ended = true
	g.waiters = nil
	if g.player != nil {
		g.player.Kill()
		g.player = nil
	}
	if c.current == groupID {
		c.clearCurrentLocked(groupID)
	}
}

// Current returns the group ID that is playing or about to play, 0 if none.
func (c *Coordinator) Current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Speaker exposes the underlying speaker for callers that need detection
// without playback (async user-language tracking). Nil-safe by construction:
// a Coordinator always owns its speaker.
func (c *Coordinator) Speaker() *Speaker { return c.speaker }

// clearCurrentLocked forgets groupID as the active group. Caller holds mu.
func (c *Coordinator) clearCurrentLocked(groupID int) {
	if c.current == groupID {
		c.current = 0
	}
	c.setSpeakingLocked(false)
	hook := c.groupDrainedHook
	if hook != nil {
		c.mu.Unlock()
		hook(groupID)
		c.mu.Lock()
	}
}

// setSpeakingLocked reports a speaking-state transition to the hook, if any.
// No-op when the state is unchanged or no hook is installed. The hook runs
// with mu released so it can safely re-enter the coordinator.
func (c *Coordinator) setSpeakingLocked(on bool) {
	if c.speakingHook == nil || c.speaking == on {
		return
	}
	c.speaking = on
	hook := c.speakingHook
	c.mu.Unlock()
	hook(on)
	c.mu.Lock()
}

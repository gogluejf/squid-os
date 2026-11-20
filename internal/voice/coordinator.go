package voice

import (
	"context"
	"sync"

	"squid-os/internal/config"
	"squid-os/internal/log"
)

// InterruptPolicy controls how an incoming group takes over playback.
type InterruptPolicy uint8

const (
	InterruptNow InterruptPolicy = iota // cancel the active group immediately
	FinishChunk                         // finish the active chunk, discard its rest
	FinishGroup                         // wait for the whole group to end
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

	emitMu     sync.Mutex // serialize admission across the out-of-lock policy hook
	mu         sync.Mutex
	nextGroup  int
	current    int // playing group ID, 0 = none
	groups     map[int]*groupState
	onPreempt  func(oldGroup int)
	pending    []int
	preempting bool

	speakingHook func(on bool) // UI indicator hook (nil = no reporting)
	speaking     bool          // last reported state of the hook

	groupDrainedHook GroupDrainedFunc // per-group drain callback (nil = none)
	worker           bool             // one lazily started playback worker, retires when idle
	closed           bool
	notifications    []func()
	notifying        bool // one FIFO callback worker; never call hooks on the UI path
}

type groupState struct {
	id             int
	policy         InterruptPolicy
	admitted       bool
	stopAfterChunk bool
	muted          bool
	ended          bool
	player         *Process           // currently playing chunk (nil between chunks)
	waiters        []chunk            // accepted chunks, FIFO
	starting       bool               // spawn in flight
	cancel         context.CancelFunc // interrupts startup as well as playback
}

type chunk struct {
	ctx       context.Context
	text      string
	alertLang string
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
// when playback ends or is killed. The hook runs asynchronously in FIFO order OUTSIDE the coordinator
// lock and may re-enter it (it must return promptly). A nil hook disables
// reporting. Safe to install at any time; transitions already in flight are
// delivered to the hook installed at the time of the transition.
func (c *Coordinator) SetSpeakingHook(fn func(on bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.speakingHook = fn
}

// GroupDrainedFunc fires exactly once when a group stops being current:
// either its playback fully drained (last accepted chunk finished) or it was
// cancelled. It runs asynchronously in FIFO order OUTSIDE the coordinator lock
// and may re-enter it (it must return promptly). A nil hook disables reporting. Safe to install at any
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
func (c *Coordinator) StartGroup(policy ...InterruptPolicy) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextGroup
	c.nextGroup++
	p := InterruptNow
	if len(policy) > 0 && policy[0] <= FinishGroup {
		p = policy[0]
	}
	c.groups[id] = &groupState{id: id, policy: p}
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
// Emit only admits work; spawning and playback run on one worker.
// Playback failures are best-effort and do not stop later accepted chunks.
func (c *Coordinator) Emit(ctx context.Context, groupID int, text string) error {
	return c.EmitAlert(ctx, groupID, text, "")
}

// EmitAlert selects a configured voice for a known localized alert language.
// Empty language is narration: language and voice selection belong to TTS.
func (c *Coordinator) EmitAlert(ctx context.Context, groupID int, text, alertLang string) error {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.groups[groupID]
	if c.closed || g == nil || g.muted || g.ended {
		return nil
	}
	if g.stopAfterChunk {
		return nil
	}
	g.waiters = append(g.waiters, chunk{ctx: ctx, text: text, alertLang: alertLang})
	if !g.admitted {
		g.admitted = true
		if c.current == 0 {
			c.current = groupID
		} else if g.policy == InterruptNow {
			old, preempt := c.current, c.onPreempt
			c.preempting = true
			c.mu.Unlock()
			preempt(old)
			c.mu.Lock()
			c.preempting = false
			if c.closed || g.muted {
				c.promoteLocked()
				return nil
			}
			c.current = groupID
		} else {
			c.pending = append(c.pending, groupID)
		}
		log.LogVoice("group.admit", "group=%d policy=%d current=%d pending=%v", groupID, g.policy, c.current, c.pending)
	}
	if c.current != groupID {
		c.applyPendingPolicyLocked()
	}
	c.startWorkerLocked()
	return nil
}

// run owns the slow pipeline. There is at most one worker, including during
// preemption: cancellation interrupts its process before the next group.
func (c *Coordinator) run() {
	for {
		c.mu.Lock()
		g := c.groups[c.current]
		if c.closed || g == nil || g.muted || len(g.waiters) == 0 {
			c.worker = false
			c.mu.Unlock()
			return
		}
		next := g.waiters[0]
		g.waiters[0] = chunk{}
		g.waiters = g.waiters[1:]
		ctx, cancel := context.WithCancel(next.ctx)
		g.cancel = cancel
		g.starting = true
		c.applyPendingPolicyLocked()
		c.mu.Unlock()

		log.LogVoice("chunk.start", "group=%d language=%q text=%q", g.id, next.alertLang, next.text)
		p, err := c.speaker.Speak(ctx, next.text, c.cfg, next.alertLang)
		if p != nil {
			log.LogVoice("chunk.started", "group=%d pid=%d", g.id, p.Pid())
		} else {
			log.LogVoice("chunk.failed", "group=%d", g.id)
		}
		c.mu.Lock()
		g.starting = false
		if err != nil || c.closed || g.muted || c.current != g.id {
			cancel()
			if p != nil {
				p.Kill()
			}
			g.cancel = nil
			c.finishChunkLocked(g)
			c.mu.Unlock()
			if p != nil {
				<-p.Done()
			}
			continue
		}
		g.player = p
		c.setSpeakingLocked(true)
		c.mu.Unlock()
		<-p.Done()
		cancel()
		c.playerFinished(g.id, p)
	}
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
	g.cancel = nil
	c.finishChunkLocked(g)
}

func (c *Coordinator) finishChunkLocked(g *groupState) {
	if c.current != g.id || g.muted {
		return
	}
	if g.stopAfterChunk && !g.starting && g.player == nil {
		g.muted = true
		g.ended = true
		g.waiters = nil
		c.clearCurrentLocked(g.id)
		return
	}
	if len(g.waiters) == 0 && !g.starting && g.player == nil && g.ended {
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
	if g.player == nil && !g.starting && len(g.waiters) == 0 && c.current == groupID {
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
	if !ok || g.muted {
		return
	}
	for i, id := range c.pending {
		if id == groupID {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			break
		}
	}
	g.muted = true
	g.ended = true
	g.waiters = nil
	if g.cancel != nil {
		g.cancel()
	}
	if g.player != nil {
		g.player.Kill()
		g.player = nil
	}
	if c.current == groupID {
		c.clearCurrentLocked(groupID)
	} else {
		c.applyPendingPolicyLocked()
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
		c.notifyLocked(func() { hook(groupID) })
	}
	c.promoteLocked()
}

func (c *Coordinator) promoteLocked() {
	if c.closed || c.preempting || c.current != 0 {
		return
	}
	for len(c.pending) > 0 {
		id := c.pending[0]
		c.pending = c.pending[1:]
		if g := c.groups[id]; g != nil && !g.muted {
			c.current = id
			log.LogVoice("group.promote", "group=%d pending=%v", id, c.pending)
			c.startWorkerLocked()
			return
		}
	}
}

func (c *Coordinator) applyPendingPolicyLocked() {
	if len(c.pending) == 0 || c.closed {
		return
	}
	g := c.groups[c.current]
	next := c.groups[c.pending[0]]
	if g == nil || next == nil || next.policy != FinishChunk {
		return
	}
	g.stopAfterChunk = true
	g.waiters = nil
	if g.player == nil && !g.starting {
		g.muted = true
		g.ended = true
		c.clearCurrentLocked(g.id)
	}
}

func (c *Coordinator) startWorkerLocked() {
	if !c.worker && !c.closed && c.current != 0 {
		c.worker = true
		go c.run()
	}
}

// CancelAll kills current + all pending groups. Shut-up = silence now.
func (c *Coordinator) CancelAll() {
	c.mu.Lock()
	ids := make([]int, 0, len(c.pending)+1)
	if c.current != 0 {
		ids = append(ids, c.current)
	}
	ids = append(ids, c.pending...)
	c.pending = nil
	c.mu.Unlock()
	for _, id := range ids {
		c.CancelGroup(id)
	}
}

// FinishCurrentChunk closes admission without killing a playing chunk.
func (c *Coordinator) FinishCurrentChunk(groupID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.groups[groupID]
	if g == nil || g.muted {
		return
	}
	g.stopAfterChunk = true
	g.ended = true
	g.waiters = nil
	if c.current == groupID {
		c.finishChunkLocked(g)
	} else {
		g.muted = true
		for i, id := range c.pending {
			if id == groupID {
				c.pending = append(c.pending[:i], c.pending[i+1:]...)
				break
			}
		}
	}
}

// setSpeakingLocked reports a speaking-state transition to the hook, if any.
// No-op when the state is unchanged or no hook is installed. The hook runs
// asynchronously outside mu so it can safely re-enter the coordinator.
func (c *Coordinator) setSpeakingLocked(on bool) {
	if c.speakingHook == nil || c.speaking == on {
		return
	}
	c.speaking = on
	hook := c.speakingHook
	c.notifyLocked(func() { hook(on) })
}

// notifyLocked preserves transition order at the state mutation, not at
// goroutine scheduling time. Hooks can re-enter the coordinator. A blocked
// consumer parks only this single dispatcher, never Update or playback.
func (c *Coordinator) notifyLocked(fn func()) {
	c.notifications = append(c.notifications, fn)
	if c.notifying {
		return
	}
	c.notifying = true
	go func() {
		for {
			c.mu.Lock()
			if len(c.notifications) == 0 {
				c.notifying = false
				c.mu.Unlock()
				return
			}
			fn := c.notifications[0]
			c.notifications[0] = nil
			c.notifications = c.notifications[1:]
			c.mu.Unlock()
			fn()
		}
	}()
}

// Close permanently closes admission and interrupts all accepted work.
// Playback retires without a UI round-trip; notification hooks must return
// (Tea Send is released by program exit). Idempotent.
func (c *Coordinator) Close() {
	c.mu.Lock()
	c.closed = true
	ids := make([]int, 0, len(c.groups))
	for id := range c.groups {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		c.CancelGroup(id)
	}
}

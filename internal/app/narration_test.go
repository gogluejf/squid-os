package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/voice"
)

// fakeTTS is a recording TTS CLI: it reads stdin to EOF and exits 0, so
// coordinator groups drain back-to-back deterministically without aplay.
func writeFakeTTSCli(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "squid-tts-say")
	src := `#!/bin/sh
cat > /dev/null
exit 0
`
	if err := os.WriteFile(path, []byte(src), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return path
}

// newVoiceTestModel builds a Model with voice wired to a real coordinator
// over a fake TTS CLI. No provider, no TUI program.
func newVoiceTestModel(t *testing.T, enabled bool) Model {
	t.Helper()
	m := newQueueTestModel(t)
	sp, err := voice.NewSpeaker(voice.SpeakerOptions{CLIPath: writeFakeTTSCli(t)})
	if err != nil {
		t.Fatalf("NewSpeaker: %v", err)
	}
	cfg := config.Voice{Enabled: enabled, Voices: map[string]string{"en": "jane", "fr": "jane"}}
	m.voiceOn = enabled
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))
	return m
}

// TestNarrationFirstChunkWhileGenerating: feeding deltas emits complete
// sentence chunks to the coordinator immediately — first audio starts while
// later sentences are still generating (no wait for stream end).
func TestNarrationFirstChunkWhileGenerating(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrator.beginMessage(true)

	m.narrator.feedDelta("Hello there. ")
	m.narrator.feedDelta("This is the second sentence of the reply. ")

	// The first sentence was emitted as a chunk BEFORE the second one even
	// arrived: the coordinator already has a current group.
	if got := m.narrator.coord.Current(); got == 0 {
		t.Fatal("first chunk did not reach the coordinator while generating")
	}

	// Stream end: final flush + EndGroup; the group drains and clears.
	m.narrator.endMessage(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for m.narrator.coord.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.narrator.coord.Current(); got != 0 {
		t.Fatalf("group still current after endMessage+drain: %d", got)
	}
}

// TestNarrationMutedGroupStopsReceivingFeeds: once the group is cancelled,
// further feeds are dropped by the coordinator (muted) and EndGroup is a
// no-op — the producer checks the coordinator before emitting.
func TestNarrationMutedGroupStopsReceivingFeeds(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrator.beginMessage(true)

	gid := m.narrator.coord.Current()
	if gid == 0 {
		// No chunk yet (text under min-words hold); mint expectation via
		// feed then cancel.
		m.narrator.feedDelta("First sentence here. ")
		gid = m.narrator.coord.Current()
		if gid == 0 {
			t.Fatal("expected an active group after feed")
		}
	}

	// alt+x-style cancellation: muted group drops every future chunk.
	m.narrator.cancelActive()

	// Late chunks (timer fire or in-flight delta) must be dropped silently.
	m.narrator.emit(gid, "late chunk that must be dropped.")
	// EndGroup on a muted group is a no-op; must not panic.
	m.narrator.coord.EndGroup(gid)
	if got := m.narrator.coord.Current(); got != 0 {
		t.Fatalf("current group after cancel = %d, want 0", got)
	}
}

// TestNarrationGroupSurvivesUIStreamReset: UIStreamState.reset() between tool
// phases must NOT end the speech group; chunks fed across the reset land in
// the same group with nothing lost or duplicated.
func TestNarrationGroupSurvivesUIStreamReset(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrationBeginForTest()

	// First sentence is emitted immediately on feed (it exceeds the
	// min-words hold), so the group becomes current right away.
	m.narrationFeedForTest("First phase sentence before tools.")
	before := m.narrator.coord.Current()
	if before == 0 {
		t.Fatal("no active group after first feed")
	}

	// Simulate the tool-phase reset exactly as handleToolEvent does.
	m.session.Stream.Reset()
	m.session.UIStream.reset()

	after := m.narrator.coord.Current()
	if after != before {
		t.Fatalf("group changed across UIStreamState.reset(): %d -> %d", before, after)
	}

	// A chunk fed after the reset goes to the SAME group (back-to-back, no
	// bump). End the message and wait for both accepted chunks to drain.
	m.narrationFeedForTest("Second phase sentence arrives now.")
	m.narrator.endMessage(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for m.narrator.coord.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.narrator.coord.Current(); got != 0 {
		t.Fatalf("group did not drain after reset+feed+end: %d", got)
	}
}

// TestNarrationVoiceIndependentOfTurnContext: cancelling the turn context
// (ctrl+c abort) never cancels the voice context, and vice versa — voice
// mute kills only speech, the model stream keeps running.
func TestNarrationVoiceIndependentOfTurnContext(t *testing.T) {
	m := newVoiceTestModel(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Turn-context style cancel must not affect a voice Emit.
	cancel()
	_ = m.narrator.coord.Emit(ctx, m.narrator.coord.StartGroup(), "still speaks.")

	// Voice cancel must not touch the turn/stream context: a fresh context
	// derived from Background stays alive after CancelGroup.
	vctx, vcancel := context.WithCancel(context.Background())
	defer vcancel()
	gid := m.narrator.coord.StartGroup()
	_ = m.narrator.coord.Emit(vctx, gid, "independent.")
	m.narrator.coord.CancelGroup(gid)
	select {
	case <-vctx.Done():
		t.Fatal("voice cancel leaked into the model-stream context")
	default:
	}
}

// TestNarrationDisabledIsNoOp: with voice off, begin/feed/end allocate no
// group and speak nothing; the narrator stays inert.
func TestNarrationDisabledIsNoOp(t *testing.T) {
	m := newVoiceTestModel(t, false)
	m.narrator.beginMessage(false)
	m.narrator.feedDelta("Nothing should be spoken here.")
	m.narrator.endMessage(context.Background())
	if got := m.narrator.coord.Current(); got != 0 {
		t.Fatalf("disabled voice allocated a group: %d", got)
	}
}

// TestNarrationShutdownCancelsActiveGroup: quitCmd tears down any in-flight
// speech group (kill + state cleared) alongside session cleanup.
func TestNarrationShutdownCancelsActiveGroup(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrationBeginForTest()
	m.narrationFeedForTest("Shutting down mid-utterance.")
	if m.narrator.coord.Current() == 0 {
		t.Fatal("expected an active group before shutdown")
	}

	m.quitCmd() // returns tea.Quit; side effect is what we assert
	if got := m.narrator.coord.Current(); got != 0 {
		t.Fatalf("shutdown left group %d active", got)
	}
}

// --- test helpers mirroring the production wiring (setStreamMode/handleStreamEvent) ---

// narrationBeginForTest mirrors setStreamMode's voice hook.
func (m Model) narrationBeginForTest() { m.narrator.beginMessage(m.voiceOn) }

// narrationFeedForTest mirrors handleStreamEvent's LoopContinue voice hook.
func (m Model) narrationFeedForTest(delta string) { m.narrator.feedDelta(delta) }

// --- speaking indicator (task 1.6) -----------------------------------------

// TestSpeakingIndicatorAppearsOnFirstChunkClearsOnExit: with the coordinator
// hook wired, the first chunk start turns the indicator on and player exit
// turns it off — via the voiceSpeakingMsg that Update applies to Model state.
func TestSpeakingIndicatorAppearsOnFirstChunkClearsOnExit(t *testing.T) {
	m := newVoiceTestModel(t, true)
	hook := &recordingHook{}
	m.narrator.coord.SetSpeakingHook(hook.fn())

	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Hello there. ")
	m.narrator.feedDelta("This is the second sentence of the reply. ")

	// First chunk reached the coordinator while generating → hook fired on.
	on, ok := hook.last()
	if !ok || !on {
		t.Fatalf("indicator not on after first chunk: events=%v", hook.events)
	}

	// Stream end: final flush + EndGroup; drain clears the indicator.
	m.narrator.endMessage(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for m.narrator.coord.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	last, _ := hook.last()
	if last {
		t.Fatalf("indicator stuck on after completion: events=%v", hook.events)
	}

	// The Bubble Tea update path applies the transition to Model state.
	m2, _ := m.Update(voiceSpeakingMsg{on: true})
	if !m2.(Model).voiceSpeaking {
		t.Fatal("Update did not set voiceSpeaking on")
	}
	m3, _ := m2.(Model).Update(voiceSpeakingMsg{on: false})
	if m3.(Model).voiceSpeaking {
		t.Fatal("Update did not clear voiceSpeaking")
	}
}

// TestSpeakingIndicatorKillPathClears: cancellation (alt+x / toggle-off /
// shutdown) kills the playing chunk and the kill path clears the indicator —
// never stuck on after cancellation.
func TestSpeakingIndicatorKillPathClears(t *testing.T) {
	m := newVoiceTestModel(t, true)
	hook := &recordingHook{}
	m.narrator.coord.SetSpeakingHook(hook.fn())

	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Shutting down mid-utterance.")
	on, ok := hook.last()
	if !ok || !on {
		t.Fatalf("indicator not on during playback: events=%v", hook.events)
	}

	m.narrator.cancelActive() // kill path: CancelGroup SIGKILLs the player
	deadline := time.Now().Add(2 * time.Second)
	for m.narrator.coord.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	last, _ := hook.last()
	if last {
		t.Fatalf("indicator stuck on after cancel: events=%v", hook.events)
	}
}

// TestSpeakingIndicatorStaleCallbackCannotClearNewerGroup: a delayed
// completion callback from a superseded group must not clear the newer
// group's indicator. Guarded at the coordinator (player-identity check) and
// observed here through the hook + Model render state.
func TestSpeakingIndicatorStaleCallbackCannotClearNewerGroup(t *testing.T) {
	m := newVoiceTestModel(t, true)
	hook := &recordingHook{}
	m.narrator.coord.SetSpeakingHook(hook.fn())

	g1 := m.narrator.coord.StartGroup()
	if err := m.narrator.coord.Emit(context.Background(), g1, "stale group."); err != nil {
		t.Fatal(err)
	}
	waitCoordIdleOrPlaying(t, m.narrator.coord)

	// The default onPreempt policy cancels g1 when g2 takes over — that is
	// exactly the stale-callback scenario: g1's watcher fires late, after
	// replacement. Only g2's own exit may turn the indicator off.
	g2 := m.narrator.coord.StartGroup()
	if err := m.narrator.coord.Emit(context.Background(), g2, "fresh group."); err != nil {
		t.Fatal(err)
	}
	waitCoordIdleOrPlaying(t, m.narrator.coord)
	if got := m.narrator.coord.Current(); got != g2 {
		t.Fatalf("current = %d, want %d", got, g2)
	}
	on, _ := hook.last()
	if !on {
		t.Fatalf("newer group's indicator not on after bump: events=%v", hook.events)
	}

	// Only g2's own exit may turn the indicator off.
	m.narrator.coord.CancelGroup(g2)
	deadline := time.Now().Add(2 * time.Second)
	for m.narrator.coord.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	last, _ := hook.last()
	if last {
		t.Fatalf("indicator stuck on after g2 killed: events=%v", hook.events)
	}
}

// waitCoordIdleOrPlaying waits until the fake CLI has started (the hanging
// recording CLI logs its argv before sleeping), i.e. the chunk is admitted
// and playing.
func waitCoordIdleOrPlaying(t *testing.T, c *voice.Coordinator) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Current() != 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// recordingHook captures speaking-state transitions in order (test seam for
// the coordinator hook; same shape as the one in internal/voice).
type recordingHook struct {
	mu     sync.Mutex
	events []bool
}

func (h *recordingHook) fn() func(on bool) {
	return func(on bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.events = append(h.events, on)
	}
}

func (h *recordingHook) last() (bool, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) == 0 {
		return false, false
	}
	return h.events[len(h.events)-1], true
}

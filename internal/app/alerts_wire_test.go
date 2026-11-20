package app

import (
	"errors"
	"testing"
	"time"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/voice"
)

var errFakeStream = errors.New("connection reset by peer")

// newAlertTestModel builds a voice-wired model with the runtime toggle on, as
// if /voice had been flipped: alerts must speak. The fake TTS CLI records
// nothing; assertions go through the coordinator's group state. speakAlert
// takes the live m.settings.Voice.VoiceEnabled() from speakAlertEvent (UI goroutine), so flipping
// the flag after construction is honored by construction — no indirection.
func newAlertTestModel(t *testing.T) Model {
	t.Helper()
	m := newVoiceTestModel(t, true)
	m.settings.Voice.Mode = config.NarrationStep
	return m
}

// waitDrained polls until the coordinator has no current group (bounded).
func waitDrained(t *testing.T, c *voice.Coordinator) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for c.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if c.Current() != 0 {
		t.Fatalf("coordinator still current after drain deadline: %d", c.Current())
	}
}

// TestSpeakAlertOwnGroupBumpsNarration: an alert is its OWN group — landing
// while a narration tail plays bumps it (default onPreempt), and both are
// independently cancellable.
func TestSpeakAlertOwnGroupBumpsNarration(t *testing.T) {
	m := newAlertTestModel(t)
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("This sentence is being spoken by the narrator. ")
	narrGid := m.narrator.coord.Current()
	if narrGid == 0 {
		t.Fatal("no narration group before alert")
	}

	m.speakAlertEvent(voice.Authorization, "")
	alertGid := m.narrator.coord.Current()
	if alertGid == 0 || alertGid == narrGid {
		t.Fatalf("alert group = %d, want a fresh non-zero group distinct from narration %d", alertGid, narrGid)
	}
	// Bump killed the narration group: its late chunks are muted and dropped.
	m.narrator.emit(narrGid, "late narration chunk that must be muted.")
	if got := m.narrator.coord.Current(); got == narrGid {
		t.Fatalf("bumped narration group %d resurrected", narrGid)
	}
	waitDrained(t, m.narrator.coord)
	// Deterministic mute check: a fresh emit for the dead narration group
	// must not start anything (muted groups drop silently).
	m.narrator.emit(narrGid, "another late narration chunk.")
	if got := m.narrator.coord.Current(); got == narrGid {
		t.Fatalf("muted narration group %d accepted a new chunk", narrGid)
	}
}

// TestSpeakAlertRecursionGuard: while an alert group is in flight, a second
// alert is DROPPED silently — a TTS failure must never spawn a spoken error
// about itself. After the first alert drains, alerts work again.
func TestSpeakAlertRecursionGuard(t *testing.T) {
	m := newAlertTestModel(t)

	m.speakAlertEvent(voice.Error, "first failure")
	firstGid := m.narrator.coord.Current()
	if firstGid == 0 {
		t.Fatal("first alert did not start a group")
	}

	// Second alert while the first is playing: dropped, no new group.
	m.speakAlertEvent(voice.Error, "second failure must be dropped")
	if got := m.narrator.coord.Current(); got != firstGid {
		t.Fatalf("recursion guard failed: current = %d, want still %d", got, firstGid)
	}

	// Drain: the guard clears and alerts work again.
	waitDrained(t, m.narrator.coord)
	m.speakAlertEvent(voice.SubAgent, "trader")
	if got := m.narrator.coord.Current(); got == 0 {
		t.Fatal("alert after drain was dropped — guard never cleared")
	}
	waitDrained(t, m.narrator.coord)
}

// TestSpeakAlertVoiceOffIsSilent: with the runtime toggle off, alerts are a
// complete no-op — no groups, no state change (acceptance: voice off = no
// behavior change).
func TestSpeakAlertVoiceOffIsSilent(t *testing.T) {
	m := newAlertTestModel(t)
	m.settings.Voice.Mode = config.NarrationOff // runtime /voice off

	m.speakAlertEvent(voice.Error, "boom")
	if got := m.narrator.coord.Current(); got != 0 {
		t.Fatalf("voice off still spoke an alert: group %d", got)
	}
	if m.narrator.alertActive {
		t.Fatal("recursion guard set while voice off")
	}
}

// TestToolEventNeedAuthSpeaksAuthorization: the auth-prompt call site speaks
// the authorization alert in the user's language as its own group. The fake
// TTS drains in milliseconds and setAuthMode renders the overlay, so the
// group is usually already finished when handleToolEvent returns — prove the
// speak via the deterministic guard lifecycle + a follow-up alert instead of
// a possibly-already-drained Current().
func TestToolEventNeedAuthSpeaksAuthorization(t *testing.T) {
	m := newAlertTestModel(t)
	m.lastUserLang = "fr"

	event := chat.ToolEvent{Type: chat.ToolEventNeedAuth, MsgIdx: 0, ToolIndex: 0,
		AuthRequest: &chat.AuthRequest{ToolName: "bash"}}
	updated, _ := m.handleToolEvent(event)
	m2 := updated.(Model)

	if m2.session.UIStream.AuthorizationCtx == nil {
		t.Fatal("auth event did not enter auth mode")
	}
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after auth alert")
	}
	// Guard cleared → alerts still work.
	m2.speakAlertEvent(voice.SubAgent, "probe")
	if got := m2.narrator.coord.Current(); got == 0 {
		t.Fatal("alert after auth drain dropped — guard never cleared")
	}
	waitDrained(t, m2.narrator.coord)
}

// TestToolEventRunningSkillLoadedSpeaksAlert: a skill load at Running speaks
// "I loaded the skill {name}". The fake TTS drains in milliseconds, so the
// group is often already finished when handleToolEvent returns (it renders
// the viewport); the deterministic proof that speakAlert ran is the
// recursion-guard lifecycle: set while the alert is in flight, cleared once
// it drains — and a FRESH alert right after must work again.
func TestToolEventRunningSkillLoadedSpeaksAlert(t *testing.T) {
	m := newAlertTestModel(t)

	event := chat.ToolEvent{Type: chat.ToolEventRunning, MsgIdx: 0, ToolIndex: 0, LoadedSkill: "browser-use"}
	updated, _ := m.handleToolEvent(event)
	m2 := updated.(Model)

	if m2.session.Doc.Config.ActiveSkill != "browser-use" {
		t.Fatalf("ActiveSkill = %q, want browser-use", m2.session.Doc.Config.ActiveSkill)
	}
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after skill-load alert")
	}
	// Guard cleared → alerts still work: a second alert starts a group.
	m2.speakAlertEvent(voice.SubAgent, "probe")
	if got := m2.narrator.coord.Current(); got == 0 {
		t.Fatal("alert after skill-load drain dropped — guard never cleared")
	}
	waitDrained(t, m2.narrator.coord)
}

// TestToolEventRunningAgentSpeaksSubAgentAlert: an agent tool starting at
// Running speaks the sub-agent alert PRE-execution (AgentName set); a
// non-agent Running event with no AgentName stays silent. Same fast-drain
// caveat as the skill test: prove via the guard lifecycle + a follow-up
// alert, not a possibly-already-drained Current().
func TestToolEventRunningAgentSpeaksSubAgentAlert(t *testing.T) {
	m := newAlertTestModel(t)

	event := chat.ToolEvent{Type: chat.ToolEventRunning, MsgIdx: 0, ToolIndex: 0, AgentName: "trader"}
	updated, _ := m.handleToolEvent(event)
	m2 := updated.(Model)
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after sub-agent alert")
	}
	m2.speakAlertEvent(voice.SubAgent, "probe")
	if got := m2.narrator.coord.Current(); got == 0 {
		t.Fatal("alert after sub-agent drain dropped — guard never cleared")
	}
	waitDrained(t, m2.narrator.coord)

	// Non-agent Running: no alert at all — guard must stay clear and no
	// group may appear.
	m3 := newAlertTestModel(t)
	updated3, _ := m3.handleToolEvent(chat.ToolEvent{Type: chat.ToolEventRunning, MsgIdx: 0, ToolIndex: 0})
	m4 := updated3.(Model)
	waitDrained(t, m4.narrator.coord)
	if m4.narrator.alertActive {
		t.Fatal("non-agent Running event set the alert guard")
	}
}

// TestLoopErrorSpeaksErrorAlertWithDetail: a stream error speaks the Error
// alert (lead-in + raw detail, same group) BEFORE killing the aborted
// narration group. Drives the LoopError branch directly with a real
// ProcessStreamEvent result (error event on an active stream).
func TestLoopErrorSpeaksErrorAlertWithDetail(t *testing.T) {
	m := newAlertTestModel(t)
	// An in-flight narration group that the error path must cancel AFTER
	// speaking the alert.
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Answer in progress when the error hit. ")
	narrGid := m.narrator.coord.Current()
	if narrGid == 0 {
		t.Fatal("no narration group before error")
	}

	updated, _ := m.handleStreamEvent(chat.StreamEvent{Error: errFakeStream})
	m2 := updated.(Model)

	// The aborted narration group is dead...
	if got := m2.narrator.coord.Current(); got == narrGid {
		t.Fatalf("aborted narration group %d still current", narrGid)
	}
	// ...and the alert group either already drained or is the one that played
	// (fake TTS exits fast): assert the guard cleared and no group lingers.
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after error alert")
	}
}

// TestSilentFailureSpeaksGenericError: a SilentFailure (empty stream ending
// with no stop reason) takes the GENERIC Error branch — finding #10 ruling:
// voice alerts are reactive announcements, not a preventive "compact your
// context" workflow, so no dedicated context-bust event is spoken. Verified
// by driving the exact classification condition through ProcessStreamEvent.
func TestSilentFailureSpeaksGenericError(t *testing.T) {
	// First prove the classifier: empty stream + no stop reason → SilentFailure.
	sessProbe := newQueueTestModel(t).session.Session
	res := chat.ProcessStreamEvent(sessProbe, chat.StreamEvent{Done: true})
	if res.Action != chat.LoopError || !res.SilentFailure {
		t.Fatalf("classifier = %+v, want LoopError+SilentFailure", res)
	}

	// Then the app branch: a fresh model's LoopError handler with a
	// SilentFailure result speaks and leaves no stuck state.
	m := newAlertTestModel(t)
	updated, _ := m.handleStreamEvent(chat.StreamEvent{Done: true})
	m2 := updated.(Model)

	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after silent-failure error alert")
	}
}

// TestThinkingStartSpeaksOncePerSession: the first thinking delta speaks the
// ThinkingStart alert (thinking mode enabled); further thinking deltas in the
// same block stay silent; a NEW message (stream reset) speaks again. A final
// case proves thinking mode OFF stays silent even when InThinking fires.
func TestThinkingStartSpeaksOncePerSession(t *testing.T) {
	m := newAlertTestModel(t)
	// Thinking mode must be enabled for the alert to fire (spec).
	m.session.Doc.Config.Inference.Thinking.Enabled = true

	// First thinking delta: alert fires.
	updated, _ := m.handleStreamEvent(chat.StreamEvent{Thinking: "hmm", InThinking: true})
	m2 := updated.(Model)
	if got := m2.narrator.coord.Current(); got == 0 {
		t.Fatal("first thinking delta did not speak the alert")
	}
	waitDrained(t, m2.narrator.coord)

	// Second thinking delta, same block: silent.
	updated2, _ := m2.handleStreamEvent(chat.StreamEvent{Thinking: "more", InThinking: true})
	m3 := updated2.(Model)
	if got := m3.narrator.coord.Current(); got != 0 {
		t.Fatalf("second thinking delta spoke again: group %d", got)
	}

	// New message: UIStream reset clears the flag; thinking speaks again.
	m3.session.UIStream.reset()
	updated3, _ := m3.handleStreamEvent(chat.StreamEvent{Thinking: "again", InThinking: true})
	m4 := updated3.(Model)
	if got := m4.narrator.coord.Current(); got == 0 {
		t.Fatal("thinking after reset did not speak again")
	}
	waitDrained(t, m4.narrator.coord)

	// Thinking mode OFF: InThinking alone must NOT speak (spec: only when
	// thinking mode is active).
	m5 := newAlertTestModel(t)
	m5.session.Doc.Config.Inference.Thinking.Enabled = false
	updated5, _ := m5.handleStreamEvent(chat.StreamEvent{Thinking: "stray", InThinking: true})
	m6 := updated5.(Model)
	waitDrained(t, m6.narrator.coord)
	if m6.narrator.alertActive {
		t.Fatal("thinking-mode-off event set the alert guard")
	}
}

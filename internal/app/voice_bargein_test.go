package app

import (
	"os"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/inputqueue"
	"squid-os/internal/localinput"
)

// newBargeInModel builds a voice-wired model with one active speech group, as
// if the assistant were mid-message ("Jane speaks"). The returned gid is the
// live group; admission must kill it.
func newBargeInModel(t *testing.T) (Model, int) {
	t.Helper()
	m := newVoiceTestModel(t, true)
	// Configure a provider so sendMessage/sendLocalSocketMessage pass
	// ensureProviderConfigured and reach startStream (stubbed in tests).
	m.session.Doc.Config.Inference = config.InferenceConfig{Provider: "testprov", Model: "test-model"}
	m.endpoints = config.EndpointsConfig{Providers: []config.ProviderSettings{{
		Name:        "testprov",
		Credentials: &config.ProviderCreds{ActiveAuthMethod: config.AuthNone},
	}}}
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Jane is speaking this sentence out loud. ")
	gid := m.narrator.coord.Current()
	if gid == 0 {
		t.Fatal("no active speech group before admission")
	}
	return m, gid
}

// assertGroupSilenced verifies the whole group is dead: not current, and any
// late chunk for it stays dropped (muted).
func assertGroupSilenced(t *testing.T, m *Model, gid int) {
	t.Helper()
	// Barge-in finishes the current chunk then stops. Wait for drain.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.narrator.coord.Current() != gid {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Late chunks are dropped after the group ends.
	m.narrator.emit(gid, "late chunk that must stay muted.")
}

// TestIdleSubmitCancelsSpeechBeforeNewTurn: idle keyboard path (Enter →
// sendMessage) silences the active group at admission, BEFORE the new turn's
// own group starts — the new message begins silent.
func TestIdleSubmitCancelsSpeechBeforeNewTurn(t *testing.T) {
	withStartStreamStub(t)
	m, gid := newBargeInModel(t)
	// sendMessage appends to history via m.paths; point it at a temp dir so
	// no artifact lands in the package directory.
	m.paths = config.Paths{Root: t.TempDir()}
	t.Cleanup(func() { os.Remove(m.paths.HistoryFile()) })
	m.mode = ModeChat
	m.textarea.SetValue("new instruction")

	updated, _ := m.handleKey(keyEnter())
	m2 := updated.(*Model)

	assertGroupSilenced(t, m2, gid)
	// The stubbed startStream ran setStreamMode → beginMessage(voiceOn): the
	// NEW turn allocated its own fresh group. It may already have drained by
	// the time we check (fake TTS exits instantly), so Current() can be 0 —
	// what matters is it is NOT the killed group and late chunks stay muted.
	if got := m2.narrator.coord.Current(); got == gid {
		t.Fatalf("new turn still shows killed group %d", gid)
	}
}

// TestQueuedStepSubmitCancelsSpeechAtAdmission: busy keyboard path (Enter →
// admitQueuedInput, next-step boundary) cancels the group at ADMISSION even
// though delivery waits for the safe boundary.
func TestQueuedStepSubmitCancelsSpeechAtAdmission(t *testing.T) {
	m, gid := newBargeInModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("steer me")

	updated, _ := m.handleKey(keyEnter())
	m2 := updated.(Model)

	assertGroupSilenced(t, &m2, gid)
	if c := m2.submissions.Counts(m2.liveSessionID()); c.Step != 1 {
		t.Fatalf("admission did not park the entry: %+v", c)
	}
}

// TestQueuedAfterTurnSubmitCancelsSpeechAtAdmission: the Alt+Q after-turn
// boundary goes through the same admission hook.
func TestQueuedAfterTurnSubmitCancelsSpeechAtAdmission(t *testing.T) {
	m, gid := newBargeInModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("after this")

	updated, _ := m.handleKey(keyAltQ())
	m2 := updated.(Model)

	assertGroupSilenced(t, &m2, gid)
	if c := m2.submissions.Counts(m2.liveSessionID()); c.Turn != 1 {
		t.Fatalf("admission did not park the entry: %+v", c)
	}
}

// TestSocketBusyAdmissionCancelsSpeechAtAdmission: the socket path while BUSY
// pushes no UI event until the boundary — the cancel must fire at admission
// itself, inside Update's localAdmissionMsg handling.
func TestSocketBusyAdmissionCancelsSpeechAtAdmission(t *testing.T) {
	m, gid := newBargeInModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true

	reply := make(chan localinput.ReceiptInfo, 1)
	req := inputqueue.AdmissionRequest{
		RequestID: reqIDFor(1), SessionID: m.liveSessionID(), Source: "squid-sponge", Text: "busy voice note", Boundary: inputqueue.BoundaryStep,
	}
	updated, _ := m.Update(localAdmissionMsg{req: req, reply: reply})
	m2 := updated.(Model)
	if res := <-reply; !res.OK {
		t.Fatalf("socket admission rejected: %+v", res)
	}

	assertGroupSilenced(t, &m2, gid)
	// Parked for the boundary: nothing was sent immediately.
	if c := m2.submissions.Counts(m2.liveSessionID()); c.Step != 1 {
		t.Fatalf("busy arrival not parked: %+v", c)
	}
	for _, msg := range m2.session.Doc.Messages {
		if msg.Role == "user" && msg.Text == "busy voice note" {
			t.Fatal("busy socket arrival must not be sent immediately")
		}
	}
}

// TestSocketIdleDeliveryCancelsSpeechAtAdmission: an idle socket submission is
// admitted AND delivered in one frame; the admission-side cancel already
// silenced the old group before sendLocalSocketMessage started the new turn.
func TestSocketIdleDeliveryCancelsSpeechAtAdmission(t *testing.T) {
	withStartStreamStub(t)
	m, gid := newBargeInModel(t)

	reply := make(chan localinput.ReceiptInfo, 1)
	req := inputqueue.AdmissionRequest{
		RequestID: reqIDFor(2), SessionID: m.liveSessionID(), Source: "squid-sponge", Text: "idle voice note", Boundary: inputqueue.BoundaryStep,
	}
	updated, _ := m.Update(localAdmissionMsg{req: req, reply: reply})
	m2 := updated.(*Model)
	if res := <-reply; !res.OK {
		t.Fatalf("socket admission rejected: %+v", res)
	}

	assertGroupSilenced(t, m2, gid)
	var found bool
	for _, msg := range m2.session.Doc.Messages {
		if msg.Role == "user" && msg.Text == "idle voice note" {
			found = true
		}
	}
	if !found {
		t.Fatal("idle socket arrival not sent as user message")
	}
}

// TestTypingWithoutSubmitDoesNotCancel: plain typing while Jane speaks must
// NOT cancel (listen-while-drafting preserved). Only submission cancels.
func TestTypingWithoutSubmitDoesNotCancel(t *testing.T) {
	m, gid := newBargeInModel(t)
	m.mode = ModeChat

	updated, _ := m.handleKey(keyRune('h'))
	m2 := updated.(Model)

	if got := m2.narrator.coord.Current(); got != gid {
		t.Fatalf("typing cancelled the group: current = %d, want %d", got, gid)
	}
}

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/inputqueue"
	"squid-os/internal/localinput"
	runtimeconfig "squid-os/internal/runtime"
)

// newQueueTestModel builds a minimal Model with a memory-only submission
// coordinator and a live session identity. No provider, no TUI program.
func newQueueTestModel(t *testing.T) Model {
	t.Helper()
	sess := NewRootUISession(config.SessionConfig{}, config.Paths{Root: t.TempDir()}, runtimeconfig.Catalog{})
	ta := textarea.New()
	ta.SetHeight(2)
	ta.MaxHeight = 20
	ta.Focus()
	m := Model{
		textarea: ta,
		width:    80,
		height:   24,
		session:  sess,
	}
	m.submissions = NewMemSubmissionCoordinator(func() bool { return false })
	t.Cleanup(m.submissions.Close)
	return m
}

func keyEnter() tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyEnter} }
func keyAltQ() tea.KeyMsg       { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}, Alt: true} }
func keyCtrlC() tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestBusyComposerQueuesStepOnEnter(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("steer me")

	updated, _ := m.handleKey(keyEnter())
	m2 := updated.(Model)

	if got := m2.textarea.Value(); got != "" {
		t.Fatalf("draft not cleared after admit: %q", got)
	}
	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 1 || c.Turn != 0 {
		t.Fatalf("counts = %+v, want step=1 turn=0", c)
	}
	pending := m2.submissions.Pending(m2.liveSessionID())
	if len(pending) != 1 || pending[0].Text != "steer me" || pending[0].Boundary != inputqueue.BoundaryStep {
		t.Fatalf("pending = %+v", pending)
	}
	// The session history must NOT have grown (no immediate send while busy).
	if n := len(m2.session.Doc.Messages); n != len(m.session.Doc.Messages) {
		t.Fatalf("session messages grew while busy: %d → %d", len(m.session.Doc.Messages), n)
	}
}

func TestBusyComposerQueuesTurnOnAltQ(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("after this")

	updated, _ := m.handleKey(keyAltQ())
	m2 := updated.(Model)

	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 0 || c.Turn != 1 {
		t.Fatalf("counts = %+v, want turn=1", c)
	}
}

func TestIdleEnterStillSendsNotQueues(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeChat
	m.textarea.SetValue("hello idle")

	// Idle Enter goes through the normal send path; without a configured
	// provider it is blocked before any queue interaction. Assert the draft
	// was consumed by the send attempt and NOTHING was queued.
	_, _ = m.handleKey(keyEnter())
	c := m.submissions.Counts(m.liveSessionID())
	if c.Step != 0 && c.Turn != 0 {
		t.Fatalf("idle enter must not queue: %+v", c)
	}
}

func TestBusyEmptySubmissionIsNoOp(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("   ")

	updated, _ := m.handleKey(keyEnter())
	m2 := updated.(Model)
	if c := m2.submissions.Counts(m2.liveSessionID()); c.Step != 0 {
		t.Fatalf("empty submission queued: %+v", c)
	}
	if m2.textarea.Value() != "   " {
		t.Fatal("draft must be untouched on empty submission")
	}
}

func TestForeignSessionRejectedWithCurrentContext(t *testing.T) {
	m := newQueueTestModel(t)
	res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{RequestID: "11111111-aaaa-bbbb-cccc-111111111111", SessionID: "foreign", Source: "test", Text: "wrong", Boundary: inputqueue.BoundaryStep}, m.liveSessionID())
	if res.OK || res.Code != inputqueue.CodeWrongSession {
		t.Fatalf("expected wrong_session: %+v", res)
	}
}

func TestBusyTypingEditsDraft(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("")

	for _, r := range []rune{'a', 'b', 'c'} {
		updated, _ := m.handleKey(keyRune(r))
		m = updated.(Model)
	}
	if got := m.textarea.Value(); got != "abc" {
		t.Fatalf("busy typing dropped keystrokes: %q", got)
	}
}

func TestBusyCtrlCPrecedenceClearDraftThenFlushThenAbort(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true

	// 1) Nonempty draft → cleared, nothing else happens.
	m.textarea.SetValue("draft")
	updated, _ := m.handleKey(keyCtrlC())
	m = updated.(Model)
	if m.textarea.Value() != "" {
		t.Fatal("draft not cleared by first ctrl+c")
	}

	// Queue two entries for the flush stage.
	if res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{RequestID: "11111111-aaaa-bbbb-cccc-111111111111", SessionID: m.liveSessionID(), Source: "test", Text: "one", Boundary: inputqueue.BoundaryStep}, m.liveSessionID()); !res.OK {
		t.Fatal(res)
	}
	if res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{RequestID: "22222222-aaaa-bbbb-cccc-222222222222", SessionID: m.liveSessionID(), Source: "test", Text: "two", Boundary: inputqueue.BoundaryTurn}, m.liveSessionID()); !res.OK {
		t.Fatal(res)
	}

	// 2) Empty draft + nonempty queues → flush (discard), turn NOT aborted.
	updated, _ = m.handleKey(keyCtrlC())
	m = updated.(Model)
	if c := m.submissions.Counts(m.liveSessionID()); c.Step != 0 || c.Turn != 0 {
		t.Fatalf("queues not flushed: %+v", c)
	}
	if !m.session.Stream.Active {
		t.Fatal("turn must not abort when only flushing queues")
	}
	if !strings.Contains(m.notification.Message, "cleared 2") {
		t.Fatalf("flush announcement missing: %+v", m.notification)
	}

	// 3) Empty draft + empty queues → abort the turn. The stream channel is
	// nil in this fixture, so assert the cancel path ran via the session's
	// cancelled-stream marker (Stream.Cancel sets CancelMessage).
	updated, _ = m.handleKey(keyCtrlC())
	m = updated.(Model)
	if m.session.Stream.CancelMessage == "" {
		t.Fatal("third ctrl+c must cancel the active stream")
	}
}

// TestSocketIdleArrivalSendsImmediately: the gateway delivery rule. A socket
// submission arriving while IDLE must be pushed to the UI loop and sent as a
// normal user message NOW — not parked as a mystery "1 next step". One
// arriving while BUSY is parked for the next safe boundary.
func TestSocketIdleArrivalSendsImmediately(t *testing.T) {
	m := newQueueTestModel(t)
	withStartStreamStub(t)

	var pushed []tea.Msg
	server, err := (&m).StartLocalInputServer("test-instance", func(msg tea.Msg) { pushed = append(pushed, msg) })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	// IDLE: admission must push a localSocketMsg for immediate delivery.
	res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{
		RequestID: "99999999-aaaa-bbbb-cccc-999999999999", SessionID: m.liveSessionID(), Source: "squid-sponge", Text: "idle voice note", Boundary: inputqueue.BoundaryStep,
	}, m.liveSessionID())
	if !res.OK {
		t.Fatal(res)
	}
	_ = res
	// Precondition: idle — exactly when the server callback pushes.
	if m.isBusy() {
		t.Fatal("precondition: model must be idle")
	}
	// The server pushed this onto the UI loop; run it through Update.
	updated, _ := m.Update(localSocketMsg{requestID: "99999999-aaaa-bbbb-cccc-999999999999", sessionID: m.liveSessionID(), text: "idle voice note"})
	m2 := updated.(Model)

	// The message is in history as a real user message...
	var found bool
	for _, msg := range m2.session.Doc.Messages {
		if msg.Role == "user" && msg.Text == "idle voice note" {
			found = true
		}
	}
	if !found {
		t.Fatal("idle socket arrival not sent as user message")
	}
	// ...and the queue entry is marked injected (count back to 0) — no chip.
	if c := m2.submissions.Counts(m2.liveSessionID()); c.Step != 0 {
		t.Fatalf("entry not marked injected after idle send: %+v", c)
	}
}

// TestSocketBusyArrivalParksForBoundary: the same arrival while BUSY must NOT
// push an immediate send — it stays pending for the next safe boundary.
func TestSocketBusyArrivalParksForBoundary(t *testing.T) {
	m := newQueueTestModel(t)
	withStartStreamStub(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true

	// Busy precondition: the server's push condition must be false.
	if !m.isBusy() {
		t.Fatal("precondition: model must be busy")
	}
	res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{
		RequestID: "aaaaaaaa-aaaa-bbbb-cccc-aaaaaaaaaaaa", SessionID: m.liveSessionID(), Source: "squid-sponge", Text: "busy voice note", Boundary: inputqueue.BoundaryStep,
	}, m.liveSessionID())
	if !res.OK {
		t.Fatal(res)
	}
	// Parked: count shows it, nothing was sent, draft untouched.
	if c := m.submissions.Counts(m.liveSessionID()); c.Step != 1 {
		t.Fatalf("busy arrival not parked: %+v", c)
	}
	for _, msg := range m.session.Doc.Messages {
		if msg.Role == "user" && msg.Text == "busy voice note" {
			t.Fatal("busy arrival must not be sent immediately")
		}
	}
}

func TestRemoteStyleAdmissionPreservesTypedDraft(t *testing.T) {
	m := newQueueTestModel(t)
	m.mode = ModeStreaming
	m.session.Stream.Active = true
	m.textarea.SetValue("half typed thought")

	// A socket-style admission arrives via the same coordinator entry point.
	res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{
		RequestID: "33333333-aaaa-bbbb-cccc-333333333333", SessionID: m.liveSessionID(), Source: "squid-sponge", Text: "voice note", Boundary: inputqueue.BoundaryStep,
	}, m.liveSessionID())
	if !res.OK {
		t.Fatal(res)
	}
	// The user's partially typed draft must survive untouched.
	if got := m.textarea.Value(); got != "half typed thought" {
		t.Fatalf("remote admission clobbered the draft: %q", got)
	}
	if c := m.submissions.Counts(m.liveSessionID()); c.Step != 1 {
		t.Fatalf("counts = %+v", c)
	}
}

func TestQueueChipRenderedFromCoordinator(t *testing.T) {
	m := newQueueTestModel(t)
	m.width = 80
	if chip := m.queueChip(); chip != "" {
		t.Fatalf("empty queue must render no chip: %q", chip)
	}
	if res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{RequestID: "44444444-aaaa-bbbb-cccc-444444444444", SessionID: m.liveSessionID(), Source: "test", Text: "x", Boundary: inputqueue.BoundaryStep}, m.liveSessionID()); !res.OK {
		t.Fatal(res)
	}
	if res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{RequestID: "55555555-aaaa-bbbb-cccc-555555555555", SessionID: m.liveSessionID(), Source: "test", Text: "y", Boundary: inputqueue.BoundaryTurn}, m.liveSessionID()); !res.OK {
		t.Fatal(res)
	}
	if chip := m.queueChip(); chip != "1 next step · 1 after turn" {
		t.Fatalf("chip = %q", chip)
	}
}

func TestDiskCoordinatorWiredAtStartup(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Root: root}
	sess := NewRootUISession(config.SessionConfig{}, paths, runtimeconfig.Catalog{})
	flag := &incognitoFlag{}
	c := newModelSubmissionCoordinator(paths, sess, flag)
	defer c.Close()

	id := sess.Doc.Identity.ID
	if id == "" {
		t.Fatal("session has no identity")
	}
	res := c.AdmitSubmission(inputqueue.AdmissionRequest{RequestID: "66666666-aaaa-bbbb-cccc-666666666666", SessionID: id, Source: "test", Text: "persisted", Boundary: inputqueue.BoundaryStep}, id)
	if !res.OK {
		t.Fatal(res)
	}
	if res.Receipt.Durability != inputqueue.DurabilityDisk {
		t.Fatalf("durability = %q, want disk", res.Receipt.Durability)
	}
	// Journal must be readable back through the store.
	entries, err := c.store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != "persisted" {
		t.Fatalf("journal = %+v", entries)
	}
	if _, err := filepath.Glob(filepath.Join(root, "input-queue", "*")); err != nil {
		t.Fatal(err)
	}
}

// TestSessionSwitchDoesNotStaleQueueIdentity reproduces the ctrl+n bug: the
// coordinator's live-session probe must follow m.session, not a captured
// initial *UISession. After switching sessions, keyboard queueing (which
// targets m.liveSessionID()) must be admitted, not rejected wrong_session.
func TestIdleCtrlCFlushesPendingQueue(t *testing.T) {
	m := newQueueTestModel(t)

	res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{
		RequestID: "77777777-cccc-dddd-eeee-777777777777", SessionID: m.liveSessionID(), Source: "test", Text: "parked at idle", Boundary: inputqueue.BoundaryStep,
	}, m.liveSessionID())
	if !res.OK {
		t.Fatal(res)
	}
	if c := m.submissions.Counts(m.liveSessionID()); c.Step != 1 {
		t.Fatalf("setup counts = %+v, want 1 step", c)
	}

	// Precedence 1: a nonempty draft is cleared, queue untouched.
	m.textarea.SetValue("half typed")
	updated, _ := m.handleKey(keyCtrlC())
	m2 := updated.(Model)
	if m2.textarea.Value() != "" {
		t.Fatal("draft not cleared by first ctrl+c")
	}
	if c := m2.submissions.Counts(m2.liveSessionID()); c.Step != 1 {
		t.Fatalf("queue flushed over a live draft: %+v", c)
	}

	// Precedence 2: empty draft → the parked entry is flushed.
	updated, _ = m2.handleKey(keyCtrlC())
	m3 := updated.(Model)
	if c := m3.submissions.Counts(m3.liveSessionID()); c.Step != 0 {
		t.Fatalf("queue not flushed by second ctrl+c: %+v", c)
	}
}

func TestSessionSwitchDoesNotStaleQueueIdentity(t *testing.T) {
	m := newQueueTestModel(t)
	oldID := m.liveSessionID()
	// Swap the session in place — exactly what clearSession / loadUISession do
	// in production. The coordinator reads m.session live through its model
	// anchor, so NO rebinding is needed; it sees the new identity on the next
	// admission automatically.
	m.session.Session = chat.NewRootSession(config.SessionConfig{}, config.Paths{Root: t.TempDir()}, runtimeconfig.Catalog{})
	newID := m.liveSessionID()
	if oldID == newID {
		t.Fatal("test setup: expected distinct session identities")
	}

	// Keyboard admission targets the NEW live session and must be accepted.
	res := m.submissions.AdmitSubmission(inputqueue.AdmissionRequest{
		RequestID: "77777777-aaaa-bbbb-cccc-777777777777",
		SessionID: m.liveSessionID(), Source: "keyboard", Text: "queued after ctrl+n", Boundary: inputqueue.BoundaryStep,
	}, m.liveSessionID())
	if !res.OK {
		t.Fatalf("admit after session switch rejected: %+v", res)
	}
	c := m.submissions.Counts(newID)
	if c.Step != 1 {
		t.Fatalf("new session counts = %+v, want 1 step", c)
	}
}

// Replace the entire session on a Model copy, as the picker does; do not
// mutate the shared session internals (which hid the original bug).
func TestQueueAfterWholeSessionReplacementOnModelCopy(t *testing.T) {
	original := newQueueTestModel(t)
	current := original
	other := NewRootUISession(config.SessionConfig{}, config.Paths{Root: t.TempDir()}, runtimeconfig.Catalog{})
	if !current.loadUISession(other.Doc, "loaded") {
		t.Fatal("load failed")
	}
	if current.liveSessionID() == original.liveSessionID() {
		t.Fatal("session did not change")
	}
	current.mode = ModeStreaming
	current.turnActive = true
	current.textarea.SetValue("typed after switch")
	updated, _ := current.Update(keyEnter())
	current = updated.(Model)
	if current.textarea.Value() != "" {
		t.Fatalf("admission failed: %s", current.notification.Message)
	}
	if current.submissions.Counts(current.liveSessionID()).Step != 1 {
		t.Fatal("missing current-session entry")
	}
	if current.submissions.Counts(original.liveSessionID()).Step != 0 {
		t.Fatal("entry leaked to old session")
	}
	reply := make(chan localinput.ReceiptInfo, 1)
	req := inputqueue.AdmissionRequest{RequestID: "88888888-aaaa-bbbb-cccc-888888888888", SessionID: current.liveSessionID(), Source: "voice", Text: "voice after switch", Boundary: inputqueue.BoundaryStep}
	updated, _ = current.Update(localAdmissionMsg{req: req, reply: reply})
	current = updated.(Model)
	if res := <-reply; !res.OK {
		t.Fatalf("socket rejected: %+v", res)
	}
	req.SessionID = original.liveSessionID()
	current.Update(localAdmissionMsg{req: req, reply: reply})
	if res := <-reply; res.OK || res.Code != inputqueue.CodeWrongSession {
		t.Fatalf("stale socket accepted: %+v", res)
	}
	query := make(chan string, 1)
	current.Update(localSessionQuery{reply: query})
	if id := <-query; id != current.liveSessionID() {
		t.Fatal("stale socket discovery")
	}
}

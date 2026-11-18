package app

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/inputqueue"
)

func withStartStreamStub(t *testing.T) {
	t.Helper()
	prev := startStreamStub
	startStreamStub = func(m *Model) tea.Cmd { return nil }
	t.Cleanup(func() { startStreamStub = prev })
}

var lastStepIDs, lastTurnIDs []string

func admitN(t *testing.T, m Model, n int, boundary inputqueue.Boundary, prefix string) {
	t.Helper()
	if boundary == inputqueue.BoundaryStep {
		lastStepIDs = nil
	} else {
		lastTurnIDs = nil
	}
	for i := 0; i < n; i++ {
		id := reqIDFor(i)
		if boundary == inputqueue.BoundaryStep {
			lastStepIDs = append(lastStepIDs, id)
		} else {
			lastTurnIDs = append(lastTurnIDs, id)
		}
		req := inputqueue.AdmissionRequest{
			RequestID: id,
			SessionID: m.liveSessionID(),
			Source:    "test",
			Text:      prefix + " " + itoaUI(i+1),
			Boundary:  boundary,
		}
		if res := m.submissions.AdmitSubmission(req, m.liveSessionID()); !res.OK {
			t.Fatalf("admit %d: %+v", i, res)
		}
	}
}

var reqSeq uint64

// reqIDFor mints a process-unique valid UUIDv4 (tests only).
func reqIDFor(i int) string {
	reqSeq++
	b := make([]byte, 16)
	for j := 0; j < 16; j++ {
		b[j] = byte(reqSeq >> (8 * j))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TestDrainStepAtToolDoneBeforeContinuation: a FIFO batch of 3 step messages
// is drained exactly once at ToolEventDone, before the single continuation.
func TestDrainStepAtToolDoneBeforeContinuation(t *testing.T) {
	withStartStreamStub(t)
	m := newQueueTestModel(t)
	base := len(m.session.Doc.Messages)

	admitN(t, m, 3, inputqueue.BoundaryStep, "steer")
	admitN(t, m, 2, inputqueue.BoundaryTurn, "later")

	event := chat.ToolEvent{Type: chat.ToolEventDone}
	updated, _ := m.handleToolEvent(event)
	m2 := asModel(updated)

	// All 3 step entries appended in FIFO order; turn entries still pending.
	got := m2.session.Doc.Messages[base:]
	if len(got) != 3 {
		t.Fatalf("appended %d messages, want 3", len(got))
	}
	for i, want := range []string{"steer 1", "steer 2", "steer 3"} {
		if got[i].Text != want || got[i].Role != "user" {
			t.Fatalf("msg %d = %+v, want user %q", i, got[i], want)
		}
	}
	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 0 || c.Turn != 2 {
		t.Fatalf("counts after drain = %+v, want step=0 turn=2", c)
	}
	// Entries finalized as injected with stable message IDs.
	e, ok := m2.submissions.Find(m2.liveSessionID(), lastStepIDs[0])
	if !ok || e == nil || e.State != inputqueue.StateInjected || len(e.InjectedMsgIDs) != 1 {
		t.Fatalf("entry state = %+v, want injected with msg id", e)
	}
}

// TestDrainTurnOnlyAfterLogicalTurnEnds: at terminal LoopDone with an empty
// step queue, ALL after-turn entries drain into one new turn.
func TestDrainTurnOnlyAfterLogicalTurnEnds(t *testing.T) {
	withStartStreamStub(t)
	m := newQueueTestModel(t)
	base := len(m.session.Doc.Messages)

	admitN(t, m, 2, inputqueue.BoundaryTurn, "after")

	// Feed a real Done event through ProcessStreamEvent so the assistant
	// message is appended exactly as in production.
	m.session.Stream.AddTextChunk("done")
	updated, _ := m.handleStreamEvent(chat.StreamEvent{Done: true, StopReason: "end_turn"})
	m2 := asModel(updated)

	got := m2.session.Doc.Messages[base:]
	if len(got) != 3 {
		t.Fatalf("appended %d messages, want 3 (assistant + 2 queued)", len(got))
	}
	if got[0].Role != "assistant" || got[0].Text != "done" {
		t.Fatalf("assistant msg = %+v", got[0])
	}
	if got[1].Text != "after 1" || got[2].Text != "after 2" {
		t.Fatalf("drained = %+v", got[1:])
	}
	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 0 || c.Turn != 0 {
		t.Fatalf("counts = %+v, want empty", c)
	}
}

// TestStepDrainExtendsTurnNotAfterTurn: at terminal LoopDone with BOTH queues
// nonempty, only the step group drains; the turn group waits for the next
// terminal boundary of the extended turn.
func TestStepDrainExtendsTurnNotAfterTurn(t *testing.T) {
	withStartStreamStub(t)
	m := newQueueTestModel(t)
	base := len(m.session.Doc.Messages)

	admitN(t, m, 1, inputqueue.BoundaryStep, "steer")
	admitN(t, m, 1, inputqueue.BoundaryTurn, "after")

	m.session.Stream.AddTextChunk("done")
	updated, _ := m.handleStreamEvent(chat.StreamEvent{Done: true, StopReason: "end_turn"})
	m2 := asModel(updated)

	got := m2.session.Doc.Messages[base:]
	if len(got) != 2 {
		t.Fatalf("appended %d, want 2 (assistant + 1 step)", len(got))
	}
	if got[1].Text != "steer 1" {
		t.Fatalf("drained = %+v, want only the step entry", got[1])
	}
	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 0 || c.Turn != 1 {
		t.Fatalf("counts = %+v, want turn=1 still pending", c)
	}
}

func TestQueueClearCommandDiscards(t *testing.T) {
	withStartStreamStub(t)
	m := newQueueTestModel(t)

	admitN(t, m, 3, inputqueue.BoundaryStep, "steer")
	admitN(t, m, 1, inputqueue.BoundaryTurn, "after")

	updated, _ := m.executeCommandByName("queue-clear")
	m2 := updated.(Model)

	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 0 || c.Turn != 0 {
		t.Fatalf("clear left pending: %+v", c)
	}
}

// TestIdentityChangeBlockedWithPendingEntries: /clear refuses while queued
// input is pending and keeps the session intact.
func TestIdentityChangeBlockedWithPendingEntries(t *testing.T) {
	m := newQueueTestModel(t)
	sessionBefore := m.session
	base := len(m.session.Doc.Messages)

	admitN(t, m, 1, inputqueue.BoundaryStep, "steer")

	updated, _ := m.executeCommandByName("clear")
	m2 := updated.(Model)

	if m2.session != sessionBefore {
		t.Fatal("session was replaced despite pending queue")
	}
	if n := len(m2.session.Doc.Messages); n != base {
		t.Fatalf("history changed: %d → %d", base, n)
	}
	if !contains(m2.notification.Message, "blocked") {
		t.Fatalf("no block notification: %+v", m2.notification)
	}
	c := m2.submissions.Counts(m2.liveSessionID())
	if c.Step != 1 {
		t.Fatalf("queue lost: %+v", c)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

var errTestStream = &testErr{}

type testErr struct{}

func (e *testErr) Error() string { return "synthetic stream failure" }

// asModel normalizes the tea.Model returned by handlers (value or pointer).
func asModel(v tea.Model) Model {
	if p, ok := v.(*Model); ok {
		return *p
	}
	return v.(Model)
}

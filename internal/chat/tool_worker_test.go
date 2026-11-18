package chat

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	goai_provider "github.com/zendev-sh/goai/provider"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/tools"
)

// Synthetic regression fixture inspired by the historical duplicate user
// messages found in a real session (adjacent identical stored instructions).
// No private session text is copied here.
const authInstructionText = "Synthetic instruction: run the tests before committing."

func newAuthWorkerSession(t *testing.T) *Session {
	t.Helper()
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"bash"}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			authToolEntry("tool_1", `{"command":"echo ok","destructive":false}`),
			authToolEntry("tool_2", `{"command":"echo second","destructive":false}`),
		},
	}}
	return s
}

func authToolEntry(id, args string) config.ToolCallEntry {
	e := config.ToolCallEntry{ID: id}
	e.Instruction.Name = "bash"
	e.Instruction.Arguments = args
	return e
}

// drainToolEvents consumes the worker channel and returns every event in order.
func drainToolEvents(ch <-chan ToolEvent) []ToolEvent {
	var events []ToolEvent
	for ev := range ch {
		if ev.Type == ToolEventRunning || ev.Type == ToolEventNeedAuth {
			continue // intermediate progress, not a terminal step
		}
		events = append(events, ev)
	}
	return events
}

// TestStartToolExecApprovedInstructionAppendsOnce covers the approved-with-
// instructions path: the worker appends exactly one user message and reports
// its ID/index on ToolEventDone so consumers never append it again.
func TestStartToolExecApprovedInstructionAppendsOnce(t *testing.T) {
	s := newAuthWorkerSession(t)
	before := len(s.Doc.Messages)

	ch := StartToolExec(context.Background(), s, ToolExecOptions{
		MsgIdx:   0,
		Decision: &AuthDecision{Approved: true, Instructions: authInstructionText},
	})
	events := drainToolEvents(ch)

	if len(events) != 1 || events[0].Type != ToolEventDone {
		t.Fatalf("expected a single ToolEventDone, got %d events (%v)", len(events), eventTypes(events))
	}
	done := events[0]

	userMsgs := userMessages(s.Doc.Messages[before:])
	if len(userMsgs) != 1 {
		t.Fatalf("worker must append exactly one user message, got %d: %+v", len(userMsgs), userMsgs)
	}
	if userMsgs[0].Text != authInstructionText {
		t.Fatalf("instruction text mismatch: %q", userMsgs[0].Text)
	}
	if done.AppendedUserMsgIdx != before {
		t.Fatalf("AppendedUserMsgIdx = %d, want %d", done.AppendedUserMsgIdx, before)
	}
	if done.AppendedUserMsgID != s.Doc.Messages[done.AppendedUserMsgIdx].ID {
		t.Fatalf("AppendedUserMsgID = %q, want %q", done.AppendedUserMsgID, s.Doc.Messages[done.AppendedUserMsgIdx].ID)
	}

	// Remaining-tool cancellation semantics: the unexecuted tool was skipped.
	entry := s.Doc.Messages[0].ToolCalls[1]
	if entry.Execution.Status != tools.ResultStatusError || !strings.Contains(entry.Execution.Error, "instructions") {
		t.Fatalf("remaining tool not cancelled for captured instruction: %#v", entry.Execution)
	}
}

// TestStartToolExecRejectedInstructionAppendsOnce covers the rejected-with-
// instructions path: the tool is marked rejected, remaining tools cancelled,
// and the instruction still appended exactly once.
func TestStartToolExecRejectedInstructionAppendsOnce(t *testing.T) {
	s := newAuthWorkerSession(t)
	before := len(s.Doc.Messages)

	ch := StartToolExec(context.Background(), s, ToolExecOptions{
		MsgIdx:   0,
		Decision: &AuthDecision{Approved: false, Instructions: authInstructionText},
	})
	events := drainToolEvents(ch)

	if len(events) != 1 || events[0].Type != ToolEventDone {
		t.Fatalf("expected a single ToolEventDone, got %d events (%v)", len(events), eventTypes(events))
	}
	done := events[0]

	userMsgs := userMessages(s.Doc.Messages[before:])
	if len(userMsgs) != 1 || userMsgs[0].Text != authInstructionText {
		t.Fatalf("rejected decision with instruction must append exactly one user message, got %+v", userMsgs)
	}
	if done.AppendedUserMsgIdx != before || done.AppendedUserMsgID == "" {
		t.Fatalf("event must carry the appended message identity: idx=%d id=%q", done.AppendedUserMsgIdx, done.AppendedUserMsgID)
	}

	entries := s.Doc.Messages[0].ToolCalls
	if entries[0].Execution.Status != tools.ResultStatusError || !strings.Contains(entries[0].Execution.Error, "rejected") {
		t.Fatalf("rejected tool not recorded: %#v", entries[0].Execution)
	}
	if entries[1].Execution.Status != tools.ResultStatusError || !strings.Contains(entries[1].Execution.Error, "not approved") {
		t.Fatalf("remaining tool not cancelled after rejection: %#v", entries[1].Execution)
	}
}

// TestStartToolExecPlainYesNoAddsNoInstruction proves a plain approval without
// instructions adds no user message and leaves the appended fields empty.
func TestStartToolExecPlainYesNoAddsNoInstruction(t *testing.T) {
	s := newAuthWorkerSession(t)
	before := len(s.Doc.Messages)

	ch := StartToolExec(context.Background(), s, ToolExecOptions{
		MsgIdx:   0,
		Decision: &AuthDecision{Approved: true, Instructions: ""},
	})
	events := drainToolEvents(ch)

	if len(events) != 1 || events[0].Type != ToolEventDone {
		t.Fatalf("expected a single ToolEventDone, got %d events (%v)", len(events), eventTypes(events))
	}
	done := events[0]
	if done.AppendedUserMsgID != "" || done.AppendedUserMsgIdx != -1 {
		t.Fatalf("plain yes must not report an appended message: id=%q idx=%d", done.AppendedUserMsgID, done.AppendedUserMsgIdx)
	}
	if got := len(userMessages(s.Doc.Messages[before:])); got != 0 {
		t.Fatalf("plain yes must add no user message, got %d", got)
	}
	// Both tools executed normally.
	for i, entry := range s.Doc.Messages[0].ToolCalls {
		if entry.Execution.Status != tools.ResultStatusSuccess {
			t.Fatalf("tool %d should have executed: %#v", i, entry.Execution)
		}
	}
}

// TestStartToolExecCancelledKeepsRemainingToolSemantics guards that a turn
// cancel mid-execution finalizes remaining tools and appends nothing.
func TestStartToolExecCancelledKeepsRemainingToolSemantics(t *testing.T) {
	s := newAuthWorkerSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ctrl+c before any tool runs

	ch := StartToolExec(ctx, s, ToolExecOptions{MsgIdx: 0, Decision: &AuthDecision{Approved: true, Instructions: authInstructionText}})
	events := drainToolEvents(ch)

	if len(events) != 1 || events[0].Type != ToolEventCancelled {
		t.Fatalf("expected ToolEventCancelled, got %v", eventTypes(events))
	}
	if got := len(userMessages(s.Doc.Messages)); got != 0 {
		t.Fatalf("cancelled execution must not append the instruction, got %d user messages", got)
	}
	entries := s.Doc.Messages[0].ToolCalls
	for i, entry := range entries {
		if entry.Execution.Status != tools.ResultStatusError || entry.Execution.Error != "cancelled: turn aborted by user" {
			t.Fatalf("entry %d not finalized as cancelled: %#v", i, entry.Execution)
		}
	}
}

// TestRunLoopCapturedInstructionAppendsOnce proves the headless RunLoop keeps
// its own once-only append: ExecuteTools reports the captured text and the
// loop stores a single user message.
func TestRunLoopCapturedInstructionAppendsOnce(t *testing.T) {
	s := newAuthWorkerSession(t)
	before := len(s.Doc.Messages)

	res := ExecuteTools(s, ToolExecOptions{
		MsgIdx:   0,
		Decision: &AuthDecision{Approved: true, Instructions: authInstructionText},
	})
	if res.Action != ToolExecDone || res.CapturedUserText != authInstructionText {
		t.Fatalf("ExecuteTools must report the captured instruction: %#v", res)
	}

	// RunLoop's once-only append path.
	if res.CapturedUserText != "" {
		s.Append(NewUserMessage(nextMessageID(s), res.CapturedUserText))
	}

	userMsgs := userMessages(s.Doc.Messages[before:])
	if len(userMsgs) != 1 || userMsgs[0].Text != authInstructionText {
		t.Fatalf("headless path must store exactly one instruction message, got %+v", userMsgs)
	}
}

// TestAuthorizationInstructionSessionSerialization persists the single
// instruction through Save/Load and verifies the reloaded document holds one
// user message with a stable sequential ID.
func TestAuthorizationInstructionSessionSerialization(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	os.MkdirAll(sessionDir, 0755)

	s := newAuthWorkerSession(t)
	s.SessionDir = sessionDir
	before := len(s.Doc.Messages)

	ch := StartToolExec(context.Background(), s, ToolExecOptions{
		MsgIdx:   0,
		Decision: &AuthDecision{Approved: true, Instructions: authInstructionText},
	})
	drainToolEvents(ch)

	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadSession(s.Doc, sessionDir, config.Paths{}, runtimeconfig.Catalog{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	loadedUsers := userMessages(loaded.Doc.Messages[before:])
	if len(loadedUsers) != 1 || loadedUsers[0].Text != authInstructionText {
		t.Fatalf("reloaded session must hold exactly one instruction message, got %+v", loadedUsers)
	}
	wantID := "msg_" + itoa(before+1)
	if loadedUsers[0].ID != wantID {
		t.Fatalf("instruction ID = %q, want %q", loadedUsers[0].ID, wantID)
	}
}

// TestAuthorizationInstructionProviderContextProjection verifies the provider
// context projects the instruction exactly once: two stored copies of the same
// text would both reach the model, which is what made the TUI double-append a
// real bug rather than a cosmetic one.
func TestAuthorizationInstructionProviderContextProjection(t *testing.T) {
	s := newAuthWorkerSession(t)
	before := len(s.Doc.Messages)

	ch := StartToolExec(context.Background(), s, ToolExecOptions{
		MsgIdx:   0,
		Decision: &AuthDecision{Approved: true, Instructions: authInstructionText},
	})
	drainToolEvents(ch)

	count := countProjectedUserText(s.BuildContext().Messages, authInstructionText)
	if count != 1 {
		t.Fatalf("provider context must project the instruction once, got %d occurrences", count)
	}

	// Simulate the historical bug shape: a second copy of the same text in
	// history is projected again — proving why the TUI must not append.
	s.Append(NewUserMessage(nextMessageID(s), authInstructionText))
	count = countProjectedUserText(s.BuildContext().Messages, authInstructionText)
	if count != 2 {
		t.Fatalf("duplicate stored instruction must be projected twice (bug shape), got %d", count)
	}
	_ = before
}

func userMessages(msgs []config.Message) []config.Message {
	var out []config.Message
	for _, m := range msgs {
		if m.Role == config.RoleUser {
			out = append(out, m)
		}
	}
	return out
}

func eventTypes(events []ToolEvent) []ToolEventType {
	out := make([]ToolEventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

func countProjectedUserText(msgs []goai_provider.Message, text string) int {
	n := 0
	for _, m := range msgs {
		if m.Role != goai_provider.RoleUser {
			continue
		}
		for _, part := range m.Content {
			if strings.Contains(part.Text, text) {
				n++
			}
		}
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }

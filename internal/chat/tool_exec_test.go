package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/tools"
	"squid-os/internal/util"
)

func TestShouldAuthorizeModes(t *testing.T) {
	destructive := &tools.Tool{IsDestructive: func(map[string]interface{}) bool { return true }}
	readOnly := &tools.Tool{IsDestructive: func(map[string]interface{}) bool { return false }}
	tests := []struct {
		mode config.AuthorizationMode
		tool *tools.Tool
		want bool
	}{
		{config.AuthorizationAuto, destructive, false},
		{config.AuthorizationAskOnWrite, destructive, true},
		{config.AuthorizationEndOnWrite, destructive, true},
		{config.AuthorizationAskOnWrite, readOnly, false},
		{config.AuthorizationEndOnWrite, readOnly, false},
		{config.AuthorizationAskForAll, readOnly, true},
		{config.AuthorizationEndOnAll, readOnly, true},
	}
	for _, test := range tests {
		if got := shouldAuthorize(test.mode, test.tool, nil); got != test.want {
			t.Fatalf("mode=%q got=%v want=%v", test.mode, got, test.want)
		}
	}
}

func TestEnforceToolResultTokenLimit(t *testing.T) {
	t.Run("small success passes", func(t *testing.T) {
		res := tools.ToolResult{Status: tools.ResultStatusSuccess, Result: "small"}
		got := enforceToolResultTokenLimit(res, 10)
		if got.Status != tools.ResultStatusSuccess || got.Result != "small" || got.Error != "" {
			t.Fatalf("unexpected result: %#v", got)
		}
	})

	t.Run("large success becomes error", func(t *testing.T) {
		big := strings.Repeat("a", 100)
		res := tools.ToolResult{Status: tools.ResultStatusSuccess, Result: big}
		got := enforceToolResultTokenLimit(res, 10)
		if got.Status != tools.ResultStatusError {
			t.Fatalf("expected error status, got %#v", got)
		}
		if got.Result != "" {
			t.Fatalf("expected dropped result, got %q", got.Result)
		}
		if !strings.Contains(got.Error, "Tool result too large") {
			t.Fatalf("expected size error, got %q", got.Error)
		}
	})

	t.Run("large error becomes size error", func(t *testing.T) {
		big := strings.Repeat("b", 100)
		res := tools.ToolResult{Status: tools.ResultStatusError, Error: big}
		got := enforceToolResultTokenLimit(res, 10)
		if got.Status != tools.ResultStatusError {
			t.Fatalf("expected error status, got %#v", got)
		}
		if got.Error == big {
			t.Fatalf("expected original oversized error to be dropped")
		}
	})

	t.Run("default applies when zero", func(t *testing.T) {
		res := tools.ToolResult{Status: tools.ResultStatusSuccess, Result: strings.Repeat("a", 100)}
		got := enforceToolResultTokenLimit(res, 0)
		if got.Status != tools.ResultStatusSuccess {
			t.Fatalf("expected default limit to allow small content, got %#v", got)
		}
	})
}

func TestExecuteToolsRejectsToolOutsideSessionScope(t *testing.T) {
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"open"}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_1",
			Type: "function",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "read_file", Arguments: `{"path":"x"}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.Status != tools.ResultStatusError || !strings.Contains(exec.Error, "unknown tool") {
		t.Fatalf("out-of-scope tool executed: %#v", exec)
	}
}

func TestExecuteToolsStoresOnlySizeErrorForOversizedResult(t *testing.T) {
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"read_file"}
	s.Doc.Config.Limits.MaxToolResultTokens = 10
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_1",
			Type: "function",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "read_file", Arguments: `{"path":"` + strings.Repeat("x", 200) + `"}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.Status != tools.ResultStatusError {
		t.Fatalf("expected error status, got %#v", exec)
	}
	if exec.Result != "" {
		t.Fatalf("expected oversized result to be dropped, got %q", exec.Result)
	}
	if !strings.Contains(exec.Error, "Tool result too large") {
		t.Fatalf("expected size error message, got %q", exec.Error)
	}
}

func TestExecuteToolsFullReadSkipsValidation(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	// Track an old checksum — full read should NOT be blocked
	oldChecksum := util.ComputeChecksum([]byte("different content"))

	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{
		file: {Checksum: oldChecksum, Trace: config.TraceRead},
	}}}
	s.Doc.Config.Tools = []string{"read_file"}
	s.Doc.Config.WorkingDir = dir
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_1",
			Type: "function",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "read_file", Arguments: `{"path":"test.txt"}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.Status != tools.ResultStatusSuccess {
		t.Fatalf("expected success for full read (should skip validation), got error: %s", exec.Error)
	}
	// Full read should refresh the checksum in file state
	if s.Doc.FileState[file].Checksum != util.ComputeChecksum([]byte(content)) {
		t.Fatalf("expected refreshed checksum in file state")
	}
}

// TestFinalizeCancelledToolsMarksRemainingEntries proves that after a turn
// cancel, no entry is left pending/running: the killed tool keeps its own
// diagnostic and every unexecuted tool gets the cancellation note.
func TestFinalizeCancelledToolsMarksRemainingEntries(t *testing.T) {
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	mk := func(id, status, err string) config.ToolCallEntry {
		e := config.ToolCallEntry{ID: id}
		e.Execution.Status = status
		e.Execution.Error = err
		return e
	}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			mk("tool_1", tools.ResultStatusSuccess, ""),
			mk("tool_2", tools.ResultStatusError, "exit code: signal: killed"), // killed mid-run
			mk("tool_3", tools.ResultStatusPending, ""),
			mk("tool_4", "", ""),
		},
	}}

	finalizeCancelledTools(s, 0, nil)

	entries := s.Doc.Messages[0].ToolCalls
	if entries[0].Execution.Status != tools.ResultStatusSuccess {
		t.Fatalf("completed entry must be untouched: %#v", entries[0])
	}
	if entries[1].Execution.Error != "exit code: signal: killed" {
		t.Fatalf("killed entry must keep its own diagnostic: %#v", entries[1])
	}
	for i := 2; i < len(entries); i++ {
		if entries[i].Execution.Status != tools.ResultStatusError || entries[i].Execution.Error != "cancelled: turn aborted by user" {
			t.Fatalf("entry %d not marked cancelled: %#v", i, entries[i])
		}
	}
}

func TestStartToolExecCancelFinalizesRemainingTools(t *testing.T) {
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	mk := func(id, name, status string) config.ToolCallEntry {
		e := config.ToolCallEntry{ID: id}
		e.Instruction.Name = name
		e.Execution.Status = status
		return e
	}
	// First tool is already running (its child was killed out-of-band via the
	// turn context); two more are queued behind it.
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			mk("tool_1", "bash", tools.ResultStatusRunning),
			mk("tool_2", "read_file", tools.ResultStatusPending),
			mk("tool_3", "read_file", tools.ResultStatusPending),
		},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	ch := StartToolExec(ctx, s, ToolExecOptions{MsgIdx: 0})
	cancel() // simulate ctrl+c: worker ctx done before any tool runs

	var event ToolEvent
	for ev := range ch {
		event = ev
	}
	if event.Type != ToolEventCancelled {
		t.Fatalf("expected ToolEventCancelled, got %v", event.Type)
	}
	entries := s.Doc.Messages[0].ToolCalls
	if entries[0].Execution.Status != tools.ResultStatusError || entries[0].Execution.Error == "" {
		t.Fatalf("running entry not finalized: %#v", entries[0])
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Execution.Status != tools.ResultStatusError || entries[i].Execution.Error != "cancelled: turn aborted by user" {
			t.Fatalf("entry %d left dangling: %#v", i, entries[i])
		}
	}
}

func TestStartToolExecDerivesFromTurnContext(t *testing.T) {
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	mk := func(id, name, status string) config.ToolCallEntry {
		e := config.ToolCallEntry{ID: id}
		e.Instruction.Name = name
		e.Execution.Status = status
		return e
	}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			mk("tool_1", "bash", tools.ResultStatusRunning),
			mk("tool_2", "read_file", tools.ResultStatusPending),
		},
	}}

	// Simulate the TUI wiring: worker ctx derives from the turn ctx.
	s.BeginTurnCtx(nil)
	turnCtx := s.TurnContext()

	ch := StartToolExec(turnCtx, s, ToolExecOptions{MsgIdx: 0})
	s.CancelTurnCtx() // single cancel source — must stop the loop and finalize

	var event ToolEvent
	for ev := range ch {
		event = ev
	}
	if event.Type != ToolEventCancelled {
		t.Fatalf("expected ToolEventCancelled, got %v", event.Type)
	}
	entries := s.Doc.Messages[0].ToolCalls
	if entries[0].Execution.Status != tools.ResultStatusError || entries[0].Execution.Error == "" {
		t.Fatalf("running entry not finalized: %#v", entries[0])
	}
	if entries[1].Execution.Error != "cancelled: turn aborted by user" {
		t.Fatalf("pending entry left dangling: %#v", entries[1])
	}
}


// TestExecuteToolsCancelMidToolFinalizesAndReportsCancelled simulates ctrl+c
// landing while a tool is executing: the turn ctx dies mid-bash, the killed
// child returns an error, and ExecuteTools must finalize the remaining
// entries and report Cancelled so no new model turn re-issues the work.
func TestExecuteToolsCancelMidToolFinalizesAndReportsCancelled(t *testing.T) {
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"bash"}
	mk := func(id, name, args, status string) config.ToolCallEntry {
		e := config.ToolCallEntry{ID: id}
		e.Instruction.Name = name
		e.Instruction.Arguments = args
		e.Execution.Status = status
		return e
	}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			mk("tool_1", "bash", `{"command":"sleep 30","destructive":false}`, tools.ResultStatusPending),
			mk("tool_2", "bash", `{"command":"echo done","destructive":false}`, tools.ResultStatusPending),
		},
	}}
	s.BeginTurnCtx(nil)

	// Cancel the turn ~50ms in: lands while tool 1's bash is still sleeping.
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.CancelTurnCtx()
	}()

	start := time.Now()
	res := ExecuteTools(s, ToolExecOptions{MsgIdx: 0})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("ExecuteTools did not return promptly after cancel (took %v)", time.Since(start))
	}

	if !res.Cancelled {
		t.Fatalf("expected Cancelled result when turn ctx dies mid-tool, got %#v", res)
	}
	entries := s.Doc.Messages[0].ToolCalls
	if entries[0].Execution.Status != tools.ResultStatusError || entries[0].Execution.Error == "" {
		t.Fatalf("killed entry not finalized: %#v", entries[0])
	}
	if entries[1].Execution.Status != tools.ResultStatusError || entries[1].Execution.Error != "cancelled: turn aborted by user" {
		t.Fatalf("pending entry left dangling: %#v", entries[1])
	}
}

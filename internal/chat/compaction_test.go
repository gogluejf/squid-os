package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/tools"
)

// Helper: build a ToolCallEntry for read_file with optional range
func tcRead(tcID, path string, startLine, endLine *int, isSuccessful bool, instrTokens, execTokens int) config.ToolCallEntry {
	args := map[string]interface{}{"path": path}
	if startLine != nil {
		args["start_line"] = *startLine
	}
	if endLine != nil {
		args["end_line"] = *endLine
	}
	argsJSON, _ := json.Marshal(args)

	tc := config.ToolCallEntry{
		ID:   tcID,
		Type: "function",
		Instruction: struct {
			Name       string `json:"name"`
			Arguments  string `json:"arguments"`
			Tokens     int    `json:"tokens,omitempty"`
			DurationMs int64  `json:"duration_ms,omitempty"`
		}{Name: "read_file", Arguments: string(argsJSON), Tokens: instrTokens},
	}

	if isSuccessful {
		tc.Execution.Status = tools.ResultStatusSuccess
		tc.Execution.Result = "file content"
		tc.Execution.Tokens = execTokens
		tc.Execution.Files = []config.FileEntry{
			{Path: path, Trace: config.TraceRead, Checksum: "abc123", Time: time.Now()},
		}
	} else {
		tc.Execution.Status = tools.ResultStatusError
		tc.Execution.Error = "file not found"
	}

	return tc
}

// Helper: build a ToolCallEntry for write_file
func tcWrite(tcID, path string, isCreate bool, isSuccessful bool, instrTokens, execTokens int) config.ToolCallEntry {
	argsJSON, _ := json.Marshal(map[string]interface{}{"path": path, "content": "new content"})
	trace := config.TraceWrite
	if isCreate {
		trace = config.TraceCreate
	}

	tc := config.ToolCallEntry{
		ID:   tcID,
		Type: "function",
		Instruction: struct {
			Name       string `json:"name"`
			Arguments  string `json:"arguments"`
			Tokens     int    `json:"tokens,omitempty"`
			DurationMs int64  `json:"duration_ms,omitempty"`
		}{Name: "write_file", Arguments: string(argsJSON), Tokens: instrTokens},
	}

	if isSuccessful {
		tc.Execution.Status = tools.ResultStatusSuccess
		tc.Execution.Result = fmt.Sprintf("file written: %s", path)
		tc.Execution.Tokens = execTokens
		tc.Execution.Files = []config.FileEntry{
			{Path: path, Trace: trace, Checksum: "def456", Time: time.Now()},
		}
	} else {
		tc.Execution.Status = tools.ResultStatusError
		tc.Execution.Error = "write failed"
	}

	return tc
}

// Helper: build a ToolCallEntry for edit_file (with optional no-op read trace)
func tcEdit(tcID, path string, isNoOp bool, isSuccessful bool, instrTokens, execTokens int) config.ToolCallEntry {
	argsJSON, _ := json.Marshal(map[string]interface{}{"path": path, "old_string": "old", "new_string": "new"})

	tc := config.ToolCallEntry{
		ID:   tcID,
		Type: "function",
		Instruction: struct {
			Name       string `json:"name"`
			Arguments  string `json:"arguments"`
			Tokens     int    `json:"tokens,omitempty"`
			DurationMs int64  `json:"duration_ms,omitempty"`
		}{Name: "edit_file", Arguments: string(argsJSON), Tokens: instrTokens},
	}

	if isSuccessful {
		tc.Execution.Status = tools.ResultStatusSuccess
		if isNoOp {
			tc.Execution.Result = "no changes made"
			tc.Execution.Files = []config.FileEntry{
				{Path: path, Trace: config.TraceRead, Checksum: "abc123", Time: time.Now()},
			}
		} else {
			tc.Execution.Result = fmt.Sprintf("replaced in %s", path)
			tc.Execution.Files = []config.FileEntry{
				{Path: path, Trace: config.TraceEdit, Checksum: "ghi789", Time: time.Now()},
			}
		}
		tc.Execution.Tokens = execTokens
	} else {
		tc.Execution.Status = tools.ResultStatusError
		tc.Execution.Error = "edit failed"
	}

	return tc
}

// Helper: build a message with tool calls
func msgWithTools(msgID string, toolCalls []config.ToolCallEntry) config.Message {
	return config.Message{
		ID:        msgID,
		Role:      config.RoleAssistant,
		CreatedAt: time.Now(),
		ToolCalls: toolCalls,
	}
}

// Helper: assert decision properties
func assertDecision(t *testing.T, plan CompactionPlan, tcID string, wantCompactArgs, wantCompactResult bool, wantSupersededBy string) {
	t.Helper()
	d, ok := plan.Decisions[tcID]
	if !ok {
		t.Fatalf("decision missing for tool call %s", tcID)
	}
	if d.CompactArguments != wantCompactArgs {
		t.Errorf("tool %s: CompactArguments = %v, want %v", tcID, d.CompactArguments, wantCompactArgs)
	}
	if d.CompactResult != wantCompactResult {
		t.Errorf("tool %s: CompactResult = %v, want %v", tcID, d.CompactResult, wantCompactResult)
	}
	if d.SupersededBy != wantSupersededBy {
		t.Errorf("tool %s: SupersededBy = %q, want %q", tcID, d.SupersededBy, wantSupersededBy)
	}
}

// Helper: assert token totals
func assertTokens(t *testing.T, tokens CompactionTokens, rawInstr, rawExec, retainedInstr, retainedExec, savedInstr, savedExec int) {
	t.Helper()
	if tokens.RawInstruction != rawInstr {
		t.Errorf("RawInstruction = %d, want %d", tokens.RawInstruction, rawInstr)
	}
	if tokens.RawExecution != rawExec {
		t.Errorf("RawExecution = %d, want %d", tokens.RawExecution, rawExec)
	}
	if tokens.Raw != rawInstr+rawExec {
		t.Errorf("Raw = %d, want %d", tokens.Raw, rawInstr+rawExec)
	}
	if tokens.RetainedInstruction != retainedInstr {
		t.Errorf("RetainedInstruction = %d, want %d", tokens.RetainedInstruction, retainedInstr)
	}
	if tokens.RetainedExecution != retainedExec {
		t.Errorf("RetainedExecution = %d, want %d", tokens.RetainedExecution, retainedExec)
	}
	if tokens.Retained != retainedInstr+retainedExec {
		t.Errorf("Retained = %d, want %d", tokens.Retained, retainedInstr+retainedExec)
	}
	if tokens.SavedInstruction != savedInstr {
		t.Errorf("SavedInstruction = %d, want %d", tokens.SavedInstruction, savedInstr)
	}
	if tokens.SavedExecution != savedExec {
		t.Errorf("SavedExecution = %d, want %d", tokens.SavedExecution, savedExec)
	}
	if tokens.Saved != savedInstr+savedExec {
		t.Errorf("Saved = %d, want %d", tokens.Saved, savedInstr+savedExec)
	}
	if tokens.RawInstruction != tokens.RetainedInstruction+tokens.SavedInstruction {
		t.Errorf("RawInstruction invariant failed: raw=%d retained=%d saved=%d", tokens.RawInstruction, tokens.RetainedInstruction, tokens.SavedInstruction)
	}
	if tokens.RawExecution != tokens.RetainedExecution+tokens.SavedExecution {
		t.Errorf("RawExecution invariant failed: raw=%d retained=%d saved=%d", tokens.RawExecution, tokens.RetainedExecution, tokens.SavedExecution)
	}
	if tokens.Raw != tokens.Retained+tokens.Saved {
		t.Errorf("Raw invariant failed: raw=%d retained=%d saved=%d", tokens.Raw, tokens.Retained, tokens.Saved)
	}
}

// Test: Empty messages produce empty plan
func TestCompactionEmpty(t *testing.T) {
	plan := BuildCompactionPlan([]config.Message{})
	if len(plan.Decisions) != 0 {
		t.Fatalf("expected 0 decisions, got %d", len(plan.Decisions))
	}
	if plan.Tokens.Raw != 0 {
		t.Fatalf("expected 0 tokens, got %d", plan.Tokens.Raw)
	}
}

// Test: Non-file tools are ignored
func TestCompactionNonFileTools(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			{
				ID:   "tc_bash",
				Type: "function",
				Instruction: struct {
					Name       string `json:"name"`
					Arguments  string `json:"arguments"`
					Tokens     int    `json:"tokens,omitempty"`
					DurationMs int64  `json:"duration_ms,omitempty"`
				}{Name: "bash", Arguments: `{"command":"ls"}`, Tokens: 10},
				Execution: struct {
					Status     string             `json:"status,omitempty"`
					Result     string             `json:"result,omitempty"`
					Error      string             `json:"error,omitempty"`
					Tokens     int                `json:"tokens,omitempty"`
					DurationMs int64              `json:"duration_ms"`
					Files      []config.FileEntry `json:"files,omitempty"`
				}{Status: tools.ResultStatusSuccess, Result: "output", Tokens: 20},
			},
		}),
	}

	plan := BuildCompactionPlan(messages)
	if len(plan.Decisions) != 0 {
		t.Fatalf("expected 0 decisions for non-file tools, got %d", len(plan.Decisions))
	}
}

// Test: Single full read is a checkpoint — retained, nothing compacted
func TestCompactionSingleFullRead(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", false, false, "")
	assertTokens(t, plan.Tokens, 10, 20, 10, 20, 0, 0)
}

// Test: Single ranged read is not a checkpoint — retained
func TestCompactionSingleRangedRead(t *testing.T) {
	sl, el := 1, 10
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", &sl, &el, true, 10, 20),
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", false, false, "")
	assertTokens(t, plan.Tokens, 10, 20, 10, 20, 0, 0)
}

// Test: Full read followed by another full read — first compacted, second retained
func TestCompactionFullReadSupersedesEarlierFullRead(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 10, 20),
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", true, true, "tc2")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 20, 40, 10, 20, 10, 20)
}

// Test: Ranged read never supersedes earlier events
func TestCompactionRangedReadNeverSupersedes(t *testing.T) {
	sl, el := 1, 10
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20), // full read
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", &sl, &el, true, 5, 10), // ranged read
		}),
	}

	plan := BuildCompactionPlan(messages)
	// tc1 is the only checkpoint; tc2 (ranged) comes after it, so both retained
	assertDecision(t, plan, "tc1", false, false, "")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 15, 30, 15, 30, 0, 0)
}

// Test: Ranged reads before a full read are compacted
func TestCompactionFullReadCompactsEarlierRangedReads(t *testing.T) {
	sl1, el1 := 1, 5
	sl2, el2 := 10, 20
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", &sl1, &el1, true, 5, 10), // ranged
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", &sl2, &el2, true, 5, 10), // ranged
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcRead("tc3", "/file.go", nil, nil, true, 10, 20), // full read (checkpoint)
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", true, true, "tc3")
	assertDecision(t, plan, "tc2", true, true, "tc3")
	assertDecision(t, plan, "tc3", false, false, "")
	assertTokens(t, plan.Tokens, 20, 40, 10, 20, 10, 20)
}

// Test: Write is a checkpoint — compacts earlier reads
func TestCompactionWriteCompactsEarlierReads(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcWrite("tc2", "/file.go", false, true, 15, 10),
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", true, true, "tc2")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 25, 30, 15, 10, 10, 20)
}

// Test: Create is a checkpoint — compacts earlier reads
func TestCompactionCreateCompactsEarlierReads(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/newfile.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcWrite("tc2", "/newfile.go", true, true, 15, 10),
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", true, true, "tc2")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 25, 30, 15, 10, 10, 20)
}

// Test: edit_file is a delta — never a checkpoint
func TestCompactionEditIsDelta(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20), // full read (checkpoint)
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcEdit("tc2", "/file.go", false, true, 15, 10), // edit (delta)
		}),
	}

	plan := BuildCompactionPlan(messages)
	// tc1 is the only checkpoint; tc2 comes after, so both retained
	assertDecision(t, plan, "tc1", false, false, "")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 25, 30, 25, 30, 0, 0)
}

// Test: No-op edit_file (trace=read) is not a checkpoint
func TestCompactionNoOpEditNotCheckpoint(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcEdit("tc1", "/file.go", true, true, 15, 10), // no-op edit (trace=read)
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 10, 20), // full read (checkpoint)
		}),
	}

	plan := BuildCompactionPlan(messages)
	// tc1 (no-op edit) is before checkpoint tc2 — compacted
	// tc2 is the latest checkpoint — retained
	assertDecision(t, plan, "tc1", true, true, "tc2")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 25, 30, 10, 20, 15, 10)
}

// Test: Failed operations are retained, not compacted
func TestCompactionFailedOpsRetained(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, false, 10, 5), // failed read
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 10, 20), // successful full read (checkpoint)
		}),
	}

	plan := BuildCompactionPlan(messages)
	// tc1 failed — retained even though before checkpoint
	assertDecision(t, plan, "tc1", false, false, "")
	assertDecision(t, plan, "tc2", false, false, "")
	// tc1 is retained (failed, exec tokens=0), tc2 is retained (checkpoint)
	assertTokens(t, plan.Tokens, 20, 20, 20, 20, 0, 0)
}

// Test: Multiple paths are handled independently
func TestCompactionMultiplePaths(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/a.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/b.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcRead("tc3", "/a.go", nil, nil, true, 10, 20), // checkpoint for /a.go
		}),
		msgWithTools("msg4", []config.ToolCallEntry{
			tcRead("tc4", "/b.go", nil, nil, true, 10, 20), // checkpoint for /b.go
		}),
	}

	plan := BuildCompactionPlan(messages)
	// /a.go: tc1 compacted by tc3, tc3 retained
	assertDecision(t, plan, "tc1", true, true, "tc3")
	assertDecision(t, plan, "tc3", false, false, "")
	// /b.go: tc2 compacted by tc4, tc4 retained
	assertDecision(t, plan, "tc2", true, true, "tc4")
	assertDecision(t, plan, "tc4", false, false, "")
	assertTokens(t, plan.Tokens, 40, 80, 20, 40, 20, 40)
}

// Test: Events after latest checkpoint are retained
func TestCompactionEventsAfterCheckpointRetained(t *testing.T) {
	sl, el := 1, 10
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20), // checkpoint
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", &sl, &el, true, 5, 10), // ranged read after checkpoint
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcEdit("tc3", "/file.go", false, true, 15, 10), // edit after checkpoint
		}),
	}

	plan := BuildCompactionPlan(messages)
	assertDecision(t, plan, "tc1", false, false, "")
	assertDecision(t, plan, "tc2", false, false, "")
	assertDecision(t, plan, "tc3", false, false, "")
	assertTokens(t, plan.Tokens, 30, 40, 30, 40, 0, 0)
}

// Test: Range-edit-range sequence — ranges before full read are compacted
func TestCompactionRangeEditRangeFullRead(t *testing.T) {
	sl1, el1 := 1, 5
	sl2, el2 := 10, 20
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", &sl1, &el1, true, 5, 10), // ranged
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcEdit("tc2", "/file.go", false, true, 15, 10), // edit
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcRead("tc3", "/file.go", &sl2, &el2, true, 5, 10), // ranged
		}),
		msgWithTools("msg4", []config.ToolCallEntry{
			tcRead("tc4", "/file.go", nil, nil, true, 10, 20), // full read (checkpoint)
		}),
	}

	plan := BuildCompactionPlan(messages)
	// tc1, tc2, tc3 are before latest checkpoint (tc4) — all compacted
	assertDecision(t, plan, "tc1", true, true, "tc4")
	assertDecision(t, plan, "tc2", true, true, "tc4")
	assertDecision(t, plan, "tc3", true, true, "tc4")
	assertDecision(t, plan, "tc4", false, false, "")
	assertTokens(t, plan.Tokens, 35, 50, 10, 20, 25, 30)
}

// Test: Planner does not mutate input messages
func TestCompactionDoesNotMutateMessages(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 10, 20),
		}),
	}

	// Capture pre-compaction state
	beforeArgs := messages[0].ToolCalls[0].Instruction.Arguments
	beforeResult := messages[0].ToolCalls[0].Execution.Result

	plan := BuildCompactionPlan(messages)
	if !plan.Decisions["tc1"].CompactArguments {
		t.Fatal("tc1 should be compacted")
	}

	// Verify messages are unchanged
	if messages[0].ToolCalls[0].Instruction.Arguments != beforeArgs {
		t.Error("BuildCompactionPlan mutated Instruction.Arguments")
	}
	if messages[0].ToolCalls[0].Execution.Result != beforeResult {
		t.Error("BuildCompactionPlan mutated Execution.Result")
	}
}

// Test: Deterministic — running twice on same input produces same plan
func TestCompactionDeterministic(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcWrite("tc3", "/file.go", false, true, 15, 10),
		}),
	}

	plan1 := BuildCompactionPlan(messages)
	plan2 := BuildCompactionPlan(messages)

	if len(plan1.Decisions) != len(plan2.Decisions) {
		t.Fatalf("decision count differs: %d vs %d", len(plan1.Decisions), len(plan2.Decisions))
	}

	for id := range plan1.Decisions {
		d1 := plan1.Decisions[id]
		d2 := plan2.Decisions[id]
		if d1.CompactArguments != d2.CompactArguments || d1.CompactResult != d2.CompactResult || d1.SupersededBy != d2.SupersededBy {
			t.Errorf("decision for %s differs between runs: %+v vs %+v", id, d1, d2)
		}
	}

	if plan1.Tokens != plan2.Tokens {
		t.Errorf("tokens differ between runs: %+v vs %+v", plan1.Tokens, plan2.Tokens)
	}
}

// Test: Mixed interleaved paths with failures
func TestCompactionInterleavedPaths(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/a.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/b.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcRead("tc3", "/a.go", nil, nil, false, 10, 5), // failed read for /a.go
		}),
		msgWithTools("msg4", []config.ToolCallEntry{
			tcWrite("tc4", "/b.go", false, true, 15, 10), // checkpoint for /b.go
		}),
		msgWithTools("msg5", []config.ToolCallEntry{
			tcRead("tc5", "/a.go", nil, nil, true, 10, 20), // checkpoint for /a.go
		}),
	}

	plan := BuildCompactionPlan(messages)
	// /a.go: tc1 compacted (before tc5), tc3 retained (failed), tc5 retained (checkpoint)
	assertDecision(t, plan, "tc1", true, true, "tc5")
	assertDecision(t, plan, "tc3", false, false, "") // failed — retained
	assertDecision(t, plan, "tc5", false, false, "")
	// /b.go: tc2 compacted (before tc4), tc4 retained (checkpoint)
	assertDecision(t, plan, "tc2", true, true, "tc4")
	assertDecision(t, plan, "tc4", false, false, "")

	// Tokens: tc1 compacted (10+20), tc2 compacted (10+20)
	// Retained: tc3(10+0, failed exec=0), tc4(15+10), tc5(10+20) = (35, 30)
	assertTokens(t, plan.Tokens, 55, 70, 35, 30, 20, 40)
}

// Test: Decision map has direct lookup by tool-call ID
func TestCompactionDirectLookupByToolCallID(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 10, 20),
		}),
	}

	plan := BuildCompactionPlan(messages)

	// Direct lookup
	d, ok := plan.Decisions["tc1"]
	if !ok {
		t.Fatal("tc1 decision missing from map")
	}
	if !d.CompactArguments {
		t.Error("tc1 should be compacted")
	}

	d, ok = plan.Decisions["tc2"]
	if !ok {
		t.Fatal("tc2 decision missing from map")
	}
	if d.CompactArguments {
		t.Error("tc2 should not be compacted")
	}
}

// Test: Failed write is not a checkpoint
func TestCompactionFailedWriteNotCheckpoint(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcWrite("tc2", "/file.go", false, false, 15, 5), // failed write
		}),
	}

	plan := BuildCompactionPlan(messages)
	// tc1 is a successful checkpoint, tc2 failed (exec tokens=0)
	assertDecision(t, plan, "tc1", false, false, "")
	assertDecision(t, plan, "tc2", false, false, "")
	assertTokens(t, plan.Tokens, 25, 20, 25, 20, 0, 0)
}

// ---------------------------------------------------------------------------
// Fixture-driven tests from testdata/file_compaction_cases.json
// Shared with Python Chat Analytics tests for cross-implementation parity.
// ---------------------------------------------------------------------------

// fixtureEvent describes a single file-tool event in the shared fixture.
type fixtureEvent struct {
	ID                string `json:"id"`
	Tool              string `json:"tool"`
	Path              string `json:"path"`
	CanonicalPath     string `json:"canonical_path"`
	Trace             string `json:"trace"`
	IsRanged          bool   `json:"is_ranged"`
	Status            string `json:"status"`
	InstructionTokens int    `json:"instruction_tokens"`
	ExecutionTokens   int    `json:"execution_tokens"`
}

// fixtureExpected describes the expected compaction outcome for a scenario.
type fixtureExpected struct {
	CompactedEvents            int `json:"compacted_events"`
	RetainedEvents             int `json:"retained_events"`
	CompactedInstructionTokens int `json:"compacted_instruction_tokens"`
	CompactedExecutionTokens   int `json:"compacted_execution_tokens"`
	CompactedTokens            int `json:"compacted_tokens"`
	RetainedInstructionTokens  int `json:"retained_instruction_tokens"`
	RetainedExecutionTokens    int `json:"retained_execution_tokens"`
	RetainedTokens             int `json:"retained_tokens"`
}

// fixtureCase describes a single compaction scenario.
type fixtureCase struct {
	Name     string          `json:"name"`
	Events   []fixtureEvent  `json:"events"`
	Expected fixtureExpected `json:"expected"`
}

// fixtureFile is the top-level shared fixture.
type fixtureFile struct {
	Cases []fixtureCase `json:"cases"`
}

// fixtureToolCall converts a fixture event to a Go ToolCallEntry.
func fixtureToolCall(ev fixtureEvent) config.ToolCallEntry {
	args := map[string]interface{}{"path": ev.Path}
	if ev.IsRanged && ev.Tool == "read_file" {
		args["start_line"] = 1
		args["end_line"] = 10
	}
	argsJSON, _ := json.Marshal(args)

	tc := config.ToolCallEntry{
		ID:   ev.ID,
		Type: "function",
	}

	switch ev.Tool {
	case "read_file":
		tc.Instruction.Name = "read_file"
	case "write_file":
		args["content"] = "new content"
		argsJSON, _ = json.Marshal(args)
		tc.Instruction.Name = "write_file"
	case "edit_file":
		args["old_string"] = "old"
		args["new_string"] = "new"
		argsJSON, _ = json.Marshal(args)
		tc.Instruction.Name = "edit_file"
	}
	tc.Instruction.Arguments = string(argsJSON)
	tc.Instruction.Tokens = ev.InstructionTokens

	isSuccess := ev.Status == "success"
	if isSuccess {
		tc.Execution.Status = tools.ResultStatusSuccess
		tc.Execution.Result = fmt.Sprintf("result for %s", ev.Path)
		tc.Execution.Tokens = ev.ExecutionTokens
		filePath := ev.CanonicalPath
		if filePath == "" {
			filePath = ev.Path
		}
		trace := ev.Trace
		if trace == "" {
			switch ev.Tool {
			case "read_file":
				trace = config.TraceRead
			case "write_file":
				trace = config.TraceWrite
			case "edit_file":
				trace = config.TraceEdit
			}
		}
		tc.Execution.Files = []config.FileEntry{
			{Path: filePath, Trace: trace, Checksum: "abc123", Time: time.Now()},
		}
	} else {
		tc.Execution.Status = tools.ResultStatusError
		tc.Execution.Error = "operation failed"
	}

	return tc
}

// TestCompactionFixture runs every scenario from the shared fixture.
func TestCompactionFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "file_compaction_cases.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var fixture fixtureFile
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}

	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			// Build messages from fixture events — one message per event
			var messages []config.Message
			for _, ev := range tc.Events {
				tcEntry := fixtureToolCall(ev)
				messages = append(messages, msgWithTools("msg_"+ev.ID, []config.ToolCallEntry{tcEntry}))
			}

			plan := BuildCompactionPlan(messages)

			// Count compacted and retained events
			compactedEvents := 0
			retainedEvents := 0
			for _, decision := range plan.Decisions {
				if decision.CompactArguments {
					compactedEvents++
				} else {
					retainedEvents++
				}
			}

			if compactedEvents != tc.Expected.CompactedEvents {
				t.Errorf("compacted_events = %d, want %d", compactedEvents, tc.Expected.CompactedEvents)
			}
			if retainedEvents != tc.Expected.RetainedEvents {
				t.Errorf("retained_events = %d, want %d", retainedEvents, tc.Expected.RetainedEvents)
			}

			// Verify token totals
			// In the Go planner, Raw = Saved (all compacted event tokens count as raw+saved)
			// Retained = tokens of non-compacted events
			if plan.Tokens.SavedInstruction != tc.Expected.CompactedInstructionTokens {
				t.Errorf("saved_instruction_tokens = %d, want %d", plan.Tokens.SavedInstruction, tc.Expected.CompactedInstructionTokens)
			}
			if plan.Tokens.SavedExecution != tc.Expected.CompactedExecutionTokens {
				t.Errorf("saved_execution_tokens = %d, want %d", plan.Tokens.SavedExecution, tc.Expected.CompactedExecutionTokens)
			}
			if plan.Tokens.Saved != tc.Expected.CompactedTokens {
				t.Errorf("saved_tokens = %d, want %d", plan.Tokens.Saved, tc.Expected.CompactedTokens)
			}
			if plan.Tokens.RetainedInstruction != tc.Expected.RetainedInstructionTokens {
				t.Errorf("retained_instruction_tokens = %d, want %d", plan.Tokens.RetainedInstruction, tc.Expected.RetainedInstructionTokens)
			}
			if plan.Tokens.RetainedExecution != tc.Expected.RetainedExecutionTokens {
				t.Errorf("retained_execution_tokens = %d, want %d", plan.Tokens.RetainedExecution, tc.Expected.RetainedExecutionTokens)
			}
			if plan.Tokens.Retained != tc.Expected.RetainedTokens {
				t.Errorf("retained_tokens = %d, want %d", plan.Tokens.Retained, tc.Expected.RetainedTokens)
			}

			if plan.Tokens.RawInstruction != plan.Tokens.RetainedInstruction+plan.Tokens.SavedInstruction {
				t.Errorf("raw instruction invariant failed: %+v", plan.Tokens)
			}
			if plan.Tokens.RawExecution != plan.Tokens.RetainedExecution+plan.Tokens.SavedExecution {
				t.Errorf("raw execution invariant failed: %+v", plan.Tokens)
			}
			if plan.Tokens.Raw != plan.Tokens.Retained+plan.Tokens.Saved {
				t.Errorf("raw invariant failed: %+v", plan.Tokens)
			}
		})
	}
}

func TestCompactionStartLineOneWithoutEndIsFullCheckpoint(t *testing.T) {
	first := tcRead("tc1", "/file.go", nil, nil, true, 10, 20)
	second := tcRead("tc2", "/file.go", nil, nil, true, 10, 20)
	second.Instruction.Arguments = `{"path":"/file.go","start_line":1}`
	plan := BuildCompactionPlan([]config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{first}),
		msgWithTools("msg2", []config.ToolCallEntry{second}),
	})
	if !plan.Decisions["tc1"].CompactResult {
		t.Fatal("start_line=1 without end_line should be a full checkpoint")
	}
	if plan.Decisions["tc2"].CompactResult {
		t.Fatal("latest full checkpoint must be retained")
	}
}

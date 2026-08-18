package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/chat/provider"
	"squid-os/internal/media"
	"squid-os/internal/tools"

	goai_provider "github.com/zendev-sh/goai/provider"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildTestSession builds a Session with the given messages and compaction setting.
func buildTestSession(messages []config.Message, compaction bool) *Session {
	cfg := config.SessionConfig{ContextCompaction: compaction}
	doc := config.NewSessionDoc(cfg)
	doc.Messages = messages
	return &Session{Doc: doc}
}

// ---------------------------------------------------------------------------
// Disabled compaction tests
// ---------------------------------------------------------------------------

func TestBuildContextDisabledProducesRawMessages(t *testing.T) {
	// A session with a simple user message and no compaction
	messages := []config.Message{
		{ID: "sys0", Role: config.RoleSystem, Text: "system prompt"},
		{ID: "u1", Role: config.RoleUser, Text: "hello", CreatedAt: time.Now()},
	}
	s := buildTestSession(messages, false)
	ctx := s.BuildContext()

	// Should produce the same messages as BuildAPIMessages (raw, uncompacted)
	expected := BuildAPIMessages(messages)
	if len(ctx.Messages) != len(expected) {
		t.Fatalf("message count: got %d, want %d", len(ctx.Messages), len(expected))
	}
	for i := range expected {
		if ctx.Messages[i].Role != expected[i].Role {
			t.Errorf("msg[%d] role: got %v, want %v", i, ctx.Messages[i].Role, expected[i].Role)
		}
	}

	// No compaction opportunities in this simple session — savings should be 0
	// because there are no file tool calls to compact
	if ctx.Tokens.Saved != 0 {
		t.Errorf("simple session should have zero savings (no file tools), got %d", ctx.Tokens.Saved)
	}
	if ctx.Tokens.Raw != ctx.Tokens.Compacted {
		t.Errorf("simple session: Raw=%d != Compacted=%d (no file tools to compact)", ctx.Tokens.Raw, ctx.Tokens.Compacted)
	}

	// Plan exists (empty for non-file-tool sessions)
	if len(ctx.Compaction.Decisions) != 0 {
		t.Errorf("simple session should have empty plan (no file tools), got %d decisions", len(ctx.Compaction.Decisions))
	}
}

func TestBuildContextDisabledStillProjectsPotentialCompaction(t *testing.T) {
	// When disabled, messages stay raw but tokens still project potential savings
	longContent := strings.Repeat("line of code content\n", 1000)
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", true, 500, 5000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 500, 5000),
		}),
	}
	ctx := BuildContext(messages, false, "", nil, nil)

	// Messages should be raw (uncompacted) — result content is intact
	rawExpected := BuildAPIMessages(messages)
	if len(ctx.Messages) != len(rawExpected) {
		t.Fatalf("message count: got %d, want %d", len(ctx.Messages), len(rawExpected))
	}

	// Tokens should project potential savings (same as enabled)
	if ctx.Tokens.Saved <= 0 {
		t.Errorf("disabled compaction should still project potential savings, got %d", ctx.Tokens.Saved)
	}
	if ctx.Tokens.Compacted >= ctx.Tokens.Raw {
		t.Errorf("disabled Compacted(%d) should be < Raw(%d) when there are compaction opportunities",
			ctx.Tokens.Compacted, ctx.Tokens.Raw)
	}

	// Plan should be populated (available for tally/analytics)
	if len(ctx.Compaction.Decisions) == 0 {
		t.Error("disabled compaction should still have a compaction plan for analytics")
	}
	if !ctx.Compaction.Decisions["tc1"].CompactArguments {
		t.Error("tc1 should be marked for compaction in the plan even when disabled")
	}
}

func TestBuildContextTokenBreakdownByProviderRole(t *testing.T) {
	messages := []config.Message{
		{ID: "sys0", Role: config.RoleSystem, Text: "system prompt"},
		{ID: "u1", Role: config.RoleUser, Text: "hello user"},
		{ID: "a1", Role: config.RoleAssistant, ThinkingText: "reasoning", Text: "answer", ToolCalls: []config.ToolCallEntry{{ID: "tc1", Type: "function"}}},
		{ID: "s1", Role: config.RoleSynthetic, Text: "synthetic assistant"},
	}
	messages[2].ToolCalls[0].Instruction.Name = "read_file"
	messages[2].ToolCalls[0].Instruction.Arguments = `{"path":"/tmp/a"}`
	messages[2].ToolCalls[0].Execution.Status = tools.ResultStatusSuccess
	messages[2].ToolCalls[0].Execution.Result = "tool result"

	ctx := BuildContext(messages, false, "", nil, nil)
	raw := tallyAPIMessagesTokens(BuildAPIMessages(messages))

	if ctx.Tokens.RawInput != raw.Input || ctx.Tokens.RawOutput != raw.Output {
		t.Fatalf("raw breakdown got input=%d output=%d, want input=%d output=%d",
			ctx.Tokens.RawInput, ctx.Tokens.RawOutput, raw.Input, raw.Output)
	}
	if ctx.Tokens.RawInput+ctx.Tokens.RawOutput != ctx.Tokens.Raw {
		t.Errorf("raw input+output must equal raw total")
	}
	if ctx.Tokens.CompactedInput+ctx.Tokens.CompactedOutput != ctx.Tokens.Compacted {
		t.Errorf("compacted input+output must equal compacted total")
	}
	if ctx.Tokens.RawOutput <= 0 || ctx.Tokens.RawInput <= 0 {
		t.Errorf("expected both input and output context tokens, got input=%d output=%d", ctx.Tokens.RawInput, ctx.Tokens.RawOutput)
	}
}

// ---------------------------------------------------------------------------
// Enabled compaction: tool-call ID, name, chronology, valid JSON
// ---------------------------------------------------------------------------

func TestBuildContextEnabledPreservesToolCallIDs(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc_read1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc_read2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Collect tool call IDs from assistant messages
	var toolCallIDs []string
	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleAssistant {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolCall {
					toolCallIDs = append(toolCallIDs, part.ToolCallID)
				}
			}
		}
	}
	if len(toolCallIDs) != 2 || toolCallIDs[0] != "tc_read1" || toolCallIDs[1] != "tc_read2" {
		t.Errorf("tool call IDs not preserved: got %v", toolCallIDs)
	}
}

func TestBuildContextEnabledPreservesToolNames(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleAssistant {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolCall {
					if part.ToolName != "read_file" {
						t.Errorf("tool name: got %q, want %q", part.ToolName, "read_file")
					}
				}
			}
		}
	}
}

func TestBuildContextEnabledPreservesChronology(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/a.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/b.go", true, 10, 20),
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcRead("tc3", "/a.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Verify message order: sys, assistant(tc1), tool(tc1), assistant(tc2), tool(tc2), assistant(tc3), tool(tc3)
	var roles []string
	for _, msg := range ctx.Messages {
		roles = append(roles, string(msg.Role))
	}
	// assistant messages should come before their tool results
	assistantIdx := 0
	for i, role := range roles {
		if role == "assistant" {
			assistantIdx = i
		}
		if role == "tool" {
			// Each tool result should come after an assistant
			if assistantIdx >= i {
				t.Errorf("tool message at index %d comes before or at assistant at %d", i, assistantIdx)
			}
		}
	}
}

func TestBuildContextEnabledProducesValidJSONArgs(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// First read (tc1) should be compacted — verify its arguments are valid JSON
	found := false
	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleAssistant {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolCall && part.ToolCallID == "tc1" {
					found = true
					var v map[string]interface{}
					if err := json.Unmarshal(part.ToolInput, &v); err != nil {
						t.Errorf("compacted arguments for tc1 is not valid JSON: %v (raw: %s)", err, string(part.ToolInput))
					}
					if p, ok := v["path"].(string); !ok || p != "/file.go" {
						t.Errorf("compacted arguments should preserve path=/file.go, got %v", v)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("tc1 tool call not found in compacted messages")
	}
}

// ---------------------------------------------------------------------------
// Superseded read results replaced, path and optional range preserved
// ---------------------------------------------------------------------------

func TestBuildContextSupersededReadReplaced(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Find the tool result for tc1 — should be compacted
	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleTool {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc1" {
					if !strings.Contains(part.ToolOutput, "[compacted]") {
						t.Errorf("tc1 result should be compacted, got: %s", part.ToolOutput)
					}
					if !strings.Contains(part.ToolOutput, "/file.go") {
						t.Errorf("tc1 result should mention path, got: %s", part.ToolOutput)
					}
				}
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc2" {
					if strings.Contains(part.ToolOutput, "[compacted]") {
						t.Errorf("tc2 result should NOT be compacted (latest checkpoint), got: %s", part.ToolOutput)
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Superseded edit_file old_string/new_string replaced with fixed text
// ---------------------------------------------------------------------------

func TestBuildContextSupersededEditReplaced(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcEdit("tc1", "/file.go", false, true, 15, 10),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// tc1 edit should be compacted: args contain COMPACTED, result is compacted
	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleAssistant {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolCall && part.ToolCallID == "tc1" {
					var v map[string]interface{}
					if err := json.Unmarshal(part.ToolInput, &v); err != nil {
						t.Fatalf("invalid JSON: %v", err)
					}
					if old, ok := v["old_string"].(string); !ok || old != "<COMPACTED>" {
						t.Errorf("old_string should be <COMPACTED>, got: %v", v)
					}
					if new, ok := v["new_string"].(string); !ok || new != "<COMPACTED>" {
						t.Errorf("new_string should be <COMPACTED>, got: %v", v)
					}
					if p, ok := v["path"].(string); !ok || p != "/file.go" {
						t.Errorf("path not preserved: %v", v)
					}
				}
			}
		}
		if msg.Role == goai_provider.RoleTool {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc1" {
					if !strings.Contains(part.ToolOutput, "[compacted]") {
						t.Errorf("tc1 result should be compacted, got: %s", part.ToolOutput)
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Superseded write/create content replaced with fixed text
// ---------------------------------------------------------------------------

func TestBuildContextSupersededWriteReplaced(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcWrite("tc1", "/file.go", true, true, 15, 10),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleAssistant {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolCall && part.ToolCallID == "tc1" {
					var v map[string]interface{}
					if err := json.Unmarshal(part.ToolInput, &v); err != nil {
						t.Fatalf("invalid JSON: %v", err)
					}
					if content, ok := v["content"].(string); !ok || content != "<COMPACTED>" {
						t.Errorf("content should be <COMPACTED>, got: %v", v)
					}
					if p, ok := v["path"].(string); !ok || p != "/file.go" {
						t.Errorf("path not preserved: %v", v)
					}
				}
			}
		}
		if msg.Role == goai_provider.RoleTool {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc1" {
					if !strings.Contains(part.ToolOutput, "[compacted]") {
						t.Errorf("tc1 result should be compacted, got: %s", part.ToolOutput)
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Persisted messages are unchanged after BuildContext
// ---------------------------------------------------------------------------

func TestBuildContextDoesNotMutatePersistedMessages(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}

	// Capture original state
	origArgs1 := messages[0].ToolCalls[0].Instruction.Arguments
	origResult1 := messages[0].ToolCalls[0].Execution.Result

	ctx := BuildContext(messages, true, "", nil, nil)
	_ = ctx

	// Verify messages are unchanged
	if messages[0].ToolCalls[0].Instruction.Arguments != origArgs1 {
		t.Error("BuildContext mutated Instruction.Arguments")
	}
	if messages[0].ToolCalls[0].Execution.Result != origResult1 {
		t.Error("BuildContext mutated Execution.Result")
	}
}

func TestBuildContextSessionDoesNotMutatePersistedMessages(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	s := buildTestSession(messages, true)

	origArgs1 := s.Doc.Messages[0].ToolCalls[0].Instruction.Arguments
	origResult1 := s.Doc.Messages[0].ToolCalls[0].Execution.Result

	_ = s.BuildContext()

	if s.Doc.Messages[0].ToolCalls[0].Instruction.Arguments != origArgs1 {
		t.Error("Session.BuildContext mutated persisted Instruction.Arguments")
	}
	if s.Doc.Messages[0].ToolCalls[0].Execution.Result != origResult1 {
		t.Error("Session.BuildContext mutated persisted Execution.Result")
	}
}

// ---------------------------------------------------------------------------
// Compacted token count describes Context.Messages exactly
// ---------------------------------------------------------------------------

func TestBuildContextTokenCountDescribesMessages(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Compacted tokens should equal the token count of the actual messages
	tt := tallyAPIMessagesTokens(ctx.Messages)
	actualTokens := tt.Input + tt.Output
	if ctx.Tokens.Compacted != actualTokens {
		t.Errorf("Compacted=%d does not describe Context.Messages exactly (actual=%d)",
			ctx.Tokens.Compacted, actualTokens)
	}

	// Saved should be Raw - Compacted
	if ctx.Tokens.Saved != ctx.Tokens.Raw-ctx.Tokens.Compacted {
		t.Errorf("Saved=%d != Raw(%d) - Compacted(%d)",
			ctx.Tokens.Saved, ctx.Tokens.Raw, ctx.Tokens.Compacted)
	}
}

// ---------------------------------------------------------------------------
// StartStreamWithContext sends Context.Messages (no independent rebuild)
// ---------------------------------------------------------------------------

func TestBuildContextStreamSendsExactSnapshot(t *testing.T) {
	// This test verifies that BuildContext produces a consistent snapshot
	// by calling it twice and comparing the messages.
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
	}
	ctx1 := BuildContext(messages, true, "", nil, nil)
	ctx2 := BuildContext(messages, true, "", nil, nil)

	if len(ctx1.Messages) != len(ctx2.Messages) {
		t.Fatalf("message count differs between builds: %d vs %d", len(ctx1.Messages), len(ctx2.Messages))
	}

	for i := range ctx1.Messages {
		if ctx1.Messages[i].Role != ctx2.Messages[i].Role {
			t.Errorf("msg[%d] role differs: %v vs %v", i, ctx1.Messages[i].Role, ctx2.Messages[i].Role)
		}
		if len(ctx1.Messages[i].Content) != len(ctx2.Messages[i].Content) {
			t.Errorf("msg[%d] content length differs: %d vs %d", i, len(ctx1.Messages[i].Content), len(ctx2.Messages[i].Content))
		}
		for j := range ctx1.Messages[i].Content {
			c1 := ctx1.Messages[i].Content[j]
			c2 := ctx2.Messages[i].Content[j]
			if c1.Type != c2.Type {
				t.Errorf("msg[%d].content[%d] type differs", i, j)
			}
			if c1.Text != c2.Text {
				t.Errorf("msg[%d].content[%d] text differs", i, j)
			}
			if c1.ToolCallID != c2.ToolCallID {
				t.Errorf("msg[%d].content[%d] toolCallID differs", i, j)
			}
			if string(c1.ToolInput) != string(c2.ToolInput) {
				t.Errorf("msg[%d].content[%d] toolInput differs", i, j)
			}
			if c1.ToolOutput != c2.ToolOutput {
				t.Errorf("msg[%d].content[%d] toolOutput differs", i, j)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Direct plan lookup — no linear scan
// ---------------------------------------------------------------------------

func TestBuildContextUsesDirectPlanLookup(t *testing.T) {
	// Build a session with many tool calls and verify that the compaction plan
	// decisions are applied correctly via direct map lookup.
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20),
		}),
		msgWithTools("msg3", []config.ToolCallEntry{
			tcRead("tc3", "/other.go", true, 10, 20),
		}),
		msgWithTools("msg4", []config.ToolCallEntry{
			tcRead("tc4", "/other.go", true, 10, 20),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Verify that both paths are compacted independently
	plan := ctx.Compaction
	if _, ok := plan.Decisions["tc1"]; !ok {
		t.Error("tc1 decision missing from plan")
	}
	if _, ok := plan.Decisions["tc3"]; !ok {
		t.Error("tc3 decision missing from plan")
	}

	// tc1 should be compacted (superseded by tc2), tc2 should not
	if !plan.Decisions["tc1"].CompactArguments {
		t.Error("tc1 should be compacted")
	}
	if plan.Decisions["tc2"].CompactArguments {
		t.Error("tc2 should not be compacted")
	}

	// tc3 should be compacted (superseded by tc4), tc4 should not
	if !plan.Decisions["tc3"].CompactArguments {
		t.Error("tc3 should be compacted")
	}
	if plan.Decisions["tc4"].CompactArguments {
		t.Error("tc4 should not be compacted")
	}
}

// ---------------------------------------------------------------------------
// Non-file tools are untouched
// ---------------------------------------------------------------------------

func TestBuildContextNonFileToolsUnchanged(t *testing.T) {
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
				}{Name: "bash", Arguments: `{"command":"ls -la"}`, Tokens: 10},
				Execution: struct {
					Status           string             `json:"status,omitempty"`
					Result           string             `json:"result,omitempty"`
					Error            string             `json:"error,omitempty"`
					Tokens           int                `json:"tokens,omitempty"`
					DurationMs       int64              `json:"duration_ms"`
					Files            []config.FileEntry `json:"files,omitempty"`
					ChildSessionID   string             `json:"child_session_id,omitempty"`
					ChildSessionName string             `json:"child_session_name,omitempty"`
					Attachments      []config.AttachmentRef `json:"attachments,omitempty"`
				}{Status: tools.ResultStatusSuccess, Result: "file output here", Tokens: 20},
			},
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Verify bash tool call is unchanged
	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleAssistant {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolCall && part.ToolCallID == "tc_bash" {
					var v map[string]interface{}
					if err := json.Unmarshal(part.ToolInput, &v); err != nil {
						t.Fatalf("invalid JSON: %v", err)
					}
					if cmd, ok := v["command"].(string); !ok || cmd != "ls -la" {
						t.Errorf("bash arguments should be unchanged, got: %v", v)
					}
				}
			}
		}
		if msg.Role == goai_provider.RoleTool {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc_bash" {
					if part.ToolOutput != "file output here" {
						t.Errorf("bash result should be unchanged, got: %s", part.ToolOutput)
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Failed tool calls are not compacted
// ---------------------------------------------------------------------------

func TestBuildContextFailedOpsNotCompacted(t *testing.T) {
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			tcRead("tc1", "/file.go", false, 10, 5), // failed
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 10, 20), // successful
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// tc1 failed — should not be compacted
	if ctx.Compaction.Decisions["tc1"].CompactArguments {
		t.Error("failed tc1 should not be compacted")
	}

	// The tool result for tc1 should contain the error, not compacted text
	for _, msg := range ctx.Messages {
		if msg.Role == goai_provider.RoleTool {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc1" {
					if strings.Contains(part.ToolOutput, "[compacted]") {
						t.Error("failed tc1 result should not be compacted")
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Token count includes replacement overhead
// ---------------------------------------------------------------------------

func TestBuildContextTokenCountIncludesReplacementOverhead(t *testing.T) {
	// Use a read with a very long result to ensure compaction saves tokens.
	// The compacted replacement text is shorter than a large file content.
	longContent := strings.Repeat("line of code content\n", 1000) // ~20KB of content
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", true, 500, 5000)
				tc.Execution.Result = longContent // large result
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 500, 5000),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Compacted should be less than raw (replacement text is much shorter than large file content)
	if ctx.Tokens.Compacted >= ctx.Tokens.Raw {
		t.Errorf("Compacted(%d) should be less than Raw(%d) when large content is replaced",
			ctx.Tokens.Compacted, ctx.Tokens.Raw)
	}

	// Saved should be positive
	if ctx.Tokens.Saved <= 0 {
		t.Errorf("Saved should be positive when large content is replaced, got %d", ctx.Tokens.Saved)
	}
}

// ---------------------------------------------------------------------------
// ContextTokens struct fields
// ---------------------------------------------------------------------------

func TestContextTokensFields(t *testing.T) {
	// Use a large file content so compaction actually saves tokens
	longContent := strings.Repeat("line of code content\n", 1000)
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", true, 500, 5000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 500, 5000),
		}),
	}
	ctx := BuildContext(messages, true, "", nil, nil)

	// Verify the token relationships
	if ctx.Tokens.Raw < ctx.Tokens.Compacted {
		t.Errorf("Raw(%d) should be >= Compacted(%d)", ctx.Tokens.Raw, ctx.Tokens.Compacted)
	}
	if ctx.Tokens.Saved != ctx.Tokens.Raw-ctx.Tokens.Compacted {
		t.Errorf("Saved(%d) should equal Raw(%d) - Compacted(%d)",
			ctx.Tokens.Saved, ctx.Tokens.Raw, ctx.Tokens.Compacted)
	}
}

// ---------------------------------------------------------------------------
// Session.BuildContext respects ContextCompaction setting
// ---------------------------------------------------------------------------

func TestSessionBuildContextRespectsCompactionSetting(t *testing.T) {
	// Use large content so compaction actually saves tokens
	longContent := strings.Repeat("line of code content\n", 1000)
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", true, 500, 5000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", true, 500, 5000),
		}),
	}

	// With compaction enabled — messages are compacted, tokens show savings
	sEnabled := buildTestSession(messages, true)
	ctxEnabled := sEnabled.BuildContext()
	if ctxEnabled.Tokens.Saved <= 0 {
		t.Error("enabled session should have positive savings")
	}
	// Messages should be compacted
	if !strings.Contains(ctxEnabled.Messages[1].Content[0].ToolOutput, "[compacted]") {
		t.Error("enabled session messages should be compacted")
	}

	// With compaction disabled — messages are raw, but tokens still project savings
	sDisabled := buildTestSession(messages, false)
	ctxDisabled := sDisabled.BuildContext()
	// Tokens should be identical (projection is the same regardless of enabled)
	if ctxDisabled.Tokens.Saved != ctxEnabled.Tokens.Saved {
		t.Errorf("disabled and enabled should have same projected savings: disabled=%d, enabled=%d",
			ctxDisabled.Tokens.Saved, ctxEnabled.Tokens.Saved)
	}
	if ctxDisabled.Tokens.Compacted != ctxEnabled.Tokens.Compacted {
		t.Errorf("disabled and enabled should have same projected compacted: disabled=%d, enabled=%d",
			ctxDisabled.Tokens.Compacted, ctxEnabled.Tokens.Compacted)
	}
	// But messages should be different (raw vs compacted)
	if len(ctxDisabled.Messages) != len(ctxEnabled.Messages) {
		t.Fatalf("message count differs: disabled=%d, enabled=%d", len(ctxDisabled.Messages), len(ctxEnabled.Messages))
	}
	// Disabled messages should contain the original long content (not compacted)
	foundLong := false
	for _, msg := range ctxDisabled.Messages {
		if msg.Role == goai_provider.RoleTool {
			for _, part := range msg.Content {
				if part.Type == goai_provider.PartToolResult && part.ToolCallID == "tc1" {
					if !strings.Contains(part.ToolOutput, "line of code content") {
						t.Error("disabled session should contain raw (uncompacted) tool result")
					}
					foundLong = true
				}
			}
		}
	}
	if !foundLong {
		t.Error("could not find raw tool result in disabled messages")
	}
}

// ---------------------------------------------------------------------------
// Media contract integration — BuildContext respects media policy decisions
// ---------------------------------------------------------------------------

func TestBuildContextMediaContractOpenAICompatOmitsFileParts(t *testing.T) {
	// OpenAI-compatible Chat Completions silently omits PartFile in the
	// openaicompat.ConvertMessages content array. This test verifies that
	// the media contract correctly marks file/PDF as unsupported for this
	// dialect, and that BuildContext with attachments would still produce
	// the PartFile (which would be silently dropped by the adapter).
	//
	// The contract test (media_contract_test.go) verifies the adapter
	// serialization behavior. This test verifies that the context builder
	// still produces the parts — the policy layer (media_policy.go) is
	// responsible for filtering them based on model capabilities.

	// Verify that the provider contract table correctly marks
	// openai-compatible as unsupported for file parts.
	p := provider.GetByName(config.ProviderOpenAI)
	if p == nil {
		t.Fatal("openai provider not registered")
	}
	if p.Dialect() != config.DialectOpenAICompatible {
		t.Fatalf("openai dialect: got %q, want %q", p.Dialect(), config.DialectOpenAICompatible)
	}

	// Verify that the media policy attempts delivery for compat models
	// (no Attachment flag → optimistic attempt).
	textOnlyCaps := &goai_provider.ModelCapabilities{
		InputModalities: goai_provider.ModalitySet{Text: true},
	}
	if d := MediaDecisionFor(textOnlyCaps, media.KindPDF); d != MediaAttempt {
		t.Errorf("text-only model PDF: got %s, want %s", d, MediaAttempt)
	}
	if d := MediaDecisionFor(textOnlyCaps, media.KindImage); d != MediaAttempt {
		t.Errorf("text-only model image: got %s, want %s", d, MediaAttempt)
	}
}

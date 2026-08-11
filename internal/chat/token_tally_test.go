package chat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

// ---------------------------------------------------------------------------
// Empty session — system messages contribute tokens
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyEmptySession(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	tally := session.CalculateTokenTally()

	if tally == nil {
		t.Fatal("tally should not be nil")
	}
	// NewSession adds system/environment messages with InputTokens,
	// so system_prompt should be > 0 and total should reflect that.
	if tally.Lifetime.Input.SystemPrompt <= 0 {
		t.Errorf("system_prompt should be > 0 for a new session, got %d", tally.Lifetime.Input.SystemPrompt)
	}
	if tally.Lifetime.Total <= 0 {
		t.Errorf("lifetime total should be > 0 (system messages), got %d", tally.Lifetime.Total)
	}
	// No user/assistant/tool output yet
	if tally.Lifetime.Output.Total != 0 {
		t.Errorf("empty session output total should be 0, got %d", tally.Lifetime.Output.Total)
	}
	if tally.Lifetime.Input.User != 0 {
		t.Errorf("empty session user input should be 0, got %d", tally.Lifetime.Input.User)
	}
}

// ---------------------------------------------------------------------------
// System prompt tokens counted as system_prompt
// ---------------------------------------------------------------------------

func TestCalculateTokenTallySystemPrompt(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	tally := session.CalculateTokenTally()

	// System prompt and environment messages should contribute to system_prompt
	if tally.Lifetime.Input.SystemPrompt <= 0 {
		t.Errorf("system_prompt should be > 0 for a new session, got %d", tally.Lifetime.Input.SystemPrompt)
	}
}

// ---------------------------------------------------------------------------
// System prompt tokens counted as system_prompt (not tool definitions)
// ---------------------------------------------------------------------------

func TestCalculateTokenTallySystemPromptBreakdown(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	tally := session.CalculateTokenTally()

	// System prompt and environment messages contribute to system_prompt
	// Tools Enabled message (if present) contributes to tool_definitions
	// For a basic session without registered tools, only system_prompt is set
	if tally.Lifetime.Input.SystemPrompt <= 0 {
		t.Errorf("system_prompt should be > 0 for a new session, got %d", tally.Lifetime.Input.SystemPrompt)
	}
}

// ---------------------------------------------------------------------------
// User message tokens counted as user input
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyUserMessage(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello world", ""))
	tally := session.CalculateTokenTally()

	if tally.Lifetime.Input.User <= 0 {
		t.Errorf("user tokens should be > 0, got %d", tally.Lifetime.Input.User)
	}
}

// ---------------------------------------------------------------------------
// Assistant message with thinking and text
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyAssistantWithThinking(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello", ""))
	session.Append(config.Message{
		ID:              "msg_2",
		Role:            config.RoleAssistant,
		Text:            "hello there",
		TextMetrics:     config.ContentMetrics{Tokens: 50},
		ThinkingText:    "let me think",
		ThinkingMetrics: config.ContentMetrics{Tokens: 30},
		ToolCallMetrics: config.ContentMetrics{Tokens: 10},
	})
	tally := session.CalculateTokenTally()

	if tally.Lifetime.Output.Assistant != 50 {
		t.Errorf("assistant output = %d, want 50", tally.Lifetime.Output.Assistant)
	}
	if tally.Lifetime.Output.Thinking != 30 {
		t.Errorf("thinking output = %d, want 30", tally.Lifetime.Output.Thinking)
	}
	if tally.Lifetime.Output.ToolCalls != 10 {
		t.Errorf("tool_calls output = %d, want 10", tally.Lifetime.Output.ToolCalls)
	}
}

// ---------------------------------------------------------------------------
// Tool execution tokens counted as input
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyToolExecutionInput(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello", ""))
	session.Append(config.Message{
		ID:          "msg_2",
		Role:        config.RoleAssistant,
		InputTokens: 100, // tool execution result tokens
		Text:        "result",
		TextMetrics: config.ContentMetrics{Tokens: 20},
	})
	tally := session.CalculateTokenTally()

	if tally.Lifetime.Input.ToolExecution != 100 {
		t.Errorf("tool_execution input = %d, want 100", tally.Lifetime.Input.ToolExecution)
	}
}

// ---------------------------------------------------------------------------
// Synthetic messages counted as synthetic input
// ---------------------------------------------------------------------------

func TestCalculateTokenTallySynthetic(t *testing.T) {
	session := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(config.Message{
		ID:          "msg_1",
		Role:        config.RoleSynthetic,
		Text:        "stream cancelled",
		InputTokens: 25,
		Label:       "aborted",
	})
	tally := session.CalculateTokenTally()

	if tally.Lifetime.Input.Synthetic != 25 {
		t.Errorf("synthetic input = %d, want 25", tally.Lifetime.Input.Synthetic)
	}
}

// ---------------------------------------------------------------------------
// Internal consistency: input total = sum of parts
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyInputConsistency(t *testing.T) {
	session := NewSession(config.SessionConfig{Tools: []string{"read_file"}}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello world", ""))
	session.Append(config.Message{
		ID:              "msg_2",
		Role:            config.RoleAssistant,
		InputTokens:     50,
		Text:            "response",
		TextMetrics:     config.ContentMetrics{Tokens: 30},
		ThinkingMetrics: config.ContentMetrics{Tokens: 10},
	})
	session.Append(config.Message{
		ID:          "msg_3",
		Role:        config.RoleSynthetic,
		Text:        "info",
		InputTokens: 15,
	})
	tally := session.CalculateTokenTally()

	expectedInputTotal := tally.Lifetime.Input.User +
		tally.Lifetime.Input.ToolExecution +
		tally.Lifetime.Input.SystemPrompt +
		tally.Lifetime.Input.ToolDefinitions +
		tally.Lifetime.Input.Synthetic

	if tally.Lifetime.Input.Total != expectedInputTotal {
		t.Errorf("input total %d != sum of parts %d",
			tally.Lifetime.Input.Total, expectedInputTotal)
	}

	expectedOutputTotal := tally.Lifetime.Output.Assistant +
		tally.Lifetime.Output.Thinking +
		tally.Lifetime.Output.ToolCalls

	if tally.Lifetime.Output.Total != expectedOutputTotal {
		t.Errorf("output total %d != sum of parts %d",
			tally.Lifetime.Output.Total, expectedOutputTotal)
	}

	expectedLifetimeTotal := tally.Lifetime.Input.Total + tally.Lifetime.Output.Total
	if tally.Lifetime.Total != expectedLifetimeTotal {
		t.Errorf("lifetime total %d != input(%d) + output(%d)",
			tally.Lifetime.Total, tally.Lifetime.Input.Total, tally.Lifetime.Output.Total)
	}
}

// ---------------------------------------------------------------------------
// Context tally: projection always computed regardless of enabled/disabled
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyContextProjectionAlways(t *testing.T) {
	// Even with compaction disabled, the context tally should project
	// potential savings because BuildContext always computes the plan.
	session := NewSession(config.SessionConfig{ContextCompaction: false}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello", ""))
	tally := session.CalculateTokenTally()

	// A simple session with no file tools — no compaction opportunities
	// so Raw == Compacted and Saved == 0 (same as enabled with no file tools)
	if tally.Context.Raw != tally.Context.Compacted {
		t.Errorf("no file tools: raw(%d) != compacted(%d)", tally.Context.Raw, tally.Context.Compacted)
	}
	if tally.Context.Saved != 0 {
		t.Errorf("no file tools: saved should be 0, got %d", tally.Context.Saved)
	}
}

// ---------------------------------------------------------------------------
// Context tally: disabled still projects savings when file tools present
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyContextDisabledProjectsSavings(t *testing.T) {
	// With compaction disabled but file tool calls present,
	// the tally should still project potential savings.
	longContent := strings.Repeat("line of code content\n", 1000)
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", nil, nil, true, 500, 5000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 500, 5000),
		}),
	}
	session := buildTestSession(messages, false)
	tally := session.CalculateTokenTally()

	// Projection should show savings even though compaction is disabled
	if tally.Context.Saved <= 0 {
		t.Errorf("disabled compaction should still project potential savings, got %d", tally.Context.Saved)
	}
	if tally.Context.Compacted >= tally.Context.Raw {
		t.Errorf("disabled Compacted(%d) should be < Raw(%d) with compaction opportunities",
			tally.Context.Compacted, tally.Context.Raw)
	}
	if tally.Context.SavedInstruction <= 0 {
		t.Errorf("disabled saved_instruction should be > 0, got %d", tally.Context.SavedInstruction)
	}
	if tally.Context.SavedExecution <= 0 {
		t.Errorf("disabled saved_execution should be > 0, got %d", tally.Context.SavedExecution)
	}

	// Verify the same session with compaction enabled produces identical projection
	sessionEnabled := buildTestSession(messages, true)
	tallyEnabled := sessionEnabled.CalculateTokenTally()

	if tally.Context.Raw != tallyEnabled.Context.Raw {
		t.Errorf("Raw should be same regardless of enabled: disabled=%d, enabled=%d",
			tally.Context.Raw, tallyEnabled.Context.Raw)
	}
	if tally.Context.Compacted != tallyEnabled.Context.Compacted {
		t.Errorf("Compacted should be same regardless of enabled: disabled=%d, enabled=%d",
			tally.Context.Compacted, tallyEnabled.Context.Compacted)
	}
	if tally.Context.Saved != tallyEnabled.Context.Saved {
		t.Errorf("Saved should be same regardless of enabled: disabled=%d, enabled=%d",
			tally.Context.Saved, tallyEnabled.Context.Saved)
	}
}

// ---------------------------------------------------------------------------
// Context tally: saved_instruction and saved_execution from CompactionPlan
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyContextWithCompaction(t *testing.T) {
	// Use a large file content so compaction actually saves tokens
	longContent := strings.Repeat("line of code content\n", 1000)
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", nil, nil, true, 500, 5000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 500, 5000),
		}),
	}
	session := buildTestSession(messages, true)
	tally := session.CalculateTokenTally()

	// Context totals come from BuildContext which counts actual message tokens
	if tally.Context.Compacted <= 0 {
		t.Errorf("context compacted should be > 0, got %d", tally.Context.Compacted)
	}
	if tally.Context.Raw <= 0 {
		t.Errorf("context raw should be > 0, got %d", tally.Context.Raw)
	}

	// saved_instruction and saved_execution come from the CompactionPlan
	// which tracks instruction/execution token fields from ToolCallEntry
	if tally.Context.SavedInstruction <= 0 {
		t.Errorf("context saved_instruction should be > 0, got %d", tally.Context.SavedInstruction)
	}
	if tally.Context.SavedExecution <= 0 {
		t.Errorf("context saved_execution should be > 0, got %d", tally.Context.SavedExecution)
	}

	// With large content, compacted should be less than raw
	if tally.Context.Compacted >= tally.Context.Raw {
		t.Errorf("compacted(%d) should be < raw(%d) when large content is replaced",
			tally.Context.Compacted, tally.Context.Raw)
	}
}

// ---------------------------------------------------------------------------
// Context saved = raw - compacted (by definition from ContextTokens)
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyContextSavedEqualsRawMinusCompacted(t *testing.T) {
	// Use large content so compaction saves tokens
	longContent := strings.Repeat("line of code content\n", 1000)
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", nil, nil, true, 500, 5000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 500, 5000),
		}),
	}
	session := buildTestSession(messages, true)
	tally := session.CalculateTokenTally()

	if tally.Context.Saved != tally.Context.Raw-tally.Context.Compacted {
		t.Errorf("context saved(%d) != raw(%d) - compacted(%d)",
			tally.Context.Saved, tally.Context.Raw, tally.Context.Compacted)
	}
}

// ---------------------------------------------------------------------------
// Full tally with mixed message types
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyFullSession(t *testing.T) {
	cfg := config.SessionConfig{
		Tools:             []string{"read_file"},
		ContextCompaction: true,
	}
	session := NewSession(cfg, config.Paths{}, runtimeconfig.Catalog{})

	// Add user message
	session.Append(NewUserMessage("msg_1", "read this file", ""))

	// Add assistant with tool call (large execution result for compaction savings)
	longContent := strings.Repeat("line of code content\n", 500)
	tc := tcRead("tc1", "/file.go", nil, nil, true, 10, 20)
	tc.Execution.Result = longContent
	assistant := config.Message{
		ID:              "msg_2",
		Role:            config.RoleAssistant,
		Text:            "reading file",
		TextMetrics:     config.ContentMetrics{Tokens: 10},
		ThinkingMetrics: config.ContentMetrics{Tokens: 5},
		ToolCalls:       []config.ToolCallEntry{tc},
	}
	session.Append(assistant)

	// Flush tool execution (sets InputTokens on assistant message)
	msgIdx := len(session.Doc.Messages) - 1
	session.Doc.Messages[msgIdx].InputTokens = 20 // execution tokens

	// Add another read to trigger compaction
	tc2 := tcRead("tc2", "/file.go", nil, nil, true, 10, 20)
	assistant2 := config.Message{
		ID:          "msg_3",
		Role:        config.RoleAssistant,
		Text:        "done",
		TextMetrics: config.ContentMetrics{Tokens: 8},
		ToolCalls:   []config.ToolCallEntry{tc2},
	}
	session.Append(assistant2)
	session.Doc.Messages[len(session.Doc.Messages)-1].InputTokens = 15

	tally := session.CalculateTokenTally()

	// Verify lifetime totals are consistent
	if tally.Lifetime.Input.Total != tally.Lifetime.Input.User+
		tally.Lifetime.Input.ToolExecution+
		tally.Lifetime.Input.SystemPrompt+
		tally.Lifetime.Input.ToolDefinitions+
		tally.Lifetime.Input.Synthetic {
		t.Errorf("input total inconsistent: %d != sum of parts", tally.Lifetime.Input.Total)
	}

	if tally.Lifetime.Output.Total != tally.Lifetime.Output.Assistant+
		tally.Lifetime.Output.Thinking+
		tally.Lifetime.Output.ToolCalls {
		t.Errorf("output total inconsistent: %d != sum of parts", tally.Lifetime.Output.Total)
	}

	if tally.Lifetime.Total != tally.Lifetime.Input.Total+tally.Lifetime.Output.Total {
		t.Errorf("lifetime total inconsistent: %d != input(%d) + output(%d)",
			tally.Lifetime.Total, tally.Lifetime.Input.Total, tally.Lifetime.Output.Total)
	}

	// Context should have savings from compaction (large content is replaced)
	if tally.Context.Saved <= 0 {
		t.Errorf("context saved should be > 0 with compaction on large content, got %d", tally.Context.Saved)
	}
}

// ---------------------------------------------------------------------------
// Tally JSON marshaling round-trip
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyJSONRoundTrip(t *testing.T) {
	session := NewSession(config.SessionConfig{Tools: []string{"read_file"}}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "test", ""))
	tally := session.CalculateTokenTally()

	// Marshal and unmarshal to verify JSON structure
	doc := config.SessionDoc{TokenTally: tally}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded config.SessionDoc
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.TokenTally == nil {
		t.Fatal("token_tally missing after round-trip")
	}
	if decoded.TokenTally.Lifetime.Total != tally.Lifetime.Total {
		t.Errorf("lifetime total mismatch after round-trip: %d != %d",
			decoded.TokenTally.Lifetime.Total, tally.Lifetime.Total)
	}
}

// ---------------------------------------------------------------------------
// Saved = Raw - Compacted can be negative (compaction expansion) — never clamp
// ---------------------------------------------------------------------------

func TestContextSavedCanBeNegative(t *testing.T) {
	// In theory, if compacted replacement text is longer than original content
	// (e.g., very short original result gets a longer compacted placeholder),
	// Saved = Raw - Compacted can be negative. This must NOT be clamped to zero.
	// Negative Saved means compaction expanded the context — a valid state.
	// The footer only displays savings when positive (> 0), but the tally
	// preserves the exact arithmetic.

	// Build a session where the compacted replacement is longer than original.
	// Use a read with a very short result so the compacted text is longer.
	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", nil, nil, true, 10, 1)
				tc.Execution.Result = "x" // 1-char result, compacted replacement is longer
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc2", "/file.go", nil, nil, true, 10, 1)
				tc.Execution.Result = "y"
				return tc
			}(),
		}),
	}
	ctx := BuildContext(messages, true)

	// Saved = Raw - Compacted; with tiny results, compacted may exceed raw
	// The key invariant is that Saved is exactly Raw - Compacted, never clamped.
	if ctx.Tokens.Saved != ctx.Tokens.Raw-ctx.Tokens.Compacted {
		t.Errorf("Saved(%d) must equal Raw(%d) - Compacted(%d) exactly — never clamped",
			ctx.Tokens.Saved, ctx.Tokens.Raw, ctx.Tokens.Compacted)
	}
	// Saved may be negative here (compaction overhead > savings on tiny content)
	// This is correct behavior — the footer will just not display a negative savings chip.
}

// ---------------------------------------------------------------------------
// Tools Enabled messages are RoleInternal, not RoleSystem
// calculateLifetimeTally must count them under ToolDefinitions
// ---------------------------------------------------------------------------

func TestCalculateTokenTallyToolsEnabledInternal(t *testing.T) {
	// BuildToolsEnabledMsg creates RoleInternal messages with Label "Tools Enabled".
	// calculateLifetimeTally must route these to ToolDefinitions, not SystemPrompt.
	session := NewSession(config.SessionConfig{Tools: []string{"read_file", "write_file"}},
		config.Paths{}, runtimeconfig.Catalog{})

	// Verify the Tools Enabled message exists and is RoleInternal
	var foundToolsMsg bool
	for _, msg := range session.Doc.Messages {
		if msg.Label == "Tools Enabled" {
			foundToolsMsg = true
			if msg.Role != config.RoleInternal {
				t.Errorf("Tools Enabled message should be RoleInternal, got %s", msg.Role)
			}
			if msg.InputTokens <= 0 {
				t.Error("Tools Enabled message should have InputTokens > 0")
			}
			break
		}
	}
	if !foundToolsMsg {
		t.Fatal("Tools Enabled message not found in session")
	}

	tally := session.CalculateTokenTally()

	// ToolDefinitions should include the Tools Enabled tokens
	if tally.Lifetime.Input.ToolDefinitions <= 0 {
		t.Errorf("ToolDefinitions should be > 0 for a session with tools, got %d",
			tally.Lifetime.Input.ToolDefinitions)
	}

	// The Tools Enabled tokens should NOT be in SystemPrompt
	// (SystemPrompt contains sys0 + env0, which are RoleSystem)
	// Verify total input = system_prompt + tool_definitions + synthetic (no user/assistant yet)
	expectedTotal := tally.Lifetime.Input.SystemPrompt +
		tally.Lifetime.Input.ToolDefinitions +
		tally.Lifetime.Input.Synthetic +
		tally.Lifetime.Input.User +
		tally.Lifetime.Input.ToolExecution

	if tally.Lifetime.Input.Total != expectedTotal {
		t.Errorf("Input.Total(%d) != sum of parts(%d)",
			tally.Lifetime.Input.Total, expectedTotal)
	}
}

// ---------------------------------------------------------------------------
// TotalTokens legacy field: loading old sessions works, saving clears it
// ---------------------------------------------------------------------------

func TestSessionDocLegacyTotalTokensClearOnSave(t *testing.T) {
	// Simulate a legacy session file with total_tokens set
	legacyJSON := `{
		"version": 1,
		"total_tokens": 5000,
		"meta": {"id": "test", "created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z"},
		"messages": []
	}`

	var doc config.SessionDoc
	if err := json.Unmarshal([]byte(legacyJSON), &doc); err != nil {
		t.Fatalf("failed to unmarshal legacy session: %v", err)
	}

	// Loading should preserve the legacy field
	if doc.TotalTokens != 5000 {
		t.Errorf("TotalTokens after load: got %d, want 5000", doc.TotalTokens)
	}

	// After SaveSessionDoc, TotalTokens should be cleared (0)
	// Use a temp directory for the save
	tmpDir := t.TempDir()
	paths := config.Paths{Sessions: tmpDir}
	tally := &config.TokenTally{}
	err := config.SaveSessionDoc(paths, "test", doc, tally)
	if err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}

	// Read back the saved file
	data, err := os.ReadFile(config.SessionPath(paths, "test"))
	if err != nil {
		t.Fatalf("failed to read saved session: %v", err)
	}

	// total_tokens should NOT appear in the saved JSON (omitempty + cleared to 0)
	if strings.Contains(string(data), "total_tokens") {
		t.Error("saved session should not contain total_tokens field")
	}

	// Verify the token_tally IS present
	var savedDoc config.SessionDoc
	if err := json.Unmarshal(data, &savedDoc); err != nil {
		t.Fatalf("failed to unmarshal saved session: %v", err)
	}
	if savedDoc.TotalTokens != 0 {
		t.Errorf("TotalTokens after save round-trip: got %d, want 0", savedDoc.TotalTokens)
	}
	if savedDoc.TokenTally == nil {
		t.Error("TokenTally should be present after save")
	}
}

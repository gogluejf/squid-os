package chat

import (
	"strings"
	"testing"
	"time"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/tools"
)

// ---------------------------------------------------------------------------
// New session: TokenTally calculated before footer first reads it
// ---------------------------------------------------------------------------

func TestLifecycleNewSessionHasTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})

	if s.Doc.TokenTally == nil {
		t.Fatal("NewSession should have initialized TokenTally")
	}
	if s.Doc.TokenTally.Lifetime.Input.SystemPrompt <= 0 {
		t.Errorf("system prompt tokens should be > 0, got %d", s.Doc.TokenTally.Lifetime.Input.SystemPrompt)
	}
}

// ---------------------------------------------------------------------------
// Loaded session: TokenTally recalculated on load
// ---------------------------------------------------------------------------

func TestLifecycleLoadSessionHasTally(t *testing.T) {
	// Build a session, then simulate loading it
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))

	// Simulate load by wrapping the doc
	loaded := LoadSession(s.Doc, "test", config.Paths{}, runtimeconfig.Catalog{})

	if loaded.Doc.TokenTally == nil {
		t.Fatal("LoadSession should have recalculated TokenTally")
	}
	if loaded.Doc.TokenTally.Lifetime.Input.User <= 0 {
		t.Errorf("loaded session user tokens should be > 0, got %d", loaded.Doc.TokenTally.Lifetime.Input.User)
	}
}

// ---------------------------------------------------------------------------
// Appending user message: lifetime tally updates
// ---------------------------------------------------------------------------

func TestLifecycleAppendUserUpdatesLifetime(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	before := s.Doc.TokenTally.Lifetime.Input.User

	s.Append(NewUserMessage("msg_1", "hello world", ""))

	if s.Doc.TokenTally == nil {
		t.Fatal("TokenTally should not be nil after append")
	}
	if s.Doc.TokenTally.Lifetime.Input.User <= before {
		t.Errorf("user tokens should increase after append: before=%d, after=%d",
			before, s.Doc.TokenTally.Lifetime.Input.User)
	}
}

// ---------------------------------------------------------------------------
// Appending assistant message: output tally updates
// ---------------------------------------------------------------------------

func TestLifecycleAppendAssistantUpdatesOutput(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))

	before := s.Doc.TokenTally.Lifetime.Output.Total
	s.Append(config.Message{
		ID:              "msg_2",
		Role:            config.RoleAssistant,
		Text:            "response text",
		TextMetrics:     config.ContentMetrics{Tokens: 50},
		ThinkingMetrics: config.ContentMetrics{Tokens: 30},
	})

	if s.Doc.TokenTally == nil {
		t.Fatal("TokenTally should not be nil")
	}
	if s.Doc.TokenTally.Lifetime.Output.Total <= before {
		t.Errorf("output tokens should increase after assistant append: before=%d, after=%d",
			before, s.Doc.TokenTally.Lifetime.Output.Total)
	}
	if s.Doc.TokenTally.Lifetime.Output.Assistant != 50 {
		t.Errorf("assistant output = %d, want 50", s.Doc.TokenTally.Lifetime.Output.Assistant)
	}
	if s.Doc.TokenTally.Lifetime.Output.Thinking != 30 {
		t.Errorf("thinking output = %d, want 30", s.Doc.TokenTally.Lifetime.Output.Thinking)
	}
}

// ---------------------------------------------------------------------------
// Appending synthetic message: synthetic input updates
// ---------------------------------------------------------------------------

func TestLifecycleAppendSyntheticUpdatesTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(config.Message{
		ID:          "msg_1",
		Role:        config.RoleSynthetic,
		Text:        "stream cancelled",
		InputTokens: 25,
		Label:       "aborted",
	})

	if s.Doc.TokenTally.Lifetime.Input.Synthetic != 25 {
		t.Errorf("synthetic input = %d, want 25", s.Doc.TokenTally.Lifetime.Input.Synthetic)
	}
}

// ---------------------------------------------------------------------------
// Truncating messages: tally reflects reduced history
// ---------------------------------------------------------------------------

func TestLifecycleTruncateUpdatesTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello world this is a longer message", ""))
	s.Append(NewUserMessage("msg_2", "another message", ""))

	tokensBefore := s.Doc.TokenTally.Lifetime.Input.User

	s.TruncateTo(1) // remove last user message

	if s.Doc.TokenTally.Lifetime.Input.User >= tokensBefore {
		t.Errorf("user tokens should decrease after truncate: before=%d, after=%d",
			tokensBefore, s.Doc.TokenTally.Lifetime.Input.User)
	}
}

// ---------------------------------------------------------------------------
// SaveAssistantMsg: tally updates via Append
// ---------------------------------------------------------------------------

func TestLifecycleSaveAssistantMsgUpdatesTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))
	before := s.Doc.TokenTally.Lifetime.Total

	idx := SaveAssistantMsg(s, config.Message{
		ID:          "msg_2",
		Role:        config.RoleAssistant,
		Text:        "response",
		TextMetrics: config.ContentMetrics{Tokens: 40},
	})

	// Index should be the position of the new message (system msgs + user + assistant)
	expectedIdx := len(s.Doc.Messages) - 1
	if idx != expectedIdx {
		t.Errorf("SaveAssistantMsg returned index %d, want %d", idx, expectedIdx)
	}
	if s.Doc.TokenTally.Lifetime.Output.Assistant != 40 {
		t.Errorf("assistant output = %d, want 40", s.Doc.TokenTally.Lifetime.Output.Assistant)
	}
	if s.Doc.TokenTally.Lifetime.Total <= before {
		t.Errorf("total should increase: before=%d, after=%d", before, s.Doc.TokenTally.Lifetime.Total)
	}
}

// ---------------------------------------------------------------------------
// FlushToolMessage: in-place mutation updates tally
// ---------------------------------------------------------------------------

func TestLifecycleFlushToolMessageUpdatesTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))

	// Save an assistant message with tool calls (pending execution)
	msgIdx := SaveAssistantMsg(s, config.Message{
		ID:   "msg_2",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			{
				ID:   "tc1",
				Type: "function",
				Instruction: struct {
					Name       string `json:"name"`
					Arguments  string `json:"arguments"`
					Tokens     int    `json:"tokens,omitempty"`
					DurationMs int64  `json:"duration_ms,omitempty"`
				}{Name: "read_file", Arguments: `{"path":"/file.go"}`, Tokens: 10},
				Execution: struct {
					Status     string             `json:"status,omitempty"`
					Result     string             `json:"result,omitempty"`
					Error      string             `json:"error,omitempty"`
					Tokens     int                `json:"tokens,omitempty"`
					DurationMs int64              `json:"duration_ms"`
					Files      []config.FileEntry `json:"files,omitempty"`
				}{Status: "success", Result: "file content here", Tokens: 15},
			},
		},
	})

	before := s.Doc.TokenTally.Lifetime.Input.ToolExecution
	FlushToolMessage(s, msgIdx)

	// FlushToolMessage sets InputTokens = TotalExecutionTokens
	after := s.Doc.TokenTally.Lifetime.Input.ToolExecution
	if after <= before {
		t.Errorf("tool execution tokens should increase after flush: before=%d, after=%d", before, after)
	}
}

// ---------------------------------------------------------------------------
// BuildContext updates TokenTally.Context from exact messages
// ---------------------------------------------------------------------------

func TestLifecycleBuildContextUpdatesContextTally(t *testing.T) {
	s := NewSession(config.SessionConfig{ContextCompaction: true}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))
	ctx := s.BuildContext()

	if s.Doc.TokenTally.Context.Raw != ctx.Tokens.Raw {
		t.Errorf("TokenTally.Context.Raw(%d) != BuildContext.Raw(%d)",
			s.Doc.TokenTally.Context.Raw, ctx.Tokens.Raw)
	}
	if s.Doc.TokenTally.Context.Compacted != ctx.Tokens.Compacted {
		t.Errorf("TokenTally.Context.Compacted(%d) != BuildContext.Compacted(%d)",
			s.Doc.TokenTally.Context.Compacted, ctx.Tokens.Compacted)
	}
	if s.Doc.TokenTally.Context.CompactedInput != ctx.Tokens.CompactedInput {
		t.Errorf("TokenTally.Context.CompactedInput(%d) != BuildContext.CompactedInput(%d)",
			s.Doc.TokenTally.Context.CompactedInput, ctx.Tokens.CompactedInput)
	}
	if s.Doc.TokenTally.Context.CompactedOutput != ctx.Tokens.CompactedOutput {
		t.Errorf("TokenTally.Context.CompactedOutput(%d) != BuildContext.CompactedOutput(%d)",
			s.Doc.TokenTally.Context.CompactedOutput, ctx.Tokens.CompactedOutput)
	}
}

func TestLifecycleBuildContextCompactionSavings(t *testing.T) {
	longContent := "line of code content\n"
	for i := 0; i < 500; i++ {
		longContent += "line of code content\n"
	}

	messages := []config.Message{
		msgWithTools("msg1", []config.ToolCallEntry{
			func() config.ToolCallEntry {
				tc := tcRead("tc1", "/file.go", nil, nil, true, 100, 1000)
				tc.Execution.Result = longContent
				return tc
			}(),
		}),
		msgWithTools("msg2", []config.ToolCallEntry{
			tcRead("tc2", "/file.go", nil, nil, true, 100, 1000),
		}),
	}
	s := buildTestSession(messages, true)
	s.RefreshTokenTally()

	ctx := s.BuildContext()

	// Context tally in Doc.TokenTally should match the Context returned
	if s.Doc.TokenTally.Context.Saved != ctx.Tokens.Saved {
		t.Errorf("TokenTally.Context.Saved(%d) != BuildContext.Saved(%d)",
			s.Doc.TokenTally.Context.Saved, ctx.Tokens.Saved)
	}
	if ctx.Tokens.Saved <= 0 {
		t.Error("compaction should produce positive savings")
	}
}

// ---------------------------------------------------------------------------
// BuildContext does not call RefreshTokenTally recursively
// ---------------------------------------------------------------------------

func TestLifecycleBuildContextNoRecursion(t *testing.T) {
	s := NewSession(config.SessionConfig{ContextCompaction: true}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))

	// Capture lifetime before BuildContext
	lifetimeBefore := s.Doc.TokenTally.Lifetime.Total

	// BuildContext should only update Context, not recalculate Lifetime
	ctx := s.BuildContext()
	_ = ctx

	lifetimeAfter := s.Doc.TokenTally.Lifetime.Total
	if lifetimeBefore != lifetimeAfter {
		t.Errorf("BuildContext should not change lifetime: before=%d, after=%d",
			lifetimeBefore, lifetimeAfter)
	}
}

// ---------------------------------------------------------------------------
// Multiple mutations: tally stays live throughout
// ---------------------------------------------------------------------------

func TestLifecycleMultipleMutationsStayLive(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	initialTotal := s.Doc.TokenTally.Lifetime.Total

	// Append user
	s.Append(NewUserMessage("msg_1", "first message", ""))
	afterUser := s.Doc.TokenTally.Lifetime.Total
	if afterUser <= initialTotal {
		t.Error("tally should increase after user message")
	}

	// Append assistant
	s.Append(config.Message{
		ID:          "msg_2",
		Role:        config.RoleAssistant,
		Text:        "response",
		TextMetrics: config.ContentMetrics{Tokens: 30},
	})
	afterAssistant := s.Doc.TokenTally.Lifetime.Total
	if afterAssistant <= afterUser {
		t.Error("tally should increase after assistant message")
	}

	// Append synthetic
	s.Append(config.Message{
		ID:          "msg_3",
		Role:        config.RoleSynthetic,
		Text:        "info message",
		InputTokens: 20,
	})
	afterSynthetic := s.Doc.TokenTally.Lifetime.Total
	if afterSynthetic <= afterAssistant {
		t.Error("tally should increase after synthetic message")
	}

	// Truncate
	s.TruncateTo(2)
	afterTruncate := s.Doc.TokenTally.Lifetime.Total
	if afterTruncate >= afterSynthetic {
		t.Error("tally should decrease after truncation")
	}
}

// ---------------------------------------------------------------------------
// PrepareTurn appends transition messages: tally updates
// ---------------------------------------------------------------------------

func TestLifecyclePrepareTurnUpdatesTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))

	// Set a pending inference change to trigger transition messages
	cfg := config.InferenceConfig{Provider: "test", Model: "test-model"}
	s.SetPendingInference(cfg)

	before := s.Doc.TokenTally.Lifetime.Total
	err := PrepareTurn(s)
	if err != nil {
		t.Fatalf("PrepareTurn failed: %v", err)
	}

	after := s.Doc.TokenTally.Lifetime.Total
	// Tally should reflect the new messages added by PrepareTurn
	if after < before {
		t.Errorf("tally should not decrease after PrepareTurn: before=%d, after=%d", before, after)
	}
}

// ---------------------------------------------------------------------------
// appendSyntheticMessage via Append refreshes tally
// ---------------------------------------------------------------------------

func TestLifecycleAppendSyntheticMessageUpdatesTally(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	before := s.Doc.TokenTally.Lifetime.Total

	idx := appendSyntheticMessage(s, "stream error: connection refused", "stream error")
	if idx < 0 {
		t.Fatal("appendSyntheticMessage should return valid index")
	}

	if s.Doc.TokenTally.Lifetime.Total <= before {
		t.Errorf("tally should increase after synthetic message: before=%d, after=%d",
			before, s.Doc.TokenTally.Lifetime.Total)
	}
	if s.Doc.TokenTally.Lifetime.Input.Synthetic <= 0 {
		t.Error("synthetic input should be > 0 after appendSyntheticMessage")
	}
}

// ---------------------------------------------------------------------------
// No separate dirty/cache state: tally always reflects current messages
// ---------------------------------------------------------------------------

func TestLifecycleNoDirtyState(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello world", ""))

	// The persisted tally and a fresh calculation should match on lifetime
	fresh := s.CalculateTokenTally()
	if s.Doc.TokenTally.Lifetime.Total != fresh.Lifetime.Total {
		t.Errorf("persisted lifetime(%d) != fresh lifetime(%d)",
			s.Doc.TokenTally.Lifetime.Total, fresh.Lifetime.Total)
	}

	// Append more and check again
	s.Append(config.Message{
		ID:          "msg_2",
		Role:        config.RoleAssistant,
		Text:        "response",
		TextMetrics: config.ContentMetrics{Tokens: 25},
	})

	fresh2 := s.CalculateTokenTally()
	if s.Doc.TokenTally.Lifetime.Total != fresh2.Lifetime.Total {
		t.Errorf("persisted lifetime(%d) != fresh lifetime(%d) after second append",
			s.Doc.TokenTally.Lifetime.Total, fresh2.Lifetime.Total)
	}
}

// ---------------------------------------------------------------------------
// TokenTally is never nil after initial session creation
// ---------------------------------------------------------------------------

func TestLifecycleTokenTallyNeverNil(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})

	if s.Doc.TokenTally == nil {
		t.Error("TokenTally should not be nil after NewSession")
	}

	s.Append(NewUserMessage("msg_1", "hello", ""))
	if s.Doc.TokenTally == nil {
		t.Error("TokenTally should not be nil after Append")
	}

	s.TruncateTo(0)
	if s.Doc.TokenTally == nil {
		t.Error("TokenTally should not be nil after TruncateTo")
	}

	s.Append(NewUserMessage("msg_2", "new message", ""))
	if s.Doc.TokenTally == nil {
		t.Error("TokenTally should not be nil after re-append")
	}
}

// ---------------------------------------------------------------------------
// Tally consistency after BuildContext + subsequent mutation
// ---------------------------------------------------------------------------

func TestLifecycleContextChangesImmediatelyAfterAppendAndTruncate(t *testing.T) {
	s := NewSession(config.SessionConfig{ContextCompaction: true}, config.Paths{}, runtimeconfig.Catalog{})
	initial := s.Doc.TokenTally.Context.Raw

	s.Append(NewUserMessage("msg_1", "hello world with enough text", ""))
	if s.Doc.TokenTally.Context.Raw <= initial {
		t.Fatalf("context raw should increase after append: before=%d after=%d", initial, s.Doc.TokenTally.Context.Raw)
	}

	afterAppend := s.Doc.TokenTally.Context.Raw
	s.TruncateTo(len(s.Doc.Messages) - 1)
	if s.Doc.TokenTally.Context.Raw >= afterAppend {
		t.Fatalf("context raw should decrease after truncate: before=%d after=%d", afterAppend, s.Doc.TokenTally.Context.Raw)
	}
}

func TestLifecycleContextChangesImmediatelyAfterToolCompletion(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "read file", ""))
	tc := tcRead("tc1", "/file.go", nil, nil, true, 10, 50)
	tc.Execution.Status = ""
	tc.Execution.Result = ""
	tc.Execution.Tokens = 0
	msgIdx := SaveAssistantMsg(s, config.Message{
		ID:        "msg_2",
		Role:      config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{tc},
	})
	before := s.Doc.TokenTally.Context.Raw

	s.Doc.Messages[msgIdx].ToolCalls[0].Execution.Status = tools.ResultStatusSuccess
	s.Doc.Messages[msgIdx].ToolCalls[0].Execution.Result = strings.Repeat("tool output ", 20)
	s.Doc.Messages[msgIdx].ToolCalls[0].Execution.Tokens = 50
	FlushToolMessage(s, msgIdx)
	if s.Doc.TokenTally.Context.Raw <= before {
		t.Fatalf("context raw should increase after tool completion: before=%d after=%d", before, s.Doc.TokenTally.Context.Raw)
	}
}

func TestLifecycleLoadRefreshesContextBeforeStream(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))
	s.Doc.TokenTally.Context.Raw = 0

	loaded := LoadSession(s.Doc, "test", config.Paths{}, runtimeconfig.Catalog{})
	if loaded.Doc.TokenTally.Context.Raw == 0 {
		t.Fatal("loaded session should refresh context tally immediately")
	}
}

func TestLifecycleSaveRefreshesContextBeforeStream(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))
	s.Doc.TokenTally.Context.Raw = 0

	tally := s.CalculateTokenTally()
	if tally.Context.Raw == 0 {
		t.Fatal("save-time full tally calculation should refresh context")
	}
	if s.Doc.TokenTally.Context.Raw != 0 {
		t.Fatal("CalculateTokenTally should not mutate session state")
	}
	s.RefreshTokenTally()
	if s.Doc.TokenTally.Context.Raw == 0 {
		t.Fatal("RefreshTokenTally should assign the current full tally")
	}
}

// ---------------------------------------------------------------------------
// Sequence of operations// ---------------------------------------------------------------------------
// Sequence of operations: realistic session flow
// ---------------------------------------------------------------------------

func TestLifecycleRealisticSessionFlow(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})

	// 1. Initial: system messages only
	total := s.Doc.TokenTally.Lifetime.Total
	if total <= 0 {
		t.Error("initial tally should be > 0 (system messages)")
	}

	// 2. User sends message
	s.Append(NewUserMessage("msg_1", "read this file", ""))
	if s.Doc.TokenTally.Lifetime.Input.User <= 0 {
		t.Error("user tokens should be > 0 after user message")
	}

	// 3. Assistant responds with tool call
	msgIdx := SaveAssistantMsg(s, config.Message{
		ID:   "msg_2",
		Role: config.RoleAssistant,
		Text: "reading file",
		ToolCalls: []config.ToolCallEntry{
			tcRead("tc1", "/file.go", nil, nil, true, 10, 20),
		},
	})

	// 4. Flush tool execution
	FlushToolMessage(s, msgIdx)

	// Tally should reflect tool execution tokens
	if s.Doc.TokenTally.Lifetime.Input.ToolExecution <= 0 {
		t.Error("tool execution tokens should be > 0 after flush")
	}

	// 5. BuildContext for next request
	ctx := s.BuildContext()
	if s.Doc.TokenTally.Context.Raw != ctx.Tokens.Raw {
		t.Errorf("context raw mismatch: Doc=%d, BuildContext=%d",
			s.Doc.TokenTally.Context.Raw, ctx.Tokens.Raw)
	}

	// 6. Truncate (undo)
	s.TruncateTo(1) // back to just system + user
	if s.Doc.TokenTally.Lifetime.Output.Total != 0 {
		t.Errorf("output should be 0 after truncate, got %d", s.Doc.TokenTally.Lifetime.Output.Total)
	}
}

func TestRefreshTokenTallyZeroesOnEmpty(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello", ""))

	// Tally should reflect the user message
	if s.Doc.TokenTally.Lifetime.Input.User <= 0 {
		t.Fatal("user tokens should be > 0")
	}

	// Truncate all messages
	s.TruncateTo(0)

	// Lifetime should now reflect only remaining (empty) history
	// (system messages were truncated too)
	if s.Doc.TokenTally.Lifetime.Input.User != 0 {
		t.Errorf("user tokens should be 0 after full truncate, got %d", s.Doc.TokenTally.Lifetime.Input.User)
	}
}

// Test that RefreshTokenTally is idempotent — calling it twice produces same result
func TestRefreshTokenTallyIdempotent(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "hello world", ""))
	s.Append(config.Message{
		ID:          "msg_2",
		Role:        config.RoleAssistant,
		Text:        "response",
		TextMetrics: config.ContentMetrics{Tokens: 25},
	})

	first := s.Doc.TokenTally.Lifetime.Total
	s.RefreshTokenTally()
	second := s.Doc.TokenTally.Lifetime.Total

	if first != second {
		t.Errorf("RefreshTokenTally should be idempotent: first=%d, second=%d", first, second)
	}
}

// Test that time.Now() in messages doesn't affect tally determinism
func TestRefreshTokenTallyDeterministic(t *testing.T) {
	s := NewSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	s.Append(NewUserMessage("msg_1", "test", ""))

	time.Sleep(1 * time.Millisecond)
	s.RefreshTokenTally()
	total1 := s.Doc.TokenTally.Lifetime.Total

	time.Sleep(1 * time.Millisecond)
	s.RefreshTokenTally()
	total2 := s.Doc.TokenTally.Lifetime.Total

	if total1 != total2 {
		t.Errorf("tally should be deterministic across calls: %d != %d", total1, total2)
	}
}

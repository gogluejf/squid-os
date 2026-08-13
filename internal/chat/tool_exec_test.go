package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
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

func TestExecuteToolsRangedReadValidatesFileState(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	checksum := util.ComputeChecksum([]byte(content))

	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{
		file: {Checksum: checksum, Trace: config.TraceRead},
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
			}{Name: "read_file", Arguments: `{"path":"test.txt","start_line":1,"end_line":2}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.Status != tools.ResultStatusSuccess {
		t.Fatalf("expected success for valid ranged read, got error: %s", exec.Error)
	}
	if exec.Result != "line1\nline2" {
		t.Fatalf("expected ranged content, got %q", exec.Result)
	}
}

func TestExecuteToolsRangedReadAllowsExternalChangeWithoutRefreshingState(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	// Track an old checksum (different from current on-disk)
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
			}{Name: "read_file", Arguments: `{"path":"test.txt","start_line":1,"end_line":2}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.Status != tools.ResultStatusSuccess {
		t.Fatalf("expected stale ranged read to succeed, got %q", exec.Error)
	}
	if exec.Result != "line1\nline2" {
		t.Fatalf("expected ranged content, got %q", exec.Result)
	}
	if s.Doc.FileState[file].Checksum != oldChecksum {
		t.Fatal("stale ranged read must not refresh tracked checksum")
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

func TestExecuteToolsMultipleRangedReadsSameVersion(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	// First ranged read — no prior tracking, so no validation needed
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
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
			}{Name: "read_file", Arguments: `{"path":"test.txt","start_line":1,"end_line":2}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})
	if s.Doc.Messages[0].ToolCalls[0].Execution.Status != tools.ResultStatusSuccess {
		t.Fatalf("first ranged read failed: %s", s.Doc.Messages[0].ToolCalls[0].Execution.Error)
	}

	// Second ranged read — file state now has the checksum, should pass
	s.Doc.Messages = append(s.Doc.Messages, config.Message{
		ID:   "msg_2",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_2",
			Type: "function",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "read_file", Arguments: `{"path":"test.txt","start_line":3,"end_line":5}`},
		}},
	})

	ExecuteTools(s, ToolExecOptions{MsgIdx: 1})
	if s.Doc.Messages[1].ToolCalls[0].Execution.Status != tools.ResultStatusSuccess {
		t.Fatalf("second ranged read failed: %s", s.Doc.Messages[1].ToolCalls[0].Execution.Error)
	}
	if s.Doc.Messages[1].ToolCalls[0].Execution.Result != "line3\nline4\nline5" {
		t.Fatalf("expected ranged content, got %q", s.Doc.Messages[1].ToolCalls[0].Execution.Result)
	}
}

func TestExecuteToolsRangedReadNotTrackedFile(t *testing.T) {
	// A ranged read on a file that's not in file_state should succeed
	// (first read of the file — nothing to validate against)
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
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
			}{Name: "read_file", Arguments: `{"path":"test.txt","start_line":2,"end_line":3}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.Status != tools.ResultStatusSuccess {
		t.Fatalf("expected success for untracked file ranged read, got error: %s", exec.Error)
	}
	if exec.Result != "line2\nline3" {
		t.Fatalf("expected ranged content, got %q", exec.Result)
	}
}

func TestExecuteToolsAgentToolPreallocatesChildRef(t *testing.T) {
	// Verify that call_agent preallocates child ID/name on the execution entry
	// even when the tool fails (e.g., agent not in scope).
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"call_agent"}
	s.Doc.Config.Agents = nil // no agents in scope
	s.Doc.Identity = config.SessionIdentity{
		ID:     "parent-1",
		RootID: "parent-1",
		Depth:  0,
	}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_agent_1",
			Type: "function",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "call_agent", Arguments: `{"agent":"trader","prompt":"analyze"}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	// Child fields should be populated even on failure
	if exec.ChildSessionID == "" {
		t.Error("expected non-empty child_session_id for agent tool")
	}
	if exec.ChildSessionName == "" {
		t.Error("expected non-empty child_session_name for agent tool")
	}
	// Name should contain agent name and tool call ID
	if !strings.Contains(exec.ChildSessionName, "trader") {
		t.Errorf("expected agent name in child name, got %q", exec.ChildSessionName)
	}
	if !strings.Contains(exec.ChildSessionName, "tool_agent_1") {
		t.Errorf("expected tool call ID in child name, got %q", exec.ChildSessionName)
	}
	// The tool should still fail because the agent is not in scope
	if exec.Status != tools.ResultStatusError {
		t.Errorf("expected error status for out-of-scope agent, got %q", exec.Status)
	}
}

func TestExecuteToolsInlineAgentPreallocatesChildRef(t *testing.T) {
	// Verify that inline_agent preallocates child ID/name on the execution entry
	// even when the tool fails (e.g., depth exceeded).
	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"inline_agent"}
	s.Doc.Config.Limits.MaxAgentDepth = 0 // depth exceeded
	s.Doc.Identity = config.SessionIdentity{
		ID:     "parent-2",
		RootID: "parent-2",
		Depth:  0,
	}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_inline_1",
			Type: "function",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "inline_agent", Arguments: `{"prompt":"do something"}`},
		}},
	}}

	ExecuteTools(s, ToolExecOptions{MsgIdx: 0})

	exec := s.Doc.Messages[0].ToolCalls[0].Execution
	if exec.ChildSessionID == "" {
		t.Error("expected non-empty child_session_id for inline agent tool")
	}
	if exec.ChildSessionName == "" {
		t.Error("expected non-empty child_session_name for inline agent tool")
	}
	// Name should have "inline-" prefix and tool call ID
	if !strings.HasPrefix(exec.ChildSessionName, "inline-") {
		t.Errorf("expected inline prefix in child name, got %q", exec.ChildSessionName)
	}
	if !strings.Contains(exec.ChildSessionName, "tool_inline_1") {
		t.Errorf("expected tool call ID in child name, got %q", exec.ChildSessionName)
	}
}

func TestExecuteToolsNonAgentToolNoChildRef(t *testing.T) {
	// Verify that non-agent tools do NOT populate child fields.
	dir := t.TempDir()
	content := "hello"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"read_file"}
	s.Doc.Config.WorkingDir = dir
	s.Doc.Identity = config.SessionIdentity{
		ID:     "parent-3",
		RootID: "parent-3",
		Depth:  0,
	}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID:   "tool_read_1",
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
	if exec.ChildSessionID != "" {
		t.Errorf("expected empty child_session_id for non-agent tool, got %q", exec.ChildSessionID)
	}
	if exec.ChildSessionName != "" {
		t.Errorf("expected empty child_session_name for non-agent tool, got %q", exec.ChildSessionName)
	}
}

func TestAgentCheckpointPersistsLinkBeforeLaunchAndStopsOnFailure(t *testing.T) {
	s := &Session{Doc: config.NewSessionDoc(config.SessionConfig{Tools: []string{"call_agent"}})}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID: "tool_agent_1",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "call_agent", Arguments: `{"agent":"trader","prompt":"analyze"}`},
		}},
	}}

	checkpointCalls := 0
	result := ExecuteTools(s, ToolExecOptions{
		MsgIdx: 0,
		Checkpoint: func() error {
			checkpointCalls++
			exec := s.Doc.Messages[0].ToolCalls[0].Execution
			if exec.ChildSessionID == "" || exec.ChildSessionName == "" {
				t.Fatal("checkpoint ran before child link was staged")
			}
			return errors.New("disk full")
		},
	})

	if checkpointCalls != 1 {
		t.Fatalf("checkpoint calls = %d, want 1", checkpointCalls)
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "disk full") {
		t.Fatalf("expected checkpoint failure, got %v", result.Error)
	}
	if s.Doc.Messages[0].ToolCalls[0].Execution.Status != "" {
		t.Fatal("agent executed despite pre-launch checkpoint failure")
	}
}

func TestCompletedToolFlushInvokesCheckpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Session{Doc: config.NewSessionDoc(config.SessionConfig{Tools: []string{"read_file"}, WorkingDir: dir})}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{{
			ID: "tool_read_1",
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: "read_file", Arguments: `{"path":"test.txt"}`},
		}},
	}}

	checkpointCalls := 0
	result := ExecuteTools(s, ToolExecOptions{MsgIdx: 0, Checkpoint: func() error {
		checkpointCalls++
		return nil
	}})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if checkpointCalls != 1 {
		t.Fatalf("checkpoint calls = %d, want 1", checkpointCalls)
	}
	if s.Doc.Messages[0].ToolCalls[0].Execution.Status != tools.ResultStatusSuccess {
		t.Fatalf("tool status = %q", s.Doc.Messages[0].ToolCalls[0].Execution.Status)
	}
}

func TestSessionToolContextIncludesIdentity(t *testing.T) {
	paths := config.Paths{Sessions: "/tmp/sessions"}
	s := NewRootSession(config.SessionConfig{
		Tools:            []string{"read_file"},
		AuthMode:         config.AuthorizationAuto,
		Inference:        config.InferenceConfig{Provider: "test", Model: "test"},
		SystemPromptFile: "default",
		Autosave:         config.SessionAutosave{Name: "test"},
	}, paths, runtimeconfig.Catalog{})

	ctx := s.ToolContext("tool_identity", tools.ChildSessionRef{})
	if ctx.ToolCallID != "tool_identity" {
		t.Errorf("expected tool call ID %q, got %q", "tool_identity", ctx.ToolCallID)
	}
	if ctx.Identity.ID == "" {
		t.Error("expected non-empty identity ID in ToolContext")
	}
	if ctx.Identity.RootID != ctx.Identity.ID {
		t.Errorf("expected root ID to equal ID for root session, got root=%q id=%q", ctx.Identity.RootID, ctx.Identity.ID)
	}
	if ctx.Identity.Depth != 0 {
		t.Errorf("expected depth 0 for root session, got %d", ctx.Identity.Depth)
	}
	if ctx.SessionDir != "/tmp/sessions/test" {
		t.Errorf("expected session dir %q, got %q", "/tmp/sessions/test", ctx.SessionDir)
	}
}

func TestSessionToolContextIncludesOptionalChildRef(t *testing.T) {
	paths := config.Paths{Sessions: "/tmp/sessions"}
	s := NewRootSession(config.SessionConfig{
		Tools:            []string{"read_file"},
		AuthMode:         config.AuthorizationAuto,
		Inference:        config.InferenceConfig{Provider: "test", Model: "test"},
		SystemPromptFile: "default",
		Autosave:         config.SessionAutosave{Name: "test"},
	}, paths, runtimeconfig.Catalog{})

	childRef := tools.ChildSessionRef{ID: "child_123", Name: "child-session"}
	ctx := s.ToolContext("tool_abc", childRef)
	if ctx.ToolCallID != "tool_abc" {
		t.Errorf("expected tool call ID %q, got %q", "tool_abc", ctx.ToolCallID)
	}
	if ctx.ChildRef != childRef {
		t.Errorf("expected child ref %#v, got %#v", childRef, ctx.ChildRef)
	}
	if ctx.Identity.ID == "" {
		t.Error("expected identity ID in ToolContext")
	}
	if ctx.SessionDir != "/tmp/sessions/test" {
		t.Errorf("expected session dir in ToolContext, got %q", ctx.SessionDir)
	}
}

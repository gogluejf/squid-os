package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

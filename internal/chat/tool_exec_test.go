package chat

import (
	"strings"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/tools"
)

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

func TestExecuteToolsStoresOnlySizeErrorForOversizedResult(t *testing.T) {
	reg := tools.GetRegistry()

	s := &Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Session.MaxToolResultTokens = 10
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

	ExecuteTools(s, reg, ToolExecOptions{AuthorizationMode: config.AuthorizationAuto, MsgIdx: 0})

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

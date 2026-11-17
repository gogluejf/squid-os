package chat

import (
	"testing"

	"squid-os/internal/config"
)

func TestShouldNudgeThinkingOnlyTurn(t *testing.T) {
	thinkOnly := config.Message{Role: config.RoleAssistant, ThinkingText: "lots of reasoning"}
	withText := config.Message{Role: config.RoleAssistant, Text: "answer", ThinkingText: "reasoning"}
	toolEntry := config.ToolCallEntry{ID: "tc_1"}
	toolEntry.Instruction.Name = "bash"
	withTool := config.Message{Role: config.RoleAssistant, ThinkingText: "reasoning", ToolCalls: []config.ToolCallEntry{toolEntry}}
	empty := config.Message{Role: config.RoleAssistant}

	cases := []struct {
		name   string
		nudged bool
		msg    config.Message
		want   bool
	}{
		{"thinking-only first attempt nudges", false, thinkOnly, true},
		{"thinking-only after nudge never nudges again", true, thinkOnly, false},
		{"visible text never nudges", false, withText, false},
		{"tool call never nudges", false, withTool, false},
		{"empty turn never nudges", false, empty, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldNudgeThinkingOnlyTurn(tc.nudged, tc.msg); got != tc.want {
				t.Errorf("shouldNudgeThinkingOnlyTurn(nudged=%v, msg=%+v) = %v, want %v", tc.nudged, tc.msg, got, tc.want)
			}
		})
	}
}

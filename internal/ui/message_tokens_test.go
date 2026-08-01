package ui

import (
	"strings"
	"testing"

	"squid-os/internal/config"
)

func TestSyntheticMessageRendersInputTokensOnly(t *testing.T) {
	message := config.Message{
		Role:        config.RoleSynthetic,
		Label:       "skill_load",
		InputTokens: 123,
	}
	if message.TextMetrics.Tokens != 0 {
		t.Fatalf("synthetic message must not contain output tokens: %+v", message.TextMetrics)
	}

	rendered := renderSyntheticMessage(message, 100, false)
	if !strings.Contains(rendered, "↑123") {
		t.Fatalf("missing input token chip: %q", rendered)
	}
	if strings.Contains(rendered, "↓") {
		t.Fatalf("synthetic message rendered output tokens: %q", rendered)
	}
}

package tools

import (
	"testing"

	"squid-os/internal/config"
)

func TestAgentToolGuards(t *testing.T) {
	if r := executeCallAgent(map[string]interface{}{"agent": "x", "prompt": "p"}, config.SessionConfig{Limits: config.SessionLimits{MaxAgentDepth: 1}}); r.Status != ResultStatusError {
		t.Fatal("expected scope rejection")
	}
	if r := executeInlineAgent(map[string]interface{}{"prompt": "p"}, config.SessionConfig{Limits: config.SessionLimits{MaxAgentDepth: 0}}); r.Error != "agent call depth exceeded" {
		t.Fatalf("%+v", r)
	}
}
func TestAgentToolsRegistered(t *testing.T) {
	for _, name := range []string{"list_agents", "call_agent", "inline_agent"} {
		if GetRegistry().Get(name) == nil {
			t.Fatalf("%s missing", name)
		}
	}
}

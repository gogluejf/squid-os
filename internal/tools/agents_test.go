package tools

import (
	"strings"
	"testing"

	"squid-os/internal/config"
)

func TestAgentToolGuards(t *testing.T) {
	if r := executeCallAgent(map[string]interface{}{"agent": "x", "prompt": "p"}, RuntimeContext{Config: config.SessionConfig{Limits: config.SessionLimits{MaxAgentDepth: 1}}}); r.Status != ResultStatusError {
		t.Fatal("expected scope rejection")
	}
	if r := executeInlineAgent(map[string]interface{}{"prompt": "p"}, RuntimeContext{Config: config.SessionConfig{Limits: config.SessionLimits{MaxAgentDepth: 0}}}); r.Error != "agent call depth exceeded" {
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

func TestIsAgentTool(t *testing.T) {
	if !IsAgentTool("call_agent") {
		t.Error("call_agent should be an agent tool")
	}
	if !IsAgentTool("inline_agent") {
		t.Error("inline_agent should be an agent tool")
	}
	if IsAgentTool("read_file") {
		t.Error("read_file should not be an agent tool")
	}
	if IsAgentTool("bash") {
		t.Error("bash should not be an agent tool")
	}
	if IsAgentTool("list_agents") {
		t.Error("list_agents should not be an agent tool")
	}
}

func TestGenerateChildSessionRefCallAgent(t *testing.T) {
	ref := GenerateChildSessionRef("call_agent", "trader", "tool_abc123")
	if ref.ID == "" {
		t.Error("expected non-empty child ID")
	}
	if ref.Name == "" {
		t.Error("expected non-empty child name")
	}
	// Name should contain agent name and tool call ID
	if !strings.Contains(ref.Name, "trader") {
		t.Errorf("expected agent name in child name, got %q", ref.Name)
	}
	if !strings.Contains(ref.Name, "tool_abc123") {
		t.Errorf("expected tool call ID in child name, got %q", ref.Name)
	}
}

func TestGenerateChildSessionRefInlineAgent(t *testing.T) {
	ref := GenerateChildSessionRef("inline_agent", "", "tool_xyz789")
	if ref.ID == "" {
		t.Error("expected non-empty child ID")
	}
	if ref.Name == "" {
		t.Error("expected non-empty child name")
	}
	// Inline name should start with "inline-"
	if !strings.HasPrefix(ref.Name, "inline-") {
		t.Errorf("expected inline prefix in child name, got %q", ref.Name)
	}
	if !strings.Contains(ref.Name, "tool_xyz789") {
		t.Errorf("expected tool call ID in child name, got %q", ref.Name)
	}
}

func TestGenerateChildSessionRefUniqueNames(t *testing.T) {
	ref1 := GenerateChildSessionRef("call_agent", "trader", "tool_1")
	ref2 := GenerateChildSessionRef("call_agent", "trader", "tool_2")
	if ref1.Name == ref2.Name {
		t.Error("different tool call IDs should produce different child names")
	}
	if ref1.ID == ref2.ID {
		t.Error("different tool call IDs should produce different child IDs")
	}
}

func TestChildSessionRefEmptyForNonAgent(t *testing.T) {
	// Non-agent tools should have an empty ChildRef
	var ref ChildSessionRef
	if ref.ID != "" || ref.Name != "" {
		t.Error("zero-value ChildSessionRef should be empty")
	}
}

func TestRuntimeContextHasChildRef(t *testing.T) {
	ctx := RuntimeContext{
		Config:     config.SessionConfig{},
		Catalog:    nil,
		Identity:   config.SessionIdentity{ID: "root-1", RootID: "root-1"},
		SessionDir: "/tmp/sessions/test",
		ToolCallID: "tool_1",
		ChildRef:   ChildSessionRef{ID: "child-1", Name: "trader-tool_1"},
	}
	if ctx.ChildRef.ID != "child-1" {
		t.Errorf("expected child ID %q, got %q", "child-1", ctx.ChildRef.ID)
	}
	if ctx.ChildRef.Name != "trader-tool_1" {
		t.Errorf("expected child name %q, got %q", "trader-tool_1", ctx.ChildRef.Name)
	}
	if ctx.ToolCallID != "tool_1" {
		t.Errorf("expected tool call ID %q, got %q", "tool_1", ctx.ToolCallID)
	}
}

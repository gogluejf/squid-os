package chat

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

func TestNewSessionIncludesToolsBootstrap(t *testing.T) {
	session := NewSession(config.SessionConfig{Tools: []string{"read_file"}}, config.Paths{}, runtimeconfig.Catalog{})
	assertMessageOrder(t, session.Doc.Messages, "sys0", "env0", "config0", "tools0")

	message := messageByID(session.Doc.Messages, "tools0")
	if message == nil || message.Label != "Tools Enabled" || message.Params["tools"] == "" {
		t.Fatalf("invalid tools bootstrap: %+v", message)
	}
	if message.InputTokens <= 0 {
		t.Fatalf("tools schema tokens should be counted: %+v", message)
	}
}

func TestSetPendingInferenceBeforeFirstTurnRewritesBootstrap(t *testing.T) {
	initial := config.InferenceConfig{
		Provider: "vllm",
		Model:    "qwen",
		Thinking: config.ThinkingConfig{Enabled: false},
	}
	next := config.InferenceConfig{
		Provider: "openai",
		Model:    "gpt",
		Thinking: config.ThinkingConfig{Enabled: true},
	}
	session := NewSession(config.SessionConfig{Inference: initial}, config.Paths{}, runtimeconfig.Catalog{})

	session.SetPendingInference(next)

	if session.Doc.Initial.Inference != next {
		t.Fatalf("initial inference not rewritten: got %+v, want %+v", session.Doc.Initial.Inference, next)
	}
	if session.CurrentInference() != next {
		t.Fatalf("current inference not rewritten: got %+v, want %+v", session.CurrentInference(), next)
	}
	if session.Doc.Pending != nil && session.Doc.Pending.Inference != nil {
		t.Fatalf("bootstrap inference change must not remain pending: %+v", session.Doc.Pending.Inference)
	}
	message := messageByID(session.Doc.Messages, "config0")
	if message == nil {
		t.Fatal("config0 message missing")
	}
	if message.Params["provider"] != "openai" || message.Params["model"] != "gpt" || message.Params["thinking"] != "on" {
		t.Fatalf("config0 not rewritten: %+v", message)
	}
	for _, message := range session.Doc.Messages {
		if message.Label == "Model Switched" || message.Label == "Thinking Switched" {
			t.Fatalf("bootstrap change created a transition: %+v", message)
		}
	}
}

func TestSetPendingInferenceAfterFirstTurnQueuesTransition(t *testing.T) {
	initial := config.InferenceConfig{Provider: "vllm", Model: "qwen"}
	next := config.InferenceConfig{Provider: "openai", Model: "gpt"}
	session := NewSession(config.SessionConfig{Inference: initial}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello", ""))

	session.SetPendingInference(next)

	if session.CurrentInference() != initial {
		t.Fatalf("current inference changed before PrepareTurn: %+v", session.CurrentInference())
	}
	if session.Doc.Initial.Inference != initial {
		t.Fatalf("initial inference changed after first turn: %+v", session.Doc.Initial.Inference)
	}
	if session.Doc.Pending == nil || session.Doc.Pending.Inference == nil || *session.Doc.Pending.Inference != next {
		t.Fatalf("inference transition not queued: %+v", session.Doc.Pending)
	}
}

func TestSessionCatalogAreIndependent(t *testing.T) {
	globalSkills, globalAgents := t.TempDir(), t.TempDir()
	workspaceA, workspaceB := t.TempDir(), t.TempDir()
	writeSessionSkill(t, filepath.Join(workspaceA, ".squid-os", "skills"), "build-a")
	writeSessionSkill(t, filepath.Join(workspaceB, ".squid-os", "skills"), "build-b")
	paths := config.Paths{Skills: globalSkills, Agents: globalAgents}
	catalogsA, err := runtimeconfig.LoadCatalog(paths, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	catalogsB, err := runtimeconfig.LoadCatalog(paths, workspaceB)
	if err != nil {
		t.Fatal(err)
	}
	sessionA := NewSession(config.SessionConfig{WorkingDir: workspaceA}, paths, catalogsA)
	sessionB := NewSession(config.SessionConfig{WorkingDir: workspaceB}, paths, catalogsB)
	if _, ok := sessionA.Catalog.Skills.Resolve("build-a"); !ok {
		t.Fatal("session A lost its catalog")
	}
	if _, ok := sessionA.Catalog.Skills.Resolve("build-b"); ok {
		t.Fatal("session A sees session B catalog")
	}
	if _, ok := sessionB.Catalog.Skills.Resolve("build-b"); !ok {
		t.Fatal("session B lost its catalog")
	}
}

func TestSetWorkingDirReappliesCapabilityPolicy(t *testing.T) {
	globalSkills, globalAgents := t.TempDir(), t.TempDir()
	workspaceA, workspaceB := t.TempDir(), t.TempDir()
	writeSessionSkill(t, filepath.Join(workspaceA, ".squid-os", "skills"), "build")
	paths := config.Paths{Skills: globalSkills, Agents: globalAgents}
	cfg := config.SessionConfig{
		WorkingDir:  workspaceA,
		SkillPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: []string{"build"}},
		AgentPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAll},
		Skills:      []config.CapabilityRef{{Scope: config.CapabilityScopeWorkspace, Name: "build"}},
	}
	catalogs, err := runtimeconfig.LoadCatalog(paths, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession(cfg, paths, catalogs)

	if err := session.SetWorkingDir(workspaceB); err != nil {
		t.Fatal(err)
	}
	if session.Doc.Config.WorkingDir != workspaceB || session.Doc.Pending == nil || session.Doc.Pending.Skills == nil {
		t.Fatalf("workspace transition not staged: %+v", session.Doc)
	}
	if len(*session.Doc.Pending.Skills) != 0 || len(session.Doc.Pending.SkillsMissing) != 1 || session.Doc.Pending.SkillsMissing[0] != "build" {
		t.Fatalf("policy not reapplied: %+v", session.Doc.Pending)
	}
}

func writeSessionSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: project build\n---\nBuild.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertMessageOrder(t *testing.T, messages []config.Message, ids ...string) {
	t.Helper()
	if len(messages) < len(ids) {
		t.Fatalf("got %d messages, want at least %d", len(messages), len(ids))
	}
	for index, id := range ids {
		if messages[index].ID != id {
			t.Fatalf("message %d: got %q, want %q", index, messages[index].ID, id)
		}
	}
}

func messageByID(messages []config.Message, id string) *config.Message {
	for index := range messages {
		if messages[index].ID == id {
			return &messages[index]
		}
	}
	return nil
}

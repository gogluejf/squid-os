package runtime

import (
	"os"
	"path/filepath"
	"squid-os/internal/config"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	off := false
	globalSkills, globalAgents, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	writeRuntimeAgent(t, globalAgents, "review", "agent/a")
	got, err := Resolve(Inputs{Settings: config.Settings{Provider: "base", Model: "m"}, Paths: config.Paths{Skills: globalSkills, MemoryDir: t.TempDir(), Agents: globalAgents}, AgentName: "review", CLI: Overrides{WorkingDir: workspace, Model: "cli/c", Thinking: &off, ToolNames: []string{"bash"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Inference.Provider != "cli" || got.Config.Inference.Model != "c" || len(got.Config.Tools) != 1 || got.Config.Tools[0] != "bash" || got.Config.Limits.MaxAgentDepth != 5 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestExplicitZeroAgentDepth(t *testing.T) {
	got, err := Resolve(Inputs{Settings: config.Settings{Provider: "p", Model: "m"}, Paths: config.Paths{Skills: t.TempDir(), MemoryDir: t.TempDir(), Agents: t.TempDir()}, CLI: Overrides{MaxAgentDepthSet: true}})
	if err != nil || got.Config.Limits.MaxAgentDepth != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestInvalidMaxTime(t *testing.T) {
	_, err := Resolve(Inputs{Paths: config.Paths{MemoryDir: "/m"}, CLI: Overrides{MaxTime: "bad"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthorizationNormalizationByTarget(t *testing.T) {
	tests := []struct {
		name      string
		mode      config.AuthorizationMode
		target    Target
		want      config.AuthorizationMode
		wantError bool
	}{
		{name: "interactive asks on write", mode: config.AuthorizationAskOnWrite, target: TargetInteractive, want: config.AuthorizationAskOnWrite},
		{name: "autonomous ends on inherited write", mode: config.AuthorizationAskOnWrite, target: TargetAutonomous, want: config.AuthorizationEndOnWrite},
		{name: "autonomous ends on inherited all", mode: config.AuthorizationAskForAll, target: TargetAutonomous, want: config.AuthorizationEndOnAll},
		{name: "autonomous keeps explicit end", mode: config.AuthorizationEndOnWrite, target: TargetAutonomous, want: config.AuthorizationEndOnWrite},
		{name: "interactive rejects end", mode: config.AuthorizationEndOnAll, target: TargetInteractive, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeAuthorization(test.mode, test.target)
			if test.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got=%q err=%v want=%q", got, err, test.want)
			}
		})
	}
}

func TestApplyToExistingSessionSetsPendingInference(t *testing.T) {
	current := config.InferenceConfig{Provider: "vllm", Model: "qwen"}
	cfg := config.SessionConfig{Inference: current}
	doc := config.NewSessionDoc(cfg)
	options := config.SessionConfig{Inference: config.InferenceConfig{Provider: "openai", Model: "gpt"}, AuthMode: config.AuthorizationAskOnWrite, Limits: config.SessionLimits{MaxToolResultTokens: 99}}

	ApplyToExistingSession(&doc, options)

	if doc.Config.Inference != current {
		t.Fatalf("current transcript inference changed: %+v", doc.Config.Inference)
	}
	if doc.Pending == nil || doc.Pending.Inference == nil || doc.Pending.Inference.Provider != "openai" || doc.Pending.Inference.Model != "gpt" {
		t.Fatalf("pending inference missing: %+v", doc.Pending)
	}
	if doc.Config.AuthMode != config.AuthorizationAskOnWrite {
		t.Fatalf("auth not applied: %q", doc.Config.AuthMode)
	}
}

func TestApplyToExistingSessionClearsMatchingPendingInference(t *testing.T) {
	current := config.InferenceConfig{Provider: "vllm", Model: "qwen"}
	cfg := config.SessionConfig{Inference: current}
	doc := config.NewSessionDoc(cfg)
	doc.Pending = &config.PendingConfig{Inference: &config.InferenceConfig{Provider: "old", Model: "pending"}}
	ApplyToExistingSession(&doc, config.SessionConfig{Inference: current})
	if doc.Pending != nil && doc.Pending.Inference != nil {
		t.Fatalf("matching inference should clear pending: %+v", doc.Pending.Inference)
	}
}

func TestApplyToExistingSessionSeparatesPendingAndDirectFields(t *testing.T) {
	current := config.SessionConfig{
		Inference:   config.InferenceConfig{Provider: "old", Model: "model"},
		ActiveSkill: "old-skill",
		Tools:       []string{"read"},
		Skills:      []config.CapabilityRef{{Scope: "global", Name: "review"}},
		Agents:      []config.CapabilityRef{{Scope: "global", Name: "researcher"}},
		AuthMode:    config.AuthorizationAuto,
		WorkingDir:  "/old",
		Limits:      config.SessionLimits{MaxSteps: 1, MaxAgentDepth: 2},
	}
	doc := config.NewSessionDoc(current)
	desired := config.SessionConfig{
		Inference:    config.InferenceConfig{Provider: "new", Model: "model"},
		ActiveSkill:  "new-skill",
		Tools:        []string{"bash"},
		Skills:       []config.CapabilityRef{{Scope: "global", Name: "plan"}},
		Agents:       []config.CapabilityRef{{Scope: "global", Name: "coder"}},
		AuthMode:     config.AuthorizationEndOnWrite,
		WorkingDir:   "/new",
		Autosave:     config.SessionAutosave{Enabled: true, Name: "saved"},
		Limits:       config.SessionLimits{MaxSteps: 9, MaxAgentDepth: 4},
		DebugEnabled: true,
	}

	ApplyToExistingSession(&doc, desired)

	if doc.Config.Inference != current.Inference || doc.Config.ActiveSkill != current.ActiveSkill {
		t.Fatalf("pending fields changed before PrepareTurn: %+v", doc.Config)
	}
	if doc.Config.Tools[0] != "read" || doc.Config.Skills[0].Name != "review" || doc.Config.Agents[0].Name != "researcher" {
		t.Fatalf("pending scopes changed before PrepareTurn: %+v", doc.Config)
	}
	if doc.Pending == nil || doc.Pending.Inference == nil || doc.Pending.ActiveSkill == nil || doc.Pending.Tools == nil || doc.Pending.Skills == nil || doc.Pending.Agents == nil {
		t.Fatalf("missing pending changes: %+v", doc.Pending)
	}
	if doc.Config.AuthMode != desired.AuthMode || doc.Config.WorkingDir != desired.WorkingDir || doc.Config.Autosave != desired.Autosave || doc.Config.Limits != desired.Limits || !doc.Config.DebugEnabled {
		t.Fatalf("direct policies not applied: %+v", doc.Config)
	}
}

func TestExistingSessionUsesSettingsAsDesiredInference(t *testing.T) {
	current := config.InferenceConfig{Provider: "vllm", Model: "qwen"}
	cfg := config.SessionConfig{Inference: current}
	doc := config.NewSessionDoc(cfg)
	resolved, err := Resolve(Inputs{Settings: config.Settings{Provider: "openai", Model: "gpt", Authorization: "ask-on-write"}, Paths: config.Paths{MemoryDir: "/memory"}, ExistingSession: &doc, Target: TargetInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config.Inference.Provider != "openai" || resolved.Config.Inference.Model != "gpt" {
		t.Fatalf("settings should be desired inference: %+v", resolved)
	}
}

func TestExistingRunPreservesSessionAndAppliesCLI(t *testing.T) {
	doc := config.NewSessionDoc(config.SessionConfig{
		Inference: config.InferenceConfig{Provider: "agent", Model: "model"},
		AuthMode:  config.AuthorizationAuto,
		Autosave:  config.SessionAutosave{Enabled: true, Name: "old"},
	})
	resolved, err := Resolve(Inputs{
		Settings:        config.Settings{Provider: "settings", Model: "model", Authorization: "ask-on-write", AutoSave: false},
		Paths:           config.Paths{MemoryDir: "/memory"},
		ExistingSession: &doc,
		SessionName:     "session-name",
		Target:          TargetAutonomous,
		CLI:             Overrides{Model: "cli/model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config.Inference.Provider != "cli" || resolved.Config.Inference.Model != "model" {
		t.Fatalf("CLI model should override preserved session inference: %+v", resolved.Config.Inference)
	}
	if resolved.Config.AuthMode != config.AuthorizationAuto || !resolved.Config.Autosave.Enabled || resolved.Config.Autosave.Name != "session-name" {
		t.Fatalf("run continuation should preserve session policy: %+v", resolved)
	}
}

func TestResolveAutosaveNamePrecedence(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		explicit    string
		want        string
	}{
		{name: "generated"},
		{name: "session", sessionName: "loaded", want: "loaded"},
		{name: "explicit", sessionName: "loaded", explicit: "renamed", want: "renamed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			on := true
			resolved, err := Resolve(Inputs{
				Settings:    config.Settings{Provider: "p", Model: "m"},
				Paths:       config.Paths{MemoryDir: "/memory"},
				SessionName: test.sessionName,
				CLI:         Overrides{Autosave: &on, AutosaveName: test.explicit},
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if resolved.Config.Autosave.Name == "" {
					t.Fatal("expected generated autosave name")
				}
			} else if resolved.Config.Autosave.Name != test.want {
				t.Fatalf("got %q, want %q", resolved.Config.Autosave.Name, test.want)
			}
		})
	}
}

func TestResolveCapabilityPolicyAndWorkspaceShadowing(t *testing.T) {
	globalSkills, globalAgents, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	writeRuntimeSkill(t, globalSkills, "review", "global")
	writeRuntimeSkill(t, filepath.Join(workspace, ".squid-os", "skills"), "review", "workspace")

	paths := config.Paths{Skills: globalSkills, Agents: globalAgents, MemoryDir: t.TempDir()}
	resolved, err := Resolve(Inputs{
		Settings: config.Settings{Provider: "p", Model: "m"},
		Paths:    paths,
		CLI:      Overrides{WorkingDir: workspace, SkillNames: []string{"review"}},
		Target:   TargetInteractive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config.SkillPolicy.Mode != config.PolicyModeAllowlist || len(resolved.Config.SkillPolicy.Requested) != 1 {
		t.Fatalf("policy not preserved: %+v", resolved.Config.SkillPolicy)
	}
	if len(resolved.Config.Skills) != 1 || resolved.Config.Skills[0].Scope != config.CapabilityScopeWorkspace {
		t.Fatalf("effective list not workspace-resolved: %+v", resolved.Config.Skills)
	}
}

func TestResolveRejectsMissingInitialAllowlist(t *testing.T) {
	paths := config.Paths{Skills: t.TempDir(), Agents: t.TempDir(), MemoryDir: t.TempDir()}
	_, err := Resolve(Inputs{
		Settings: config.Settings{Provider: "p", Model: "m"},
		Paths:    paths,
		CLI:      Overrides{WorkingDir: t.TempDir(), SkillNames: []string{"missing"}},
		Target:   TargetInteractive,
	})
	if err == nil {
		t.Fatal("expected missing allowlist error")
	}
}

func writeRuntimeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nInstructions.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeRuntimeAgent(t *testing.T, root, name, model string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\nmodel: " + model + "\ntools: [read]\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyToExistingSessionAppliesAllNonPendingFields(t *testing.T) {
	current := config.SessionConfig{Inference: config.InferenceConfig{Provider: "old", Model: "model"}}
	doc := config.NewSessionDoc(current)
	desired := config.SessionConfig{
		Inference:        config.InferenceConfig{Provider: "new", Model: "model"},
		SystemPromptFile: "new-system",
		AgentName:        "agent",
		AgentSystem:      "instructions",
		Memory:           config.SessionMemory{Namespace: "agent", Path: "/memory", Instructions: "remember"},
	}
	ApplyToExistingSession(&doc, desired)
	if doc.Config.SystemPromptFile != desired.SystemPromptFile || doc.Config.AgentName != desired.AgentName || doc.Config.AgentSystem != desired.AgentSystem || doc.Config.Memory != desired.Memory {
		t.Fatalf("resolved non-pending fields were not applied: %+v", doc.Config)
	}
	if doc.Config.Inference != current.Inference || doc.Pending == nil || doc.Pending.Inference == nil {
		t.Fatalf("inference transition not preserved: config=%+v pending=%+v", doc.Config.Inference, doc.Pending)
	}
}

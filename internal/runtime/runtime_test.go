package runtime

import (
	"squid-os/internal/agent"
	"squid-os/internal/config"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	off := false
	got, err := Resolve(Inputs{Settings: config.Settings{Provider: "base", Model: "m"}, Paths: config.Paths{MemoryDir: "/memory", Agents: "/agents"}, Agent: &agent.Definition{Name: "review", Model: "agent/a", Tools: []string{"read"}}, CLI: Overrides{Model: "cli/c", Thinking: &off, ToolNames: []string{"bash"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Inference.Provider != "cli" || got.Inference.Model != "c" || len(got.Tools) != 1 || got.Tools[0] != "bash" || got.Limits.MaxAgentDepth != 5 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestExplicitZeroAgentDepth(t *testing.T) {
	got, err := Resolve(Inputs{Settings: config.Settings{Provider: "p", Model: "m"}, Paths: config.Paths{MemoryDir: "/m", Agents: "/a"}, CLI: Overrides{MaxAgentDepthSet: true}})
	if err != nil || got.Limits.MaxAgentDepth != 0 {
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
		{name: "TUI asks on write", mode: config.AuthorizationAskOnWrite, target: TargetTUI, want: config.AuthorizationAskOnWrite},
		{name: "run ends on inherited write", mode: config.AuthorizationAskOnWrite, target: TargetNonInteractive, want: config.AuthorizationEndOnWrite},
		{name: "run ends on inherited all", mode: config.AuthorizationAskForAll, target: TargetNonInteractive, want: config.AuthorizationEndOnAll},
		{name: "run keeps explicit end", mode: config.AuthorizationEndOnWrite, target: TargetNonInteractive, want: config.AuthorizationEndOnWrite},
		{name: "TUI rejects end", mode: config.AuthorizationEndOnAll, target: TargetTUI, wantError: true},
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
		Skills:      []string{"review"},
		Agents:      []string{"researcher"},
		AuthMode:    config.AuthorizationAuto,
		WorkingDir:  "/old",
		Limits:      config.SessionLimits{MaxSteps: 1, MaxAgentDepth: 2},
	}
	doc := config.NewSessionDoc(current)
	desired := config.SessionConfig{
		Inference:    config.InferenceConfig{Provider: "new", Model: "model"},
		ActiveSkill:  "new-skill",
		Tools:        []string{"bash"},
		Skills:       []string{"plan"},
		Agents:       []string{"coder"},
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
	if doc.Config.Tools[0] != "read" || doc.Config.Skills[0] != "review" || doc.Config.Agents[0] != "researcher" {
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
	resolved, err := Resolve(Inputs{Settings: config.Settings{Provider: "openai", Model: "gpt", Authorization: "ask-on-write"}, Paths: config.Paths{MemoryDir: "/memory"}, ExistingSession: &doc, Target: TargetTUI})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Inference.Provider != "openai" || resolved.Inference.Model != "gpt" {
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
		Target:          TargetNonInteractive,
		CLI:             Overrides{Model: "cli/model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Inference.Provider != "cli" || resolved.Inference.Model != "model" {
		t.Fatalf("CLI model should override preserved session inference: %+v", resolved.Inference)
	}
	if resolved.AuthMode != config.AuthorizationAuto || !resolved.Autosave.Enabled || resolved.Autosave.Name != "session-name" {
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
				if resolved.Autosave.Name == "" {
					t.Fatal("expected generated autosave name")
				}
			} else if resolved.Autosave.Name != test.want {
				t.Fatalf("got %q, want %q", resolved.Autosave.Name, test.want)
			}
		})
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

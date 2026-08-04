package runtime

import (
	"fmt"
	"os"
	"strings"
	"time"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/memory"
	"squid-os/internal/skills"
	"squid-os/internal/tools"
	"squid-os/internal/util"
)

const DefaultMaxAgentDepth = 5

type Target string

const (
	TargetTUI            Target = "tui"
	TargetNonInteractive Target = "non-interactive"
)

type Overrides struct {
	AgentName, Model, WorkingDir, AgentSystem, ActiveSkill string
	AuthMode                                               config.AuthorizationMode
	MemoryNamespace, MemoryInstructions, AutosaveName      string
	Thinking, Autosave                                     *bool
	ToolNames, SkillNames, CallableAgentNames              []string
	MaxSteps, MaxTools, MaxToolResultTokens, MaxAgentDepth int
	MaxAgentDepthSet                                       bool
	MaxTime                                                string
}

type Inputs struct {
	Settings        config.Settings
	Paths           config.Paths
	Agent           *agent.Definition
	CLI             Overrides
	ExistingSession *config.SessionDoc
	SessionName     string
	Target          Target
}

type SessionRequest struct {
	Paths           config.Paths
	Endpoints       config.EndpointsConfig
	Config          config.SessionConfig
	ExistingSession *config.SessionDoc
	SessionName     string
	Prompt          string
}

// Resolve builds a SessionConfig from settings, agent, and CLI overrides.
// Precedence: CLI > agent > settings.
func Resolve(in Inputs) (config.SessionConfig, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return config.SessionConfig{}, fmt.Errorf("current working directory: %w", err)
	}
	cfg := config.SessionConfig{
		Inference:        config.InferenceConfig{Provider: in.Settings.Provider, Model: in.Settings.Model, Thinking: in.Settings.Thinking},
		SystemPromptFile: in.Settings.SystemPromptFile,
		WorkingDir:       workingDir,
		AuthMode:         in.Settings.ValidateAuthorization(),
		Limits: config.SessionLimits{
			MaxToolResultTokens: in.Settings.MaxToolResultTokens,
			MaxAgentDepth:       DefaultMaxAgentDepth,
		},
		Autosave:     config.SessionAutosave{Enabled: in.Settings.AutoSave},
		DebugEnabled: in.Settings.DebugEnabled,
		Tools:        availableToolNames(),
		Skills:       availableSkillNames(),
		Agents:       availableAgentNames(),
	}

	// Existing sessions start from their persisted config. TUI continuation overlays
	// current interactive preferences; non-interactive continuation stays session-owned.
	if in.ExistingSession != nil {
		existing := config.CloneSessionConfig(in.ExistingSession.Config)
		if in.Target == "" || in.Target == TargetTUI {
			existing.Inference = cfg.Inference
			existing.Autosave.Enabled = cfg.Autosave.Enabled
			existing.AuthMode = cfg.AuthMode
			existing.Limits.MaxToolResultTokens = cfg.Limits.MaxToolResultTokens
			existing.DebugEnabled = cfg.DebugEnabled
		}
		cfg = existing
	}

	// Agent definitions apply only when creating a new session.
	if a := in.Agent; a != nil && in.ExistingSession == nil {
		cfg.AgentName, cfg.AgentSystem, cfg.WorkingDir = a.Name, a.System, util.ExpandHome(first(a.WorkingDirectory, cfg.WorkingDir))
		cfg.Tools, cfg.Skills, cfg.Agents = clone(a.Tools), clone(a.Skills), clone(a.Agents)
		if a.AuthMode != "" {
			mode, err := config.ParseAuthorizationMode(a.AuthMode)
			if err != nil {
				return config.SessionConfig{}, fmt.Errorf("agent auth_mode: %w", err)
			}
			cfg.AuthMode = mode
		}
		cfg.Autosave = config.SessionAutosave{Enabled: a.Save.Enabled, Name: ""}
		if a.Thinking != nil {
			cfg.Inference.Thinking.Enabled = *a.Thinking
		}
		if a.Model != "" {
			if err := applyModel(&cfg.Inference, a.Model); err != nil {
				return config.SessionConfig{}, err
			}
		}
		cfg.Limits = resolveLimits(cfg.Limits, a.Limits.MaxSteps, a.Limits.MaxTools, a.Limits.MaxTime, a.Limits.MaxToolResultTokens, a.Limits.MaxAgentDepth)
		cfg.Memory.Namespace = a.Memory.Namespace
		cfg.Memory.Instructions = a.Memory.Instructions
	}

	// CLI overrides agent + settings
	c := in.CLI
	if c.AgentName != "" {
		cfg.AgentName = c.AgentName
	}
	if c.Model != "" {
		if err := applyModel(&cfg.Inference, c.Model); err != nil {
			return config.SessionConfig{}, err
		}
	}
	if c.Thinking != nil {
		cfg.Inference.Thinking.Enabled = *c.Thinking
	}
	if c.WorkingDir != "" {
		cfg.WorkingDir = util.ExpandHome(c.WorkingDir)
	}
	if c.AgentSystem != "" {
		cfg.AgentSystem = c.AgentSystem
	}
	if c.ToolNames != nil {
		cfg.Tools = clone(c.ToolNames)
	}
	if c.SkillNames != nil {
		cfg.Skills = clone(c.SkillNames)
	}
	if c.CallableAgentNames != nil {
		cfg.Agents = clone(c.CallableAgentNames)
	}
	if c.ActiveSkill != "" {
		cfg.ActiveSkill = c.ActiveSkill
	}
	if c.AuthMode != "" {
		cfg.AuthMode = c.AuthMode
	}
	if c.Autosave != nil {
		cfg.Autosave.Enabled = *c.Autosave
	}
	if c.AutosaveName != "" {
		cfg.Autosave.Name = c.AutosaveName
	}
	if c.MemoryNamespace != "" {
		cfg.Memory.Namespace = c.MemoryNamespace
	}
	if c.MemoryInstructions != "" {
		cfg.Memory.Instructions = c.MemoryInstructions
	}
	cfg.Limits = resolveLimits(cfg.Limits, c.MaxSteps, c.MaxTools, "", c.MaxToolResultTokens, c.MaxAgentDepth)
	if c.MaxTime != "" {
		duration, err := time.ParseDuration(c.MaxTime)
		if err != nil {
			return config.SessionConfig{}, fmt.Errorf("max time: %w", err)
		}
		cfg.Limits.MaxTime = duration.String()
	}
	if c.MaxAgentDepthSet {
		cfg.Limits.MaxAgentDepth = c.MaxAgentDepth
	}

	if cfg.Autosave.Enabled {
		switch {
		case c.AutosaveName != "":
			cfg.Autosave.Name = c.AutosaveName
		case in.SessionName != "":
			cfg.Autosave.Name = in.SessionName
		case cfg.Autosave.Name == "":
			cfg.Autosave.Name = time.Now().Format("2006-01-02_15-04-05")
		}
	} else {
		cfg.Autosave.Name = ""
	}

	if cfg.Memory.Namespace == "" {
		cfg.Memory.Namespace = string(memory.NamespaceGlobal)
	}
	path, err := memory.ResolvePath(memory.Namespace(cfg.Memory.Namespace), cfg.WorkingDir, memory.Paths{GlobalMemoryDir: in.Paths.MemoryDir, AgentsDir: in.Paths.Agents}, cfg.AgentName)
	if err != nil {
		return config.SessionConfig{}, err
	}
	cfg.Memory.Path = path

	cfg.AuthMode, err = normalizeAuthorization(cfg.AuthMode, in.Target)
	if err != nil {
		return config.SessionConfig{}, err
	}

	return cfg, nil
}

// ApplyToExistingSession separates next-turn transitions from direct runtime policy updates.
func ApplyToExistingSession(doc *config.SessionDoc, desired config.SessionConfig) {
	if doc == nil {
		return
	}

	current := config.CloneSessionConfig(doc.Config)
	pending := &config.PendingConfig{}

	if desired.Inference != current.Inference {
		value := desired.Inference
		pending.Inference = &value
	}
	if desired.ActiveSkill != current.ActiveSkill {
		value := desired.ActiveSkill
		pending.ActiveSkill = &value
	}
	if !util.EqualStringSlices(desired.Tools, current.Tools) {
		value := clone(desired.Tools)
		pending.Tools = &value
	}
	if !util.EqualStringSlices(desired.Skills, current.Skills) {
		value := clone(desired.Skills)
		pending.Skills = &value
	}
	if !util.EqualStringSlices(desired.Agents, current.Agents) {
		value := clone(desired.Agents)
		pending.Agents = &value
	}

	// Apply the complete resolved config, then retain current transcript-sensitive
	// fields until PrepareTurn commits their pending transitions.
	doc.Config = config.CloneSessionConfig(desired)
	doc.Config.Inference = current.Inference
	doc.Config.ActiveSkill = current.ActiveSkill
	doc.Config.Tools = clone(current.Tools)
	doc.Config.Skills = clone(current.Skills)
	doc.Config.Agents = clone(current.Agents)

	if pending.Inference == nil && pending.ActiveSkill == nil && pending.Tools == nil && pending.Skills == nil && pending.Agents == nil {
		doc.Pending = nil
	} else {
		doc.Pending = pending
	}
}

func normalizeAuthorization(mode config.AuthorizationMode, target Target) (config.AuthorizationMode, error) {
	if target == "" {
		target = TargetTUI
	}
	switch target {
	case TargetTUI:
		switch mode {
		case config.AuthorizationAuto, config.AuthorizationAskOnWrite, config.AuthorizationAskForAll:
			return mode, nil
		default:
			return "", fmt.Errorf("authorization mode %q is not valid for TUI", mode)
		}
	case TargetNonInteractive:
		switch mode {
		case config.AuthorizationAuto:
			return mode, nil
		case config.AuthorizationAskOnWrite, config.AuthorizationEndOnWrite:
			return config.AuthorizationEndOnWrite, nil
		case config.AuthorizationAskForAll, config.AuthorizationEndOnAll:
			return config.AuthorizationEndOnAll, nil
		default:
			return "", fmt.Errorf("authorization mode %q is not valid for non-interactive execution", mode)
		}
	default:
		return "", fmt.Errorf("unknown runtime target %q", target)
	}
}

func applyModel(inf *config.InferenceConfig, combined string) error {
	p, m, ok := strings.Cut(combined, "/")
	if !ok || p == "" || m == "" {
		return fmt.Errorf("model must use provider/model format")
	}
	inf.Provider, inf.Model = p, m
	return nil
}

func resolveLimits(base config.SessionLimits, steps, tools int, duration string, tokens, depth int) config.SessionLimits {
	if steps != 0 {
		base.MaxSteps = steps
	}
	if tools != 0 {
		base.MaxTools = tools
	}
	if duration != "" {
		base.MaxTime = duration
	}
	if tokens != 0 {
		base.MaxToolResultTokens = tokens
	}
	if depth != 0 {
		base.MaxAgentDepth = depth
	}
	return base
}

func first(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func clone(values []string) []string { return append([]string(nil), values...) }

func availableToolNames() []string {
	registered := tools.GetTools()
	names := make([]string, 0, len(registered))
	for _, tool := range registered {
		names = append(names, tool.Name)
	}
	return names
}

func availableSkillNames() []string {
	registry := skills.GetRegistry()
	if registry == nil {
		return nil
	}
	entries := registry.List()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}

func availableAgentNames() []string {
	registry := agent.GetRegistry()
	if registry == nil {
		return nil
	}
	entries := registry.List()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}

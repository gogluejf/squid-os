package runtime

import (
	"fmt"
	"os"
	"path/filepath"
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
	TargetInteractive Target = "interactive"
	TargetAutonomous  Target = "autonomous"
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
	AgentName       string
	CLI             Overrides
	ExistingSession *config.SessionDoc
	SessionName     string
	Target          Target
}

type Catalog struct {
	WorkingDir string
	Skills     *skills.Registry
	Agents     *agent.Registry
}

type Resolved struct {
	Config  config.SessionConfig
	Catalog Catalog
}

type SessionRequest struct {
	Paths           config.Paths
	Endpoints       config.EndpointsConfig
	Config          config.SessionConfig
	Catalog         Catalog
	ExistingSession *config.SessionDoc
	SessionName     string
	Prompt          string
}

// Resolve builds a SessionConfig from settings, agent, and CLI overrides.
// Precedence: CLI > agent > settings.
func Resolve(in Inputs) (Resolved, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return Resolved{}, fmt.Errorf("current working directory: %w", err)
	}
	if in.CLI.WorkingDir != "" {
		workingDir = util.ExpandHome(in.CLI.WorkingDir)
	}

	catalogDir := workingDir
	catalog, err := loadCatalog(in.Paths, catalogDir)
	if err != nil {
		return Resolved{}, err
	}

	cfg := config.SessionConfig{
		Target:           string(in.Target),
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
		SkillPolicy:  allPolicy(),
		AgentPolicy:  allPolicy(),
	}

	if in.ExistingSession != nil {
		existing := config.CloneSessionConfig(in.ExistingSession.Config)
		if in.Target == "" || in.Target == TargetInteractive {
			existing.Inference = cfg.Inference
			existing.Autosave.Enabled = cfg.Autosave.Enabled
			existing.AuthMode = cfg.AuthMode
			existing.Limits.MaxToolResultTokens = cfg.Limits.MaxToolResultTokens
			existing.DebugEnabled = cfg.DebugEnabled
		}
		cfg = existing
		if cfg.WorkingDir != catalogDir {
			catalog, err = loadCatalog(in.Paths, cfg.WorkingDir)
			catalogDir = cfg.WorkingDir
		}
		if err != nil {
			return Resolved{}, err
		}
	} else if in.AgentName != "" {
		definition, loadErr := catalog.Agents.Load(in.AgentName)
		if loadErr != nil {
			return Resolved{}, loadErr
		}
		cfg.AgentName = definition.Name
		cfg.AgentSystem = definition.System
		cfg.WorkingDir = util.ExpandHome(first(definition.WorkingDirectory, cfg.WorkingDir))
		cfg.SkillPolicy = policyFor(definition.Skills)
		cfg.AgentPolicy = policyFor(definition.Agents)
		if definition.Tools != nil {
			cfg.Tools = clone(definition.Tools)
		}
		if definition.AuthMode != "" {
			mode, parseErr := config.ParseAuthorizationMode(definition.AuthMode)
			if parseErr != nil {
				return Resolved{}, fmt.Errorf("agent auth_mode: %w", parseErr)
			}
			cfg.AuthMode = mode
		}
		cfg.Autosave = config.SessionAutosave{Enabled: definition.Save.Enabled}
		if definition.Thinking != nil {
			cfg.Inference.Thinking.Enabled = *definition.Thinking
		}
		if definition.Model != "" {
			if err := applyModel(&cfg.Inference, definition.Model); err != nil {
				return Resolved{}, err
			}
		}
		cfg.Limits = resolveLimits(cfg.Limits, definition.Limits.MaxSteps, definition.Limits.MaxTools, definition.Limits.MaxTime, definition.Limits.MaxToolResultTokens, definition.Limits.MaxAgentDepth)
		cfg.Memory.Namespace = definition.Memory.Namespace
		cfg.Memory.Instructions = definition.Memory.Instructions
	}

	c := in.CLI
	if c.AgentName != "" {
		cfg.AgentName = c.AgentName
	}
	if c.Model != "" {
		if err := applyModel(&cfg.Inference, c.Model); err != nil {
			return Resolved{}, err
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
		cfg.SkillPolicy = policyFor(c.SkillNames)
	}
	if c.CallableAgentNames != nil {
		cfg.AgentPolicy = policyFor(c.CallableAgentNames)
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
		duration, parseErr := time.ParseDuration(c.MaxTime)
		if parseErr != nil {
			return Resolved{}, fmt.Errorf("max time: %w", parseErr)
		}
		cfg.Limits.MaxTime = duration.String()
	}
	if c.MaxAgentDepthSet {
		cfg.Limits.MaxAgentDepth = c.MaxAgentDepth
	}

	if cfg.WorkingDir != catalogDir {
		catalog, err = loadCatalog(in.Paths, cfg.WorkingDir)
		if err != nil {
			return Resolved{}, err
		}
	}
	cfg.Skills, cfg.Agents, err = resolveCatalog(catalog, cfg.SkillPolicy, cfg.AgentPolicy, in.ExistingSession == nil)
	if err != nil {
		return Resolved{}, err
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
		return Resolved{}, err
	}
	cfg.Memory.Path = path
	cfg.AuthMode, err = normalizeAuthorization(cfg.AuthMode, in.Target)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Config: cfg, Catalog: catalog}, nil
}

// ApplyToExistingSession separates next-turn transitions from direct runtime policy updates.
func ApplyToExistingSession(doc *config.SessionDoc, desired config.SessionConfig) {
	if doc == nil {
		return
	}

	current := config.CloneSessionConfig(doc.Config)
	pending := &config.PendingConfig{}

	if desired.Target != current.Target {
		value := desired.Target
		pending.Target = &value
	}
	if desired.Inference != current.Inference {
		value := desired.Inference
		pending.Inference = &value
	}
	if desired.ActiveSkill != current.ActiveSkill {
		value := desired.ActiveSkill
		pending.ActiveSkill = &value
	}
	if !equalStringSlices(desired.Tools, current.Tools) {
		value := clone(desired.Tools)
		pending.Tools = &value
	}
	if !equalCapabilityRefs(desired.Skills, current.Skills) {
		value := cloneRefs(desired.Skills)
		pending.Skills = &value
	}
	if !equalCapabilityRefs(desired.Agents, current.Agents) {
		value := cloneRefs(desired.Agents)
		pending.Agents = &value
	}

	// Apply the complete resolved config, then retain current transcript-sensitive
	// fields until PrepareTurn commits their pending transitions.
	doc.Config = config.CloneSessionConfig(desired)
	doc.Config.Target = current.Target
	doc.Config.Inference = current.Inference
	doc.Config.ActiveSkill = current.ActiveSkill
	doc.Config.Tools = clone(current.Tools)
	doc.Config.Skills = cloneRefs(current.Skills)
	doc.Config.Agents = cloneRefs(current.Agents)

	if pending.Target == nil && pending.Inference == nil && pending.ActiveSkill == nil && pending.Tools == nil && pending.Skills == nil && pending.Agents == nil {
		doc.Pending = nil
	} else {
		doc.Pending = pending
	}
}

func normalizeAuthorization(mode config.AuthorizationMode, target Target) (config.AuthorizationMode, error) {
	if target == "" {
		target = TargetInteractive
	}
	switch target {
	case TargetInteractive:
		switch mode {
		case config.AuthorizationAuto, config.AuthorizationAskOnWrite, config.AuthorizationAskForAll:
			return mode, nil
		default:
			return "", fmt.Errorf("authorization mode %q is not valid for interactive mode", mode)
		}
	case TargetAutonomous:
		switch mode {
		case config.AuthorizationAuto:
			return mode, nil
		case config.AuthorizationAskOnWrite, config.AuthorizationEndOnWrite:
			return config.AuthorizationEndOnWrite, nil
		case config.AuthorizationAskForAll, config.AuthorizationEndOnAll:
			return config.AuthorizationEndOnAll, nil
		default:
			return "", fmt.Errorf("authorization mode %q is not valid for autonomous mode", mode)
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

func cloneRefs(refs []config.CapabilityRef) []config.CapabilityRef {
	return append([]config.CapabilityRef(nil), refs...)
}

func equalCapabilityRefs(a, b []config.CapabilityRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Scope != b[i].Scope || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func LoadCatalog(paths config.Paths, workingDir string) (Catalog, error) {
	return loadCatalog(paths, workingDir)
}

func (catalog Catalog) Resolve(skillPolicy, agentPolicy config.CapabilityPolicy) (skillsRefs, agentRefs []config.CapabilityRef, skillsMissing, agentsMissing []string) {
	skillsRefs, skillsMissing = resolveSkills(catalog.Skills, skillPolicy)
	agentRefs, agentsMissing = resolveAgents(catalog.Agents, agentPolicy)
	return skillsRefs, agentRefs, skillsMissing, agentsMissing
}

func allPolicy() config.CapabilityPolicy {
	return config.CapabilityPolicy{Mode: config.PolicyModeAll}
}

// nil means unrestricted; a non-nil slice, including empty, means allowlist.
func policyFor(requested []string) config.CapabilityPolicy {
	if requested == nil {
		return allPolicy()
	}
	return config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: clone(requested)}
}

func loadCatalog(paths config.Paths, workingDir string) (Catalog, error) {
	workspaceRoot := filepath.Join(workingDir, ".squid-os")
	skillRegistry, err := skills.LoadRegistry(paths.Skills, filepath.Join(workspaceRoot, "skills"))
	if err != nil {
		return Catalog{}, fmt.Errorf("load skill catalog: %w", err)
	}
	agentRegistry, err := agent.LoadRegistry(paths.Agents, filepath.Join(workspaceRoot, "agents"))
	if err != nil {
		return Catalog{}, fmt.Errorf("load agent catalog: %w", err)
	}
	return Catalog{WorkingDir: workingDir, Skills: skillRegistry, Agents: agentRegistry}, nil
}

func resolveCatalog(catalog Catalog, skillPolicy, agentPolicy config.CapabilityPolicy, rejectMissing bool) ([]config.CapabilityRef, []config.CapabilityRef, error) {
	skillRefs, agentRefs, skillMissing, agentMissing := catalog.Resolve(skillPolicy, agentPolicy)
	if rejectMissing && len(skillMissing) > 0 {
		return nil, nil, fmt.Errorf("skills not found: %s", strings.Join(skillMissing, ", "))
	}
	if rejectMissing && len(agentMissing) > 0 {
		return nil, nil, fmt.Errorf("agents not found: %s", strings.Join(agentMissing, ", "))
	}
	return skillRefs, agentRefs, nil
}

func resolveSkills(registry *skills.Registry, policy config.CapabilityPolicy) ([]config.CapabilityRef, []string) {
	if policy.Mode == config.PolicyModeAll {
		entries := registry.List()
		refs := make([]config.CapabilityRef, 0, len(entries))
		for _, entry := range entries {
			refs = append(refs, config.CapabilityRef{Scope: entry.Scope, Name: entry.Name})
		}
		return refs, nil
	}
	return resolveRequested(policy.Requested, func(name string) (config.CapabilityRef, bool) {
		entry, ok := registry.Resolve(name)
		return config.CapabilityRef{Scope: entry.Scope, Name: entry.Name}, ok
	})
}

func resolveAgents(registry *agent.Registry, policy config.CapabilityPolicy) ([]config.CapabilityRef, []string) {
	if policy.Mode == config.PolicyModeAll {
		entries := registry.List()
		refs := make([]config.CapabilityRef, 0, len(entries))
		for _, entry := range entries {
			refs = append(refs, config.CapabilityRef{Scope: entry.Scope, Name: entry.Name})
		}
		return refs, nil
	}
	return resolveRequested(policy.Requested, func(name string) (config.CapabilityRef, bool) {
		entry, ok := registry.Resolve(name)
		return config.CapabilityRef{Scope: entry.Scope, Name: entry.Name}, ok
	})
}

func resolveRequested(names []string, resolve func(string) (config.CapabilityRef, bool)) ([]config.CapabilityRef, []string) {
	refs := make([]config.CapabilityRef, 0, len(names))
	var missing []string
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		ref, ok := resolve(name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		refs = append(refs, ref)
	}
	return refs, missing
}

func availableToolNames() []string {
	registered := tools.GetTools()
	names := make([]string, 0, len(registered))
	for _, tool := range registered {
		names = append(names, tool.Name)
	}
	return names
}

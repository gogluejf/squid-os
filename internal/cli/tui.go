package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/agent"
	"squid-os/internal/app"
	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

type TUIOptions struct {
	Prompt, SessionName, Model, WorkingDir, AgentName, AgentSystem, ActiveSkill, AutosaveName, AuthMode string
	Thinking, Autosave                                                                                  *bool
	ToolNames, SkillNames, CallableAgentNames                                                           []string
	Limits                                                                                              LimitOptions
	thinking, save                                                                                      bool
}

type tuiCmd struct {
	execute func(*TUIOptions) error
}

var _ Command[TUIOptions] = tuiCmd{}

func (tuiCmd) Spec() CommandSpec {
	return CommandSpec{Use: "tui", Short: "Start the interactive terminal interface", Args: cobra.NoArgs, Runnable: true}
}

func (tuiCmd) Flags(f *pflag.FlagSet) *TUIOptions {
	o := &TUIOptions{}
	f.StringVarP(&o.Prompt, "prompt", "p", "", "prefill the input textarea")
	f.StringVar(&o.SessionName, "session", "", "open a saved session")
	f.StringVar(&o.Model, "model", "", "override model (provider/model)")
	f.BoolVar(&o.thinking, "thinking", false, "override thinking mode")
	f.StringVar(&o.WorkingDir, "working-dir", "", "set the working directory")
	f.StringVar(&o.AgentName, "agent", "", "select the root agent")
	f.StringVar(&o.AgentSystem, "system", "", "override agent system instructions")
	f.StringSliceVar(&o.ToolNames, "tools", nil, "available tool names")
	f.StringSliceVar(&o.SkillNames, "skills", nil, "available skill names")
	f.StringVar(&o.ActiveSkill, "skill", "", "active skill")
	f.StringVar(&o.AuthMode, "auth-mode", "", "authorization mode")
	f.StringSliceVar(&o.CallableAgentNames, "agents", nil, "callable agent names")
	f.BoolVar(&o.save, "save", false, "autosave TUI session")
	f.StringVar(&o.AutosaveName, "save-name", "", "autosaved session name")
	f.IntVar(&o.Limits.MaxSteps, "max-steps", 0, "maximum loop steps")
	f.IntVar(&o.Limits.MaxTools, "max-tools", 0, "maximum tool executions")
	f.StringVar(&o.Limits.MaxTime, "max-time", "", "maximum duration")
	f.IntVar(&o.Limits.MaxToolResultTokens, "max-tool-result-tokens", 0, "maximum tool result tokens")
	f.IntVar(&o.Limits.MaxAgentDepth, "max-agent-depth", 0, "maximum nested agent depth")
	return o
}

func (tuiCmd) Prepare(cmd *cobra.Command, o *TUIOptions, _ []string) error {
	if cmd.Flags().Changed("thinking") {
		o.Thinking = &o.thinking
	}
	if cmd.Flags().Changed("save") {
		o.Autosave = &o.save
	}
	if o.AutosaveName != "" {
		o.save = true
		o.Autosave = &o.save
	}
	o.Limits.MaxAgentDepthSet = cmd.Flags().Changed("max-agent-depth")
	if err := validateTUIAuthMode(o.AuthMode); err != nil {
		return err
	}
	if o.SessionName != "" && o.AgentName != "" {
		// Validate in launchTUI instead — registry isn't initialized yet here.
	}
	if o.SessionName != "" {
		for _, name := range []string{"system", "working-dir"} {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s cannot be used with --session", name)
			}
		}
	}
	if o.Limits.MaxSteps < 0 || o.Limits.MaxTools < 0 || o.Limits.MaxToolResultTokens < 0 || o.Limits.MaxAgentDepth < 0 {
		return fmt.Errorf("TUI limits cannot be negative")
	}
	return nil
}

func (c tuiCmd) Run(o *TUIOptions, _ CommandIO) error { return c.execute(o) }

func (tuiCmd) Completions() []Completion {
	return []Completion{
		{Flag: "session", Provider: flagSessions}, {Flag: "skill", Provider: flagSkills},
		{Flag: "skills", Provider: flagSkills}, {Flag: "agent", Provider: flagAgents},
		{Flag: "agents", Provider: flagAgents}, {Flag: "tools", Provider: flagTools},
		{Flag: "model", Provider: flagModels}, {Flag: "thinking", Values: []string{"true", "false"}},
		{Flag: "save", Values: []string{"true", "false"}},
		{Flag: "auth-mode", Values: []string{"auto", "ask-on-write", "ask-for-all"}},
	}
}

func launchTUI(opts *TUIOptions) error {
	cfg, err := loadApplicationConfig()
	if err != nil {
		return err
	}

	var existing *config.SessionDoc
	sessionName := opts.SessionName

	var definition *agent.Definition
	if opts.AgentName != "" {
		definition, err = agent.GetRegistry().Load(opts.AgentName)
		if err != nil {
			return err
		}
		// Validate agent save name matches explicit --session
		if opts.SessionName != "" && definition.Save.Name != "" && definition.Save.Name != opts.SessionName {
			return fmt.Errorf("--agent %q (save name: %q) conflicts with --session %q", opts.AgentName, definition.Save.Name, opts.SessionName)
		}
	}

	// Load session in priority order: explicit --session > agent save name > autoload last
	if sessionName != "" {
		doc, err := config.LoadSessionDoc(cfg.paths, sessionName)
		if err != nil {
			return fmt.Errorf("load session %q: %w", sessionName, err)
		}
		existing = &doc
	} else if definition != nil && definition.Save.Name != "" {
		if doc, err := config.LoadSessionDoc(cfg.paths, definition.Save.Name); err == nil {
			existing = &doc
			sessionName = definition.Save.Name
		}
	}
	if existing == nil && cfg.settings.AutoLoadLastSession && cfg.settings.LastSessionName != "" {
		if doc, err := config.LoadSessionDoc(cfg.paths, cfg.settings.LastSessionName); err == nil {
			existing, sessionName = &doc, cfg.settings.LastSessionName
		}
	}
	authMode, err := parseOptionalAuthorization(opts.AuthMode)
	if err != nil {
		return err
	}
	resolved, err := runtimeconfig.Resolve(runtimeconfig.Inputs{
		Settings: cfg.settings, Paths: cfg.paths, Agent: definition, ExistingSession: existing,
		SessionName: sessionName, Target: runtimeconfig.TargetTUI,
		CLI: runtimeconfig.Overrides{
			AgentName: opts.AgentName, Model: opts.Model, Thinking: opts.Thinking,
			WorkingDir: opts.WorkingDir, AgentSystem: opts.AgentSystem, ToolNames: opts.ToolNames,
			SkillNames: opts.SkillNames, ActiveSkill: opts.ActiveSkill, CallableAgentNames: opts.CallableAgentNames,
			Autosave: opts.Autosave, AutosaveName: opts.AutosaveName, AuthMode: authMode,
			MaxSteps: opts.Limits.MaxSteps, MaxTools: opts.Limits.MaxTools, MaxTime: opts.Limits.MaxTime,
			MaxToolResultTokens: opts.Limits.MaxToolResultTokens, MaxAgentDepth: opts.Limits.MaxAgentDepth,
			MaxAgentDepthSet: opts.Limits.MaxAgentDepthSet,
		},
	})
	if err != nil {
		return err
	}
	runtimeconfig.ApplyToExistingSession(existing, resolved)
	request := runtimeconfig.SessionRequest{Paths: cfg.paths, Endpoints: cfg.endpoints, Config: resolved, ExistingSession: existing, SessionName: sessionName, Prompt: opts.Prompt}
	if sessionName != "" {
		cfg.settings.LastSessionName = sessionName
	}
	program := tea.NewProgram(app.New(app.StartupOptions{Session: request, Settings: cfg.settings, History: cfg.history}), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

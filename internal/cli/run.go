package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	runservice "squid-os/internal/run"
	runtimeconfig "squid-os/internal/runtime"
)

type RunOptions struct {
	AgentName, SessionName, Prompt, Mode, Model, WorkingDir, AgentSystem string
	ActiveSkill, AutosaveName, AuthMode                                  string
	MemoryNamespace, MemoryInstructions                                  string
	Thinking, Autosave                                                   *bool
	ToolNames, SkillNames, CallableAgentNames                            []string
	Limits                                                               LimitOptions
	thinking, save                                                       bool
	// Child session lineage flags (internal: set by agent delegation only)
	ChildSessionID, ParentSessionID, RootSessionID, ParentToolCallID string
	SessionDepth                                                     int
	ParentSessionDir                                                 string
	childSessionID, parentSessionID, rootSessionID, parentToolCallID bool
	childSessionDepth, parentSessionDir                              bool
}

type runCmd struct{ execute func(*RunOptions) error }

var _ Command[RunOptions] = runCmd{}

func (runCmd) Spec() CommandSpec {
	return CommandSpec{
		Use: "run [agent]", Short: "Execute an autonomous run", Args: cobra.MaximumNArgs(1), Runnable: true,
		ValidArgs: func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
			return flagAgents(prefix), cobra.ShellCompDirectiveNoFileComp
		},
	}
}

func (runCmd) Flags(f *pflag.FlagSet) *RunOptions {
	o := &RunOptions{Mode: "final_message"}
	f.StringVar(&o.AgentName, "agent", "", "installed agent name")
	f.StringVar(&o.SessionName, "session", "", "continue a saved session")
	f.StringVarP(&o.Prompt, "prompt", "p", "", "prompt to execute")
	f.StringVar(&o.Mode, "mode", o.Mode, "output mode")
	f.StringVar(&o.Model, "model", "", "override model (provider/model)")
	f.BoolVar(&o.thinking, "thinking", false, "override thinking mode")
	f.StringVar(&o.WorkingDir, "working-dir", "", "set working directory")
	f.StringVar(&o.AgentSystem, "system", "", "override agent system instructions")
	f.StringSliceVar(&o.ToolNames, "tools", nil, "available tools")
	f.StringSliceVar(&o.SkillNames, "skills", nil, "available skills")
	f.StringVar(&o.ActiveSkill, "skill", "", "active skill")
	f.StringSliceVar(&o.CallableAgentNames, "agents", nil, "callable agents")
	f.BoolVar(&o.save, "save", false, "autosave run session")
	f.StringVar(&o.AutosaveName, "save-name", "", "autosaved session name")
	f.StringVar(&o.AuthMode, "auth-mode", "", "non-interactive authorization mode")
	f.StringVar(&o.MemoryNamespace, "memory-namespace", "", "memory namespace")
	f.StringVar(&o.MemoryInstructions, "memory-instructions", "", "memory instructions")
	f.IntVar(&o.Limits.MaxSteps, "max-steps", 0, "maximum loop steps")
	f.IntVar(&o.Limits.MaxTools, "max-tools", 0, "maximum tool executions")
	f.StringVar(&o.Limits.MaxTime, "max-time", "", "maximum duration")
	f.IntVar(&o.Limits.MaxToolResultTokens, "max-tool-result-tokens", 0, "maximum tool result tokens")
	f.IntVar(&o.Limits.MaxAgentDepth, "max-agent-depth", 0, "maximum nested agent depth")
	// Internal child-session flags — hidden from help, used only by agent delegation.
	f.StringVar(&o.ChildSessionID, "session-id", "", "internal: preallocated child session ID")
	f.MarkHidden("session-id")
	f.StringVar(&o.ParentSessionID, "parent-session-id", "", "internal: parent session ID")
	f.MarkHidden("parent-session-id")
	f.StringVar(&o.RootSessionID, "root-session-id", "", "internal: root session ID")
	f.MarkHidden("root-session-id")
	f.StringVar(&o.ParentToolCallID, "parent-tool-call-id", "", "internal: parent tool-call ID")
	f.MarkHidden("parent-tool-call-id")
	f.IntVar(&o.SessionDepth, "session-depth", 0, "internal: child session depth")
	f.MarkHidden("session-depth")
	f.StringVar(&o.ParentSessionDir, "parent-session-dir", "", "internal: parent session directory")
	f.MarkHidden("parent-session-dir")
	return o
}

func (runCmd) Prepare(cmd *cobra.Command, o *RunOptions, args []string) error {
	if len(args) == 1 {
		if o.AgentName != "" && o.AgentName != args[0] {
			return fmt.Errorf("positional agent %q conflicts with --agent %q", args[0], o.AgentName)
		}
		o.AgentName = args[0]
	}
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
	if err := validateRunAuthMode(o.AuthMode); err != nil {
		return err
	}
	if o.Limits.MaxSteps < 0 || o.Limits.MaxTools < 0 || o.Limits.MaxToolResultTokens < 0 || o.Limits.MaxAgentDepth < 0 {
		return fmt.Errorf("run limits cannot be negative")
	}
	// Reject bootstrap-changing flags when continuing a session.
	// These checks run in Prepare so they are caught by CLI tests before execute.
	sessionChanged := cmd.Flags().Changed("session")
	if sessionChanged {
		if o.AgentSystem != "" {
			return fmt.Errorf("--system cannot be used when continuing a session")
		}
		if o.WorkingDir != "" {
			return fmt.Errorf("--working-dir cannot be used when continuing a session")
		}
		if o.MemoryNamespace != "" {
			return fmt.Errorf("--memory-namespace cannot be used when continuing a session")
		}
		if o.MemoryInstructions != "" {
			return fmt.Errorf("--memory-instructions cannot be used when continuing a session")
		}
	}
	// Track which internal child-session flags were set
	o.childSessionID = cmd.Flags().Changed("session-id")
	o.parentSessionID = cmd.Flags().Changed("parent-session-id")
	o.rootSessionID = cmd.Flags().Changed("root-session-id")
	o.parentToolCallID = cmd.Flags().Changed("parent-tool-call-id")
	o.childSessionDepth = cmd.Flags().Changed("session-depth")
	o.parentSessionDir = cmd.Flags().Changed("parent-session-dir")
	if err := validateChildSessionFlags(o); err != nil {
		return err
	}
	return nil
}

// validateChildSessionFlags validates the internal lineage flags used for child sessions.
func validateChildSessionFlags(o *RunOptions) error {
	anySet := o.childSessionID || o.parentSessionID || o.rootSessionID || o.parentToolCallID || o.childSessionDepth || o.parentSessionDir
	if !anySet {
		return nil
	}
	// Existing child continuation is not implemented yet. A future child load
	// path must resolve the persisted child from its parent link and directory.
	if o.SessionName != "" {
		return fmt.Errorf("--session cannot be combined with child session lineage flags")
	}
	// All lineage flags must be provided together
	if !o.childSessionID {
		return fmt.Errorf("--session-id is required for child sessions")
	}
	if !o.parentSessionID {
		return fmt.Errorf("--parent-session-id is required for child sessions")
	}
	if !o.rootSessionID {
		return fmt.Errorf("--root-session-id is required for child sessions")
	}
	if !o.parentToolCallID {
		return fmt.Errorf("--parent-tool-call-id is required for child sessions")
	}
	if !o.childSessionDepth {
		return fmt.Errorf("--session-depth is required for child sessions")
	}
	if !o.parentSessionDir {
		return fmt.Errorf("--parent-session-dir is required for child sessions")
	}
	if o.SessionDepth < 1 {
		return fmt.Errorf("session depth must be at least 1 for child sessions")
	}
	if o.ChildSessionID == "" {
		return fmt.Errorf("--session-id must not be empty")
	}
	if o.ParentSessionID == "" {
		return fmt.Errorf("--parent-session-id must not be empty")
	}
	if o.RootSessionID == "" {
		return fmt.Errorf("--root-session-id must not be empty")
	}
	if o.ParentToolCallID == "" {
		return fmt.Errorf("--parent-tool-call-id must not be empty")
	}
	if o.ParentSessionDir == "" {
		return fmt.Errorf("--parent-session-dir must not be empty")
	}
	return nil
}

func (c runCmd) Run(o *RunOptions, streams CommandIO) error {
	prompt, err := composePrompt(o.Prompt, streams.In)
	if err != nil {
		return err
	}
	if prompt == "" {
		return fmt.Errorf("prompt is required through --prompt or stdin")
	}
	o.Prompt = prompt
	return c.execute(o)
}

func (runCmd) Completions() []Completion {
	return []Completion{
		{Flag: "session", Provider: flagSessions}, {Flag: "skill", Provider: flagSkills}, {Flag: "skills", Provider: flagSkills},
		{Flag: "agent", Provider: flagAgents}, {Flag: "agents", Provider: flagAgents}, {Flag: "tools", Provider: flagTools},
		{Flag: "model", Provider: flagModels}, {Flag: "thinking", Values: []string{"true", "false"}},
		{Flag: "save", Values: []string{"true", "false"}}, {Flag: "mode", Values: []string{"final_message", "stream", "silent", "structured"}},
		{Flag: "memory-namespace", Values: []string{"workspace", "global", "agent"}}, {Flag: "auth-mode", Values: []string{"auto", "end-on-write", "end-on-all"}},
	}
}

func executeRun(o *RunOptions) error {
	cfg, err := loadApplicationConfig()
	if err != nil {
		return err
	}
	if o.AgentName != "" && o.SessionName != "" {
		return fmt.Errorf("--agent cannot be used with --session")
	}

	var existing *config.SessionDoc
	if o.SessionName != "" {
		if err := config.ValidateSessionName(o.SessionName); err != nil {
			return fmt.Errorf("session name: %w", err)
		}
		doc, err := config.LoadSessionDoc(config.RootSessionDir(cfg.paths, o.SessionName))
		if err != nil {
			return err
		}
		existing = &doc
	}

	// Reject bootstrap-changing flags when continuing a session
	if existing != nil {
		if o.AgentSystem != "" {
			return fmt.Errorf("--system cannot be used when continuing a session")
		}
		if o.WorkingDir != "" {
			return fmt.Errorf("--working-dir cannot be used when continuing a session")
		}
		if o.MemoryNamespace != "" {
			return fmt.Errorf("--memory-namespace cannot be used when continuing a session")
		}
		if o.MemoryInstructions != "" {
			return fmt.Errorf("--memory-instructions cannot be used when continuing a session")
		}
	}
	authMode, err := parseOptionalAuthorization(o.AuthMode)
	if err != nil {
		return err
	}

	// Build child session options if internal lineage flags are present
	var childOpts *config.ChildSessionOptions
	if o.childSessionID {
		childOpts = &config.ChildSessionOptions{
			ID:               o.ChildSessionID,
			ParentID:         o.ParentSessionID,
			RootID:           o.RootSessionID,
			ParentToolCallID: o.ParentToolCallID,
			Depth:            o.SessionDepth,
			ParentSessionDir: o.ParentSessionDir,
		}
	}

	resolved, err := runtimeconfig.Resolve(runtimeconfig.Inputs{
		Settings: cfg.settings, Paths: cfg.paths, AgentName: o.AgentName, ExistingSession: existing, SessionName: o.SessionName, Target: runtimeconfig.TargetAutonomous,
		CLI: runtimeconfig.Overrides{AgentName: o.AgentName, Model: o.Model, Thinking: o.Thinking, WorkingDir: o.WorkingDir, AgentSystem: o.AgentSystem,
			ToolNames: o.ToolNames, SkillNames: o.SkillNames, ActiveSkill: o.ActiveSkill, CallableAgentNames: o.CallableAgentNames,
			Autosave: o.Autosave, AutosaveName: o.AutosaveName, AuthMode: authMode, MemoryNamespace: o.MemoryNamespace, MemoryInstructions: o.MemoryInstructions,
			MaxSteps: o.Limits.MaxSteps, MaxTools: o.Limits.MaxTools, MaxTime: o.Limits.MaxTime, MaxToolResultTokens: o.Limits.MaxToolResultTokens,
			MaxAgentDepth: o.Limits.MaxAgentDepth, MaxAgentDepthSet: o.Limits.MaxAgentDepthSet},
	})
	if err != nil {
		return err
	}
	if resolved.Config.Inference.Model == "" {
		return fmt.Errorf("no model configured")
	}
	runtimeconfig.ApplyToExistingSession(existing, resolved.Config)
	request := runservice.Request{Session: runtimeconfig.SessionRequest{Paths: cfg.paths, Endpoints: cfg.endpoints, Config: resolved.Config, Catalog: resolved.Catalog, ExistingSession: existing, SessionName: o.SessionName, Prompt: o.Prompt}, ChildSession: childOpts}
	if runservice.OutputMode(o.Mode) == runservice.OutputStream {
		stream := runservice.NewStreamWriter(os.Stdout)
		saved := resolved.Config.Autosave.Enabled
		if err := stream.Write(runservice.StreamEnvelope{Event: "session_start", Saved: &saved, SessionName: resolved.Config.Autosave.Name}); err != nil {
			return err
		}
		request.OnEvent = func(event chat.LoopEvent) { _ = stream.WriteLoopEvent(event) }
	}
	result, err := runservice.Execute(context.Background(), request)
	if err != nil {
		return err
	}
	return runservice.WriteResult(runservice.OutputMode(o.Mode), result, os.Stdout, os.Stderr)
}

func composePrompt(prefix string, reader io.Reader) (string, error) {
	if file, ok := reader.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", fmt.Errorf("inspect stdin: %w", err)
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return strings.TrimSpace(prefix), nil
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	prefix, stdin := strings.TrimSpace(prefix), strings.TrimSpace(string(data))
	if prefix == "" {
		return stdin, nil
	}
	if stdin == "" {
		return prefix, nil
	}
	return prefix + "\n\n" + stdin, nil
}

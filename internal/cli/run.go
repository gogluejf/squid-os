package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/agent"
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
}

type runCmd struct{ execute func(*RunOptions) error }

var _ Command[RunOptions] = runCmd{}

func (runCmd) Spec() CommandSpec {
	return CommandSpec{
		Use: "run [agent]", Short: "Execute a non-interactive run", Args: cobra.MaximumNArgs(1), Runnable: true,
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
	if o.SessionName != "" {
		if o.AgentName != "" {
			return fmt.Errorf("--agent cannot be used with --session")
		}
		for _, name := range []string{"system", "working-dir", "memory-namespace", "memory-instructions"} {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s cannot be used with --session", name)
			}
		}
	}
	if o.Limits.MaxSteps < 0 || o.Limits.MaxTools < 0 || o.Limits.MaxToolResultTokens < 0 || o.Limits.MaxAgentDepth < 0 {
		return fmt.Errorf("run limits cannot be negative")
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
	var definition *agent.Definition
	if o.AgentName != "" {
		definition, err = agent.GetRegistry().Load(o.AgentName)
		if err != nil {
			return err
		}
	}
	var existing *config.SessionDoc
	if o.SessionName != "" {
		doc, err := config.LoadSessionDoc(cfg.paths, o.SessionName)
		if err != nil {
			return err
		}
		existing = &doc
	}
	authMode, err := parseOptionalAuthorization(o.AuthMode)
	if err != nil {
		return err
	}
	resolved, err := runtimeconfig.Resolve(runtimeconfig.Inputs{
		Settings: cfg.settings, Paths: cfg.paths, Agent: definition, ExistingSession: existing, SessionName: o.SessionName, Target: runtimeconfig.TargetNonInteractive,
		CLI: runtimeconfig.Overrides{AgentName: o.AgentName, Model: o.Model, Thinking: o.Thinking, WorkingDir: o.WorkingDir, AgentSystem: o.AgentSystem,
			ToolNames: o.ToolNames, SkillNames: o.SkillNames, ActiveSkill: o.ActiveSkill, CallableAgentNames: o.CallableAgentNames,
			Autosave: o.Autosave, AutosaveName: o.AutosaveName, AuthMode: authMode, MemoryNamespace: o.MemoryNamespace, MemoryInstructions: o.MemoryInstructions,
			MaxSteps: o.Limits.MaxSteps, MaxTools: o.Limits.MaxTools, MaxTime: o.Limits.MaxTime, MaxToolResultTokens: o.Limits.MaxToolResultTokens,
			MaxAgentDepth: o.Limits.MaxAgentDepth, MaxAgentDepthSet: o.Limits.MaxAgentDepthSet},
	})
	if err != nil {
		return err
	}
	if resolved.Inference.Model == "" {
		return fmt.Errorf("no model configured")
	}
	if err := validateWorkingDir(resolved.WorkingDir); err != nil {
		return err
	}
	runtimeconfig.ApplyToExistingSession(existing, resolved)
	request := runservice.Request{Session: runtimeconfig.SessionRequest{Paths: cfg.paths, Endpoints: cfg.endpoints, Config: resolved, ExistingSession: existing, SessionName: o.SessionName, Prompt: o.Prompt}}
	if runservice.OutputMode(o.Mode) == runservice.OutputStream {
		stream := runservice.NewStreamWriter(os.Stdout)
		saved := resolved.Autosave.Enabled
		if err := stream.Write(runservice.StreamEnvelope{Event: "session_start", Saved: &saved, SessionName: resolved.Autosave.Name}); err != nil {
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

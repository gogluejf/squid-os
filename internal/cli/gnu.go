package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"squid-os/internal/gnu"
	runservice "squid-os/internal/run"
	runtimeconfig "squid-os/internal/runtime"
)

type GNUOptions struct {
	Prompt, Model, WorkingDir string
	Yes, PrintOnly            bool
	streams                   CommandIO
}

type gnuCmd struct{ execute func(*GNUOptions) error }

var _ Command[GNUOptions] = gnuCmd{}

func (gnuCmd) Spec() CommandSpec {
	return CommandSpec{Use: "gnu [request]", Short: "Generate and optionally execute a shell command", Args: cobra.ArbitraryArgs, Runnable: true}
}

func (gnuCmd) Flags(f *pflag.FlagSet) *GNUOptions {
	o := &GNUOptions{}
	f.StringVarP(&o.Prompt, "prompt", "p", "", "natural-language command request")
	f.StringVar(&o.Model, "model", "", "override model (provider/model)")
	f.StringVar(&o.WorkingDir, "working-dir", "", "command working directory")
	f.BoolVarP(&o.Yes, "yes", "y", false, "execute without confirmation")
	f.BoolVar(&o.PrintOnly, "print", false, "print the generated command without executing")
	return o
}

func (gnuCmd) Prepare(_ *cobra.Command, o *GNUOptions, args []string) error {
	positional := strings.TrimSpace(strings.Join(args, " "))
	if o.Prompt != "" && positional != "" {
		return fmt.Errorf("use either --prompt or positional request, not both")
	}
	if o.Prompt == "" {
		o.Prompt = positional
	}
	if strings.TrimSpace(o.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	return nil
}

func (c gnuCmd) Run(o *GNUOptions, streams CommandIO) error { o.streams = streams; return c.execute(o) }
func (gnuCmd) Completions() []Completion                    { return []Completion{{Flag: "model", Provider: flagModels}} }

func executeGNU(o *GNUOptions) error {
	cfg, err := loadApplicationConfig()
	if err != nil {
		return err
	}
	workingDir := o.WorkingDir
	if workingDir == "" {
		workingDir, err = currentWorkingDir()
		if err != nil {
			return err
		}
	}
	save := false
	resolved, err := runtimeconfig.Resolve(runtimeconfig.Inputs{Settings: cfg.settings, Paths: cfg.paths, Target: runtimeconfig.TargetAutonomous, CLI: runtimeconfig.Overrides{Model: o.Model, WorkingDir: workingDir, Autosave: &save}})
	if err != nil {
		return err
	}
	presenter := gnu.Presenter{Writer: o.streams.Err, Color: terminalFile(o.streams.Err) && noColorUnset()}
	presenter.Waiting(resolved.Config.Inference.Provider + "/" + resolved.Config.Inference.Model)
	result, err := runservice.Execute(context.Background(), runservice.Request{Session: runtimeconfig.SessionRequest{Paths: cfg.paths, Endpoints: cfg.endpoints, Config: resolved.Config, Catalog: resolved.Catalog, Prompt: gnu.BuildPrompt(o.Prompt, gnu.DetectPlatform(workingDir))}})
	if err != nil {
		return err
	}
	suggestion, err := gnu.ParseSuggestion(result.FinalText)
	if err != nil {
		return err
	}
	if o.PrintOnly {
		fmt.Fprintln(o.streams.Out, suggestion.Command)
		return nil
	}
	presenter.Suggestion(suggestion)
	if !o.Yes {
		prompt := "  Execute? [y/N] "
		if presenter.Color {
			prompt = "  \x1b[1mExecute?\x1b[0m \x1b[2m[y/N]\x1b[0m "
		}
		approved, err := gnu.ConfirmWithPrompt(o.streams.In, o.streams.Err, prompt)
		if err != nil {
			return err
		}
		if !approved {
			presenter.Aborted()
			return nil
		}
	}
	presenter.Executing()
	fmt.Fprintln(o.streams.Err)
	return gnu.Execute(suggestion.Command, workingDir, o.streams.In, o.streams.Out, o.streams.Err)
}

var currentWorkingDir = func() (string, error) { return os.Getwd() }
var noColorUnset = func() bool { return os.Getenv("NO_COLOR") == "" }

func terminalFile(w io.Writer) bool { f, ok := w.(*os.File); return ok && term.IsTerminal(int(f.Fd())) }

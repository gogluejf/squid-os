package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/version"
)

type CommandSpec struct {
	Use       string
	Short     string
	Args      cobra.PositionalArgs
	ValidArgs cobra.CompletionFunc
	Hidden    bool
	Runnable  bool
}

type CommandIO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Command[O any] interface {
	Spec() CommandSpec
	Flags(*pflag.FlagSet) *O
	Prepare(*cobra.Command, *O, []string) error
	Run(*O, CommandIO) error
	Completions() []Completion
}

type Completion struct {
	Flag     string
	Provider func(string) []string
	Values   []string
}

func Execute() error {
	return BuildRoot().Execute()
}

func BuildRoot() *cobra.Command {
	return buildRoot(
		tuiCmd{execute: launchTUI},
		runCmd{execute: executeRun},
		gnuCmd{execute: executeGNU},
	)
}

func buildRoot(tui Command[TUIOptions], run Command[RunOptions], gnu Command[GNUOptions]) *cobra.Command {
	root := &cobra.Command{
		Use:           "squid-os",
		Short:         "Agentic terminal environment",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Full(),
		Args:          cobra.NoArgs,
	}

	bindCommand(root, tui)
	root.AddCommand(makeCobra(tui), makeCobra(run), makeCobra(gnu))

	models := makeCobra(modelsCmd{})
	models.AddCommand(makeCobra(modelsListCmd{}), makeCobra(modelsRefreshCmd{}))
	root.AddCommand(models, makeCobra(modelsCacheRefreshCmd{}))
	return root
}

func makeCobra[O any](command Command[O]) *cobra.Command {
	spec := command.Spec()
	cmd := &cobra.Command{Use: spec.Use, Short: spec.Short, Args: spec.Args, ValidArgsFunction: spec.ValidArgs, Hidden: spec.Hidden}
	bindCommand(cmd, command)
	return cmd
}

func bindCommand[O any](cmd *cobra.Command, command Command[O]) {
	opts := command.Flags(cmd.Flags())
	wireCompletions(cmd, command.Completions())
	if !command.Spec().Runnable {
		return
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := command.Prepare(cmd, opts, args); err != nil {
			return err
		}
		return command.Run(opts, CommandIO{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()})
	}
}

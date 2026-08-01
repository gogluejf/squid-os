package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/modelcache"
)

type noOpts struct{}

type modelsCmd struct{}
type modelsListCmd struct{}
type modelsRefreshCmd struct{}
type modelsCacheRefreshCmd struct{}

var _ Command[noOpts] = modelsCmd{}
var _ Command[noOpts] = modelsListCmd{}
var _ Command[noOpts] = modelsRefreshCmd{}
var _ Command[noOpts] = modelsCacheRefreshCmd{}

func (modelsCmd) Spec() CommandSpec {
	return CommandSpec{Use: "models", Short: "List and refresh discovered models", Args: cobra.NoArgs}
}
func (modelsCmd) Flags(*pflag.FlagSet) *noOpts                    { return &noOpts{} }
func (modelsCmd) Prepare(*cobra.Command, *noOpts, []string) error { return nil }
func (modelsCmd) Run(*noOpts, CommandIO) error                    { return nil }
func (modelsCmd) Completions() []Completion                       { return nil }

func (modelsListCmd) Spec() CommandSpec {
	return CommandSpec{Use: "list", Short: "List cached models", Args: cobra.NoArgs, Runnable: true}
}
func (modelsListCmd) Flags(*pflag.FlagSet) *noOpts                    { return &noOpts{} }
func (modelsListCmd) Prepare(*cobra.Command, *noOpts, []string) error { return nil }
func (modelsListCmd) Run(_ *noOpts, streams CommandIO) error {
	cfg, err := loadApplicationConfig()
	if err != nil {
		return err
	}
	candidates, _ := (modelcache.Store{Dir: cfg.paths.CacheDir}).Candidates(cfg.endpoints, "", time.Now())
	for _, candidate := range candidates {
		fmt.Fprintln(streams.Out, candidate)
	}
	return nil
}
func (modelsListCmd) Completions() []Completion { return nil }

func (modelsRefreshCmd) Spec() CommandSpec {
	return CommandSpec{Use: "refresh", Short: "Refresh the model cache", Args: cobra.NoArgs, Runnable: true}
}
func (modelsRefreshCmd) Flags(*pflag.FlagSet) *noOpts                    { return &noOpts{} }
func (modelsRefreshCmd) Prepare(*cobra.Command, *noOpts, []string) error { return nil }
func (modelsRefreshCmd) Run(_ *noOpts, streams CommandIO) error          { return refreshModels(streams.Out) }
func (modelsRefreshCmd) Completions() []Completion                       { return nil }

func (modelsCacheRefreshCmd) Spec() CommandSpec {
	return CommandSpec{Use: "model-cache-refresh", Short: "Background model cache refresh worker", Args: cobra.NoArgs, Hidden: true, Runnable: true}
}
func (modelsCacheRefreshCmd) Flags(*pflag.FlagSet) *noOpts                    { return &noOpts{} }
func (modelsCacheRefreshCmd) Prepare(*cobra.Command, *noOpts, []string) error { return nil }
func (modelsCacheRefreshCmd) Run(_ *noOpts, streams CommandIO) error {
	return refreshModels(streams.Out)
}
func (modelsCacheRefreshCmd) Completions() []Completion { return nil }

func refreshModels(out anyWriter) error {
	if lock := os.Getenv("SQUID_OS_MODEL_CACHE_LOCK"); lock != "" {
		defer os.Remove(lock)
	}
	cfg, err := loadApplicationConfig()
	if err != nil {
		return err
	}
	models, err := (modelcache.Store{Dir: cfg.paths.CacheDir}).Refresh(context.Background(), cfg.endpoints)
	if err != nil {
		return err
	}
	count := 0
	for _, model := range models {
		if !model.NeedsConfig {
			count++
		}
	}
	fmt.Fprintf(out, "cached %d models\n", count)
	return nil
}

type anyWriter interface{ Write([]byte) (int, error) }

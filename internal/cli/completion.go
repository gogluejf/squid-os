package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/modelcache"
	"squid-os/internal/skills"
	"squid-os/internal/tools"
)

func wireCompletions(cmd *cobra.Command, completions []Completion) {
	for _, c := range completions {
		if c.Values != nil {
			registerStaticCompletion(cmd, c.Flag, c.Values)
		} else if c.Provider != nil {
			registerCompletion(cmd, c.Flag, c.Provider)
		}
	}
}

func registerCompletion(cmd *cobra.Command, flag string, candidates func(string) []string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(_ *cobra.Command, _ []string, value string) ([]string, cobra.ShellCompDirective) {
		prefix := value
		if i := strings.LastIndex(value, ","); i >= 0 {
			prefix = value[i+1:]
		}
		return candidates(prefix), cobra.ShellCompDirectiveNoFileComp
	})
}

func registerStaticCompletion(cmd *cobra.Command, flag string, values []string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		var matches []string
		for _, value := range values {
			if strings.HasPrefix(value, prefix) {
				matches = append(matches, value)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	})
}

// --- Shared flag completion providers ---

var completionPathsCache config.Paths
var completionPathsValid bool

func getCompletionPaths() config.Paths {
	if completionPathsValid {
		return completionPathsCache
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return config.Paths{}
	}
	configDir := filepath.Join(home, ".config", "squid-os")
	settings := config.LoadSettings(configDir)
	completionPathsCache = config.NewPaths(configDir, home, settings)
	completionPathsValid = true
	return completionPathsCache
}

func flagSessions(prefix string) []string {
	paths := getCompletionPaths()
	var names []string
	for _, entry := range config.ListSessions(paths) {
		if strings.HasPrefix(entry.Name, prefix) {
			names = append(names, entry.Name)
		}
	}
	return names
}

func flagSkills(prefix string) []string {
	paths := getCompletionPaths()
	_ = skills.InitRegistry(paths.Skills)
	registry := skills.GetRegistry()
	if registry == nil {
		return nil
	}
	var names []string
	for _, entry := range registry.List() {
		if strings.HasPrefix(entry.Name, prefix) {
			names = append(names, entry.Name)
		}
	}
	return names
}

func flagAgents(prefix string) []string {
	paths := getCompletionPaths()
	registry, err := agent.InitRegistry(paths.Agents)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range registry.List() {
		if strings.HasPrefix(entry.Name, prefix) {
			names = append(names, entry.Name)
		}
	}
	return names
}

func flagTools(prefix string) []string {
	var names []string
	for _, tool := range tools.GetTools() {
		if strings.HasPrefix(tool.Name, prefix) {
			names = append(names, tool.Name)
		}
	}
	return names
}

func flagModels(prefix string) []string {
	paths := getCompletionPaths()
	endpoints := config.LoadEndpoints(paths)
	store := modelcache.Store{Dir: paths.CacheDir}
	candidates, fresh := store.Candidates(endpoints, prefix, time.Now())
	if !fresh {
		startModelRefreshWorker(store)
	}
	return candidates
}

func startModelRefreshWorker(store modelcache.Store) {
	if !store.TryLock() {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		store.Unlock()
		return
	}
	command := exec.Command(executable, "model-cache-refresh")
	command.Env = append(os.Environ(), "SQUID_OS_MODEL_CACHE_LOCK="+store.LockPath())
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	if err := command.Start(); err != nil {
		store.Unlock()
		return
	}
	_ = command.Process.Release()
}

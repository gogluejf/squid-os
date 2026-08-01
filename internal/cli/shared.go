package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/skills"
)

// LimitOptions is shared by all interactive and run commands.
type LimitOptions struct {
	MaxSteps, MaxTools, MaxToolResultTokens, MaxAgentDepth int
	MaxAgentDepthSet                                       bool
	MaxTime                                                string
}

type applicationConfig struct {
	paths     config.Paths
	settings  config.Settings
	endpoints config.EndpointsConfig
	history   config.History
}

func loadApplicationConfig() (applicationConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return applicationConfig{}, fmt.Errorf("cannot determine home dir: %w", err)
	}
	configDir := filepath.Join(home, ".config", "squid-os")
	if err := config.InitConfig(configDir); err != nil {
		return applicationConfig{}, fmt.Errorf("init config: %w", err)
	}

	settings := config.LoadSettings(configDir)
	paths := config.NewPaths(configDir, home, settings)
	if err := paths.EnsureDirs(); err != nil {
		return applicationConfig{}, fmt.Errorf("create config dirs: %w", err)
	}
	log.Init(paths)
	log.SetEnabled(settings.DebugEnabled)
	if _, err := agent.InitRegistry(paths.Agents); err != nil {
		return applicationConfig{}, fmt.Errorf("initialize agents: %w", err)
	}
	if err := skills.InitRegistry(paths.Skills); err != nil {
		return applicationConfig{}, fmt.Errorf("initialize skills: %w", err)
	}

	return applicationConfig{
		paths:     paths,
		settings:  settings,
		endpoints: config.LoadEndpoints(paths),
		history:   config.LoadHistory(paths),
	}, nil
}

func parseOptionalAuthorization(value string) (config.AuthorizationMode, error) {
	if value == "" {
		return "", nil
	}
	return config.ParseAuthorizationMode(value)
}

func validateWorkingDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("working directory is not a directory: %s", path)
	}
	return nil
}

func validateTUIAuthMode(value string) error {
	if value == "" {
		return nil
	}
	mode, err := config.ParseAuthorizationMode(value)
	if err != nil {
		return err
	}
	switch mode {
	case config.AuthorizationAuto, config.AuthorizationAskOnWrite, config.AuthorizationAskForAll:
		return nil
	default:
		return fmt.Errorf("invalid TUI auth mode %q", value)
	}
}

func validateRunAuthMode(value string) error {
	if value == "" {
		return nil
	}
	mode, err := config.ParseAuthorizationMode(value)
	if err != nil {
		return err
	}
	switch mode {
	case config.AuthorizationAuto, config.AuthorizationEndOnWrite, config.AuthorizationEndOnAll:
		return nil
	default:
		return fmt.Errorf("invalid run auth mode %q", value)
	}
}

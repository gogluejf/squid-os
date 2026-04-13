package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"rig-chat/internal/app"
	"rig-chat/internal/config"
	"rig-chat/internal/headless"
)

var (
	flagConfigDir string
	flagThinking  string
	flagPrompt    string
	flagImage     string
	flagSystem    string
	flagHeadless  bool
	flagIncognito bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "rig-chat",
		Short: "Interactive TUI chat with OpenAI-compatible endpoints",
		RunE:  run,
	}

	rootCmd.Flags().StringVar(&flagConfigDir, "config-dir", "", "config directory path")
	rootCmd.Flags().StringVar(&flagThinking, "thinking", "", "thinking mode (on/off)")
	rootCmd.Flags().StringVar(&flagPrompt, "prompt", "", "send first prompt immediately")
	rootCmd.Flags().StringVar(&flagImage, "image", "", "attach image to first message")
	rootCmd.Flags().StringVar(&flagSystem, "system", "", "system prompt file")
	rootCmd.Flags().BoolVar(&flagHeadless, "headless", false, "no TUI, stream to stdout")
	rootCmd.Flags().BoolVar(&flagIncognito, "incognito", false, "start in incognito mode (no history or session saving)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Resolve config dir
	cfgDir := flagConfigDir
	if cfgDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home dir: %w", err)
		}
		cfgDir = home + "/.config/rig-chat"
	}

	paths := config.NewPaths(cfgDir)
	if err := paths.EnsureDirs(); err != nil {
		return fmt.Errorf("create config dirs: %w", err)
	}

	// Seed default config files on first run
	if _, err := os.Stat(paths.EndpointsFile()); os.IsNotExist(err) {
		_ = config.SaveEndpoints(paths, config.DefaultEndpoints())
	}
	if _, err := os.Stat(paths.SettingsFile()); os.IsNotExist(err) {
		_ = config.SaveSettings(paths, config.DefaultSettings())
	}
	_ = config.SeedDefaultSystemPrompt(paths)

	// Load config (always from files after seeding)
	settings := config.LoadSettings(paths)
	endpoints := config.LoadEndpoints(paths)
	history := config.LoadHistory(paths)

	// Apply CLI flag overrides
	switch flagThinking {
	case "on":
		settings.Thinking = true
	case "off":
		settings.Thinking = false
	}
	if flagSystem != "" {
		settings.SystemPromptFile = flagSystem
	}

	// Headless mode
	if flagHeadless {
		return runHeadless(paths, settings, endpoints)
	}

	// Auto-load session if configured
	var initialSession *config.SessionFile
	if settings.AutoLoadLastSession && settings.LastSessionName != "" && !flagIncognito {
		if sf, err := config.LoadSession(paths, settings.LastSessionName); err == nil {
			initialSession = &sf
		}
	}

	// TUI mode
	m := app.New(paths, settings, endpoints, history, initialSession, flagIncognito)

	// Handle --image flag
	if flagImage != "" {
		m.SetAttachedImage(flagImage)
	}

	// Handle --prompt flag (set initial text, user sends on first Enter)
	if flagPrompt != "" {
		m.SetInitialPrompt(flagPrompt)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func runHeadless(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig) error {
	if flagPrompt == "" {
		return fmt.Errorf("--headless requires --prompt")
	}
	return headless.Run(paths, settings, endpoints, flagPrompt, flagImage)
}

package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"squid-os/internal/app"
	"squid-os/internal/config"
	"squid-os/internal/headless"
	"squid-os/internal/log"
	"squid-os/internal/version"
)

var (
	flagThinking  string
	flagPrompt    string
	flagImage     string
	flagSystem    string
	flagIncognito bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "squid-os",
		Short: "Interactive TUI chat with OpenAI-compatible endpoints",
		Version: version.Full(),
		RunE:  run,
	}

	rootCmd.Flags().StringVar(&flagThinking, "thinking", "", "thinking mode (on/off)")
	rootCmd.Flags().StringVarP(&flagPrompt, "prompt", "p", "", "send prompt in headless mode")
	rootCmd.Flags().StringVarP(&flagImage, "image", "i", "", "attach image to first message")
	rootCmd.Flags().StringVar(&flagSystem, "system", "", "system prompt file")
	rootCmd.Flags().BoolVar(&flagIncognito, "incognito", false, "start in incognito mode (no history or session saving)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Resolve config dir
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}
	cfgDir := home + "/.config/squid-os"

	// Unpack embedded defaults on first run (before any other config load)
	if err := config.InitConfig(cfgDir); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	settings := config.LoadSettings(cfgDir)
	paths := config.NewPaths(cfgDir, home, settings)
	if err := paths.EnsureDirs(); err != nil {
		return fmt.Errorf("create config dirs: %w", err)
	}
	log.Init(paths)

	endpoints := config.LoadEndpoints(paths)
	history := config.LoadHistory(paths)

	log.SetEnabled(settings.DebugEnabled && !flagIncognito)

	// Apply CLI flag overrides
	switch flagThinking {
	case "on":
		settings.Thinking.Enabled = true
	case "off":
		settings.Thinking.Enabled = false
	}
	if flagSystem != "" {
		settings.SystemPromptFile = flagSystem
	}

	// Headless mode: -p implies headless
	if flagPrompt != "" {
		return runHeadless(paths, settings, endpoints)
	}

	// Auto-load session if configured
	var initialSession *config.SessionDoc
	if settings.AutoLoadLastSession && settings.LastSessionName != "" && !flagIncognito {
		if sf, err := config.LoadSessionDoc(paths, settings.LastSessionName); err == nil {
			initialSession = &sf
		}
	}

	// TUI mode
	m := app.New(paths, settings, endpoints, history, initialSession, flagIncognito)

	// Handle --image flag
	if flagImage != "" {
		m.SetAttachedImage(flagImage)
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func runHeadless(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig) error {
	if flagPrompt == "" {
		return fmt.Errorf("-p/--prompt is required for headless mode")
	}
	return headless.Run(paths, settings, endpoints, flagPrompt, flagImage)
}

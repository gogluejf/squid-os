package config

import (
	"os"
	"path/filepath"
)

// Paths holds all resolved config directory paths
type Paths struct {
	Root       string // config/squid-os
	Sessions   string // config/squid-os/sessions
	SysPrompts string // config/squid-os/sys-prompts
	Logs       string // config/squid-os/logs
}

func NewPaths(configDir string) Paths {
	return Paths{
		Root:       configDir,
		Sessions:   filepath.Join(configDir, "sessions"),
		SysPrompts: filepath.Join(configDir, "sys-prompts"),
		Logs:       filepath.Join(configDir, "logs"),
	}
}

// EnsureDirs creates all config directories if they don't exist
func (p Paths) EnsureDirs() error {
	dirs := []string{p.Root, p.Sessions, p.SysPrompts, p.Logs}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (p Paths) SettingsFile() string  { return filepath.Join(p.Root, "settings.json") }
func (p Paths) EndpointsFile() string { return filepath.Join(p.Root, "endpoints.json") }
func (p Paths) HistoryFile() string   { return filepath.Join(p.Root, "history.json") }

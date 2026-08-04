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
	Skills     string // config/squid-os/skills
	Agents     string // config/squid-os/agents
	CacheDir   string // XDG cache/squid-os
	// Domain directories — derived from home + Settings
	ProjectDir string // ~/src
	MemoryDir  string // ~/memory
	TempFolder string // ~/tmp
}

func NewPaths(configDir string, homeDir string, s Settings) Paths {
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		cacheRoot = filepath.Join(homeDir, ".cache")
	}
	return Paths{
		Root:         configDir,
		Sessions:     filepath.Join(configDir, "sessions"),
		SysPrompts:   filepath.Join(configDir, "sys-prompts"),
		Logs:         filepath.Join(configDir, "logs"),
		Skills:       filepath.Join(configDir, "skills"),
		Agents:       filepath.Join(configDir, "agents"),
		CacheDir:     filepath.Join(cacheRoot, "squid-os"),
		ProjectDir: filepath.Join(homeDir, s.ProjectDir),
		MemoryDir:  filepath.Join(homeDir, s.MemoryDir),
		TempFolder: filepath.Join(homeDir, s.TempFolder),
	}
}

// EnsureDirs creates all config directories if they don't exist
func (p Paths) EnsureDirs() error {
	dirs := []string{p.Root, p.Sessions, p.SysPrompts, p.Logs, p.Skills, p.Agents, p.CacheDir, p.ProjectDir, p.MemoryDir, p.TempFolder}
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

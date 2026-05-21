package config

import (
	"encoding/json"
	"os"
)

type Settings struct {
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	Thinking            bool   `json:"thinking"`
	SystemPromptFile    string `json:"system_prompt_file"`
	MaxHistory          int    `json:"max_history"`
	LastSessionName     string `json:"last_session_name"`
	AutoSave            bool   `json:"auto_save"`
	AutoLoadLastSession bool   `json:"auto_load_last_session"`
	ContextWindow       int    `json:"context_window"`
	DebugEnabled        bool   `json:"debug_enabled"`
	// Domain directories — relative to home, resolved by Paths
	ProjectDir   string `json:"project_dir"`    // default: "src"
	MemoryDir    string `json:"memory_dir"`     // default: "memory"
	TempFolder   string `json:"temp_folder"`    // default: "tmp"
	DocumentsDir string `json:"documents_dir"`  // default: "Documents/squid-os"
}

func DefaultSettings() Settings {
	return Settings{
		Provider:            "vllm",
		Model:               "",
		Thinking:            false,
		MaxHistory:          500,
		AutoSave:            true,
		AutoLoadLastSession: true,
		DebugEnabled:        true,
		ProjectDir:   "src",
		MemoryDir:    "memory",
		TempFolder:   "tmp",
		DocumentsDir: "Documents/squid-os",
	}
}

// LoadSettings loads settings.json or returns defaults
func LoadSettings(p Paths) Settings {
	s := DefaultSettings()
	data, err := os.ReadFile(p.SettingsFile())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	if s.MaxHistory <= 0 {
		s.MaxHistory = 500
	}
	return s
}

// SaveSettings writes settings.json
func SaveSettings(p Paths, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.SettingsFile(), data, 0644)
}

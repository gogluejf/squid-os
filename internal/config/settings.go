package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Authorization modes
const (
	AuthorizationAuto       = "auto"
	AuthorizationAskOnWrite = "ask-on-write"
	AuthorizationAskForAll  = "ask-for-all"
)

// ThinkingConfig groups thinking-related settings.
type ThinkingConfig struct {
	// Enabled requests reasoning/thinking mode from the active provider/backend.
	// How this is expressed on the wire is provider-specific.
	Enabled bool `json:"enabled"`

	// ParseReasoningFromText enables local parsing of reasoning embedded in text
	// when Thinking is on and the provider/backend can return inline reasoning
	// rather than native reasoning chunks (for example some Qwen-style setups when serving without reasoning parser directly in the template).
	ParseReasoningFromText bool `json:"parse_reasoning_from_text"`
}

type Settings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`

	// Thinking config: enables reasoning mode and optional inline parsing.
	Thinking ThinkingConfig `json:"thinking"`

	SystemPromptFile    string `json:"system_prompt_file"`
	MaxHistory          int    `json:"max_history"`
	LastSessionName     string `json:"last_session_name"`
	AutoSave            bool   `json:"auto_save"`
	AutoLoadLastSession bool   `json:"auto_load_last_session"`
	ContextWindow       int    `json:"context_window"`
	DebugEnabled        bool   `json:"debug_enabled"`
	Authorization       string `json:"authorization"` // auto | ask-on-write | ask-for-all
	// Domain directories — relative to home, resolved by Paths
	ProjectDir   string `json:"project_dir"`   // default: "src"
	MemoryDir    string `json:"memory_dir"`    // default: "memory"
	TempFolder   string `json:"temp_folder"`   // default: "tmp"
	DocumentsDir string `json:"documents_dir"` // default: "Documents/squid-os"
}

// ValidateAuthorization returns the normalized authorization mode, falling back to auto.
func (s Settings) ValidateAuthorization() string {
	switch s.Authorization {
	case AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll:
		return s.Authorization
	default:
		return AuthorizationAuto
	}
}

// LoadSettings loads settings.json from the given config directory.
func LoadSettings(cfgDir string) Settings {
	var s Settings
	data, err := os.ReadFile(filepath.Join(cfgDir, "settings.json"))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
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

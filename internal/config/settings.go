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
}

func DefaultSettings() Settings {
	return Settings{
		Provider:            "vllm",
		Model:               "",
		Thinking:            false,
		MaxHistory:          500,
		AutoSave:            false,
		AutoLoadLastSession: false,
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

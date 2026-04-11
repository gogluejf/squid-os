package config

import (
	"encoding/json"
	"os"
)

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

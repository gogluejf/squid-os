package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ProviderConfig struct {
	Name      string `json:"name"`
	ChatURL   string `json:"chat_completions_url"`
	ModelsURL string `json:"models_url"`
}

type EndpointsConfig struct {
	Providers []ProviderConfig `json:"providers"`
}

// LoadEndpoints loads endpoints.json from the given config directory.
func LoadEndpoints(cfgDir string) EndpointsConfig {
	var e EndpointsConfig
	data, err := os.ReadFile(filepath.Join(cfgDir, "endpoints.json"))
	if err != nil {
		return e
	}
	_ = json.Unmarshal(data, &e)
	return e
}

// ResolveChatURL returns the ChatURL for the active provider, falling back to
// the first provider's URL, then the vllm default.
func ResolveChatURL(endpoints EndpointsConfig, provider string) string {
	for _, p := range endpoints.Providers {
		if p.Name == provider {
			return p.ChatURL
		}
	}
	if len(endpoints.Providers) > 0 {
		return endpoints.Providers[0].ChatURL
	}
	return "https://localhost/v1/chat/completions"
}

// SaveEndpoints writes endpoints.json
func SaveEndpoints(p Paths, e EndpointsConfig) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.EndpointsFile(), data, 0644)
}

package config

import (
	"encoding/json"
	"os"
)

func DefaultEndpoints() EndpointsConfig {
	return EndpointsConfig{
		Providers: []ProviderConfig{
			{
				Name:      "vllm",
				ChatURL:   "http://localhost/v1/chat/completions",
				ModelsURL: "http://localhost/v1/models",
			},
			{
				Name:      "ollama",
				ChatURL:   "http://localhost/ollama/v1/chat/completions",
				ModelsURL: "http://localhost/ollama/v1/models",
			},
		},
	}
}

// LoadEndpoints loads endpoints.json or returns defaults
func LoadEndpoints(p Paths) EndpointsConfig {
	e := DefaultEndpoints()
	data, err := os.ReadFile(p.EndpointsFile())
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
	return "http://localhost/v1/chat/completions"
}

// SaveEndpoints writes endpoints.json
func SaveEndpoints(p Paths, e EndpointsConfig) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.EndpointsFile(), data, 0644)
}

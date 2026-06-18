package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderOllama, func(settings *config.ProviderSettings) Provider {
		return newOllamaProvider(settings)
	})
}

type OllamaProvider struct {
	settings *config.ProviderSettings
}

func newOllamaProvider(settings *config.ProviderSettings) *OllamaProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &OllamaProvider{settings: settings}
}

func (o *OllamaProvider) Name() string                     { return config.ProviderOllama }
func (o *OllamaProvider) Dialect() config.Dialect          { return config.DialectOpenAICompatible }
func (o *OllamaProvider) SupportedAuth() []config.AuthMethod { return []config.AuthMethod{config.AuthNone} }
func (o *OllamaProvider) StaticModels() []string           { return nil }
func (o *OllamaProvider) DefaultBaseURL() string           { return "http://localhost:11434" }
func (o *OllamaProvider) RequiresBaseURL() bool            { return true }

func (o *OllamaProvider) GetChatURL() string {
	base := o.settings.BaseURL
	if base == "" {
		base = o.DefaultBaseURL()
	}
	return base + "/v1/chat/completions"
}

func (o *OllamaProvider) GetModelsURL() string {
	base := o.settings.BaseURL
	if base == "" {
		base = o.DefaultBaseURL()
	}
	return base + "/v1/models"
}

func (o *OllamaProvider) PrepareRequest(req *http.Request) error { return nil }
func (o *OllamaProvider) IsExpired() bool                   { return false }
func (o *OllamaProvider) Refresh() error                    { return nil }

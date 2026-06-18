package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderOllama, func(creds *config.ProviderCreds) Provider {
		return &OllamaProvider{creds: creds}
	})
}

type OllamaProvider struct {
	creds *config.ProviderCreds
}

func (o *OllamaProvider) Name() string                          { return config.ProviderOllama }
func (o *OllamaProvider) Dialect() config.Dialect               { return config.DialectOpenAICompatible }
func (o *OllamaProvider) SupportedAuth() []config.AuthMethod    { return []config.AuthMethod{config.AuthNone} }
func (o *OllamaProvider) StaticModels() []string                { return nil }
func (o *OllamaProvider) DefaultBaseURL() string                { return "http://localhost:11434" }
func (o *OllamaProvider) GetChatURL(settings *config.ProviderSettings) string  { return settings.BaseURL + "/v1/chat/completions" }
func (o *OllamaProvider) GetModelsURL(settings *config.ProviderSettings) string { return settings.BaseURL + "/v1/models" }

func (o *OllamaProvider) PrepareRequest(req *http.Request) error { return nil }
func (o *OllamaProvider) IsExpired() bool                       { return false }
func (o *OllamaProvider) Refresh() error                        { return nil }

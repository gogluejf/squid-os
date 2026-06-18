package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderLiteLLM, func(settings *config.ProviderSettings) Provider {
		return newLiteLLMProvider(settings)
	})
}

type LiteLLMProvider struct {
	settings *config.ProviderSettings
}

func newLiteLLMProvider(settings *config.ProviderSettings) *LiteLLMProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &LiteLLMProvider{settings: settings}
}

func (l *LiteLLMProvider) Name() string                         { return config.ProviderLiteLLM }
func (l *LiteLLMProvider) Dialect() config.Dialect              { return config.DialectOpenAICompatible }
func (l *LiteLLMProvider) SupportedAuth() []config.AuthMethod   { return []config.AuthMethod{config.AuthNone, config.AuthAPIKey} }
func (l *LiteLLMProvider) StaticModels() []string               { return nil }
func (l *LiteLLMProvider) DefaultBaseURL() string               { return "http://localhost:4000" }
func (l *LiteLLMProvider) RequiresBaseURL() bool                { return true }

func (l *LiteLLMProvider) GetChatURL() string {
	base := l.settings.BaseURL
	if base == "" {
		base = l.DefaultBaseURL()
	}
	return base + "/v1/chat/completions"
}

func (l *LiteLLMProvider) GetModelsURL() string {
	base := l.settings.BaseURL
	if base == "" {
		base = l.DefaultBaseURL()
	}
	return base + "/v1/models"
}

func (l *LiteLLMProvider) PrepareRequest(req *http.Request) error {
	if l.settings.Credentials != nil && l.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && l.settings.Credentials.APIKey != "" {
		req.Header.Set("x-litellm-api-key", l.settings.Credentials.APIKey)
	}
	return nil
}
func (l *LiteLLMProvider) IsExpired() bool { return false }
func (l *LiteLLMProvider) Refresh() error  { return nil }

package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderLiteLLM, func(creds *config.ProviderCreds) Provider {
		return &LiteLLMProvider{creds: creds}
	})
}

type LiteLLMProvider struct {
	creds *config.ProviderCreds
}

func (l *LiteLLMProvider) Name() string                          { return config.ProviderLiteLLM }
func (l *LiteLLMProvider) Dialect() config.Dialect               { return config.DialectOpenAICompatible }
func (l *LiteLLMProvider) SupportedAuth() []config.AuthMethod    { return []config.AuthMethod{config.AuthNone, config.AuthAPIKey} }
func (l *LiteLLMProvider) StaticModels() []string                { return nil }
func (l *LiteLLMProvider) DefaultBaseURL() string                { return "http://localhost:4000" }
func (l *LiteLLMProvider) GetChatURL(settings *config.ProviderSettings) string  { return settings.BaseURL + "/v1/chat/completions" }
func (l *LiteLLMProvider) GetModelsURL(settings *config.ProviderSettings) string { return settings.BaseURL + "/v1/models" }

func (l *LiteLLMProvider) PrepareRequest(req *http.Request) error {
	if l.creds != nil && l.creds.ActiveAuthMethod == config.AuthAPIKey && l.creds.APIKey != "" {
		req.Header.Set("x-litellm-api-key", l.creds.APIKey)
	}
	return nil
}
func (l *LiteLLMProvider) IsExpired() bool { return false }
func (l *LiteLLMProvider) Refresh() error  { return nil }

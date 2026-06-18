package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	RegisterMeta(ProviderMeta{
		Name:           config.ProviderLiteLLM,
		DefaultBaseURL: "https://localhost:4000",
		Dialect:        config.DialectOpenAICompatible,
		SupportedAuth:  []config.AuthMethod{config.AuthNone, config.AuthAPIKey},
		New: func(creds *config.ProviderCreds) ProviderImpl {
			return &LiteLLMProvider{creds: creds}
		},
	})
}

// LiteLLMProvider handles LiteLLM proxy authentication.
// Uses "x-litellm-api-key" header for API key auth.
type LiteLLMProvider struct {
	creds *config.ProviderCreds
}

func (l *LiteLLMProvider) PrepareRequest(req *http.Request) error {
	if l.creds != nil && l.creds.ActiveAuthMethod == config.AuthAPIKey && l.creds.APIKey != "" {
		req.Header.Set("x-litellm-api-key", l.creds.APIKey)
	}
	return nil
}

func (l *LiteLLMProvider) IsExpired() bool { return false }
func (l *LiteLLMProvider) Refresh() error  { return nil }
func (l *LiteLLMProvider) GetChatURL(settings *config.ProviderSettings) string {
	return settings.BaseURL + "/v1/chat/completions"
}
func (l *LiteLLMProvider) GetModelsURL(settings *config.ProviderSettings) string {
	return settings.BaseURL + "/v1/models"
}

package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderVLLM,
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthNone, config.AuthAPIKey},
	})
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOllama,
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthNone},
	})
}

// LocalProvider is a local backend provider (vllm, ollama).
// Supports optional API key auth via Bearer token.
type LocalProvider struct {
	creds *config.ProviderCreds
}

// NewLocalProvider creates a LocalProvider from user settings.
func NewLocalProvider(creds *config.ProviderCreds) *LocalProvider {
	return &LocalProvider{creds: creds}
}

func (l *LocalProvider) PrepareRequest(req *http.Request) error {
	if l.creds != nil && l.creds.ActiveAuthMethod == config.AuthAPIKey && l.creds.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.creds.APIKey)
	}
	return nil
}

func (l *LocalProvider) IsExpired() bool { return false }
func (l *LocalProvider) Refresh() error  { return nil }

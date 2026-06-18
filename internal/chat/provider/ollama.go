package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOllama,
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthNone},
		New: func(creds *config.ProviderCreds) ProviderImpl {
			return &OllamaProvider{creds: creds}
		},
	})
}

// OllamaProvider handles Ollama backend — no auth required.
type OllamaProvider struct {
	creds *config.ProviderCreds
}

func (o *OllamaProvider) PrepareRequest(req *http.Request) error { return nil }
func (o *OllamaProvider) IsExpired() bool                       { return false }
func (o *OllamaProvider) Refresh() error                        { return nil }

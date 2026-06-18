package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderVLLM,
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthNone},
	})
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOllama,
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthNone},
	})
}

// LocalProvider is a no-op provider for local backends (vllm, ollama) that don't require auth.
type LocalProvider struct{}

func (l *LocalProvider) PrepareRequest(req *http.Request) error { return nil }
func (l *LocalProvider) GetAccessToken() string                { return "" }
func (l *LocalProvider) NeedsAuth() bool                       { return false }
func (l *LocalProvider) IsExpired() bool                       { return false }
func (l *LocalProvider) Refresh() error                        { return nil }

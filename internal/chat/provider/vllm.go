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
		New: func(creds *config.ProviderCreds) ProviderImpl {
			return &VLLMProvider{creds: creds}
		},
	})
}

// VLLMProvider handles vLLM backend authentication.
// Supports optional API key auth via Bearer token.
type VLLMProvider struct {
	creds *config.ProviderCreds
}

func (v *VLLMProvider) PrepareRequest(req *http.Request) error {
	if v.creds != nil && v.creds.ActiveAuthMethod == config.AuthAPIKey && v.creds.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.creds.APIKey)
	}
	return nil
}

func (v *VLLMProvider) IsExpired() bool { return false }
func (v *VLLMProvider) Refresh() error  { return nil }

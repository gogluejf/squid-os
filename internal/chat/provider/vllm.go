package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderVLLM, func(creds *config.ProviderCreds) Provider {
		return &VLLMProvider{creds: creds}
	})
}

type VLLMProvider struct {
	creds *config.ProviderCreds
}

func (v *VLLMProvider) Name() string                             { return config.ProviderVLLM }
func (v *VLLMProvider) Dialect() config.Dialect                  { return config.DialectOpenAICompatible }
func (v *VLLMProvider) SupportedAuth() []config.AuthMethod       { return []config.AuthMethod{config.AuthNone, config.AuthAPIKey} }
func (v *VLLMProvider) StaticModels() []string                   { return nil }
func (v *VLLMProvider) DefaultBaseURL() string                   { return "http://localhost:8000" }
func (v *VLLMProvider) GetChatURL(settings *config.ProviderSettings) string  { return settings.BaseURL + "/v1/chat/completions" }
func (v *VLLMProvider) GetModelsURL(settings *config.ProviderSettings) string { return settings.BaseURL + "/v1/models" }

func (v *VLLMProvider) PrepareRequest(req *http.Request) error {
	if v.creds != nil && v.creds.ActiveAuthMethod == config.AuthAPIKey && v.creds.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.creds.APIKey)
	}
	return nil
}
func (v *VLLMProvider) IsExpired() bool { return false }
func (v *VLLMProvider) Refresh() error  { return nil }

package provider

import (
	"net/http"

	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderVLLM, func(settings *config.ProviderSettings) Provider {
		return newVLLMProvider(settings)
	})
}

type VLLMProvider struct {
	settings *config.ProviderSettings
}

func newVLLMProvider(settings *config.ProviderSettings) *VLLMProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &VLLMProvider{settings: settings}
}

func (v *VLLMProvider) Name() string                          { return config.ProviderVLLM }
func (v *VLLMProvider) Dialect() config.Dialect               { return config.DialectOpenAICompatible }
func (v *VLLMProvider) SupportedAuth() []config.AuthMethod    { return []config.AuthMethod{config.AuthNone, config.AuthAPIKey} }
func (v *VLLMProvider) StaticModels() []string                { return nil }
func (v *VLLMProvider) DefaultBaseURL() string                { return "http://localhost:8000" }
func (v *VLLMProvider) RequiresBaseURL() bool                 { return true }

func (v *VLLMProvider) GetChatURL() string {
	base := v.settings.BaseURL
	if base == "" {
		base = v.DefaultBaseURL()
	}
	return base + "/v1/chat/completions"
}

func (v *VLLMProvider) GetModelsURL() string {
	base := v.settings.BaseURL
	if base == "" {
		base = v.DefaultBaseURL()
	}
	return base + "/v1/models"
}

func (v *VLLMProvider) PrepareRequest(req *http.Request) error {
	if v.settings.Credentials != nil && v.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && v.settings.Credentials.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.settings.Credentials.APIKey)
	}
	return nil
}
func (v *VLLMProvider) IsExpired() bool { return false }
func (v *VLLMProvider) Refresh() error  { return nil }

package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_nvidia "github.com/zendev-sh/goai/provider/nvidia"
)

func init() {
	Register(config.ProviderNVIDIA, func(settings *config.ProviderSettings) Provider {
		return newNVIDIAProvider(settings)
	})
}

type NVIDIAProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newNVIDIAProvider(settings *config.ProviderSettings) *NVIDIAProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &NVIDIAProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderNVIDIA, owner: p}
	return p
}

func (p *NVIDIAProvider) Name() string            { return config.ProviderNVIDIA }
func (p *NVIDIAProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *NVIDIAProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *NVIDIAProvider) StaticModels() []ModelEntry { return nil }
func (p *NVIDIAProvider) DefaultBaseURL() string     { return "https://integrate.api.nvidia.com/v1" }
func (p *NVIDIAProvider) RequiresBaseURL() bool      { return false }
func (p *NVIDIAProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *NVIDIAProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "meta/llama-3.1-70b-instruct"
	}
	opts := []goai_nvidia.Option{goai_nvidia.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_nvidia.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_nvidia.Chat(model, opts...), false, nil
}

func (p *NVIDIAProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderNVIDIA, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *NVIDIAProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderNVIDIA, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *NVIDIAProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

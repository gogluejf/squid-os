package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_xai "github.com/zendev-sh/goai/provider/xai"
)

func init() {
	Register(config.ProviderXAI, func(settings *config.ProviderSettings) Provider {
		return newXAIProvider(settings)
	})
}

type XAIProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newXAIProvider(settings *config.ProviderSettings) *XAIProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &XAIProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderXAI, owner: p}
	return p
}

func (p *XAIProvider) Name() string            { return config.ProviderXAI }
func (p *XAIProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *XAIProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *XAIProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "grok-4"},
		{ID: "grok-3"},
		{ID: "grok-3-mini"},
		{ID: "grok-2-vision-1212"},
	}
}
func (p *XAIProvider) DefaultBaseURL() string                                            { return "https://api.x.ai/v1" }
func (p *XAIProvider) RequiresBaseURL() bool                                             { return false }
func (p *XAIProvider) RequestProviderOptions(model string, thinking bool) map[string]any { return nil }

func (p *XAIProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "grok-4"
	}
	opts := []goai_xai.Option{goai_xai.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_xai.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_xai.Chat(model, opts...), false, nil
}

func (p *XAIProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderXAI, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *XAIProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderXAI, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *XAIProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

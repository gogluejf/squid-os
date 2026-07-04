package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_perplexity "github.com/zendev-sh/goai/provider/perplexity"
)

func init() {
	Register(config.ProviderPerplexity, func(settings *config.ProviderSettings) Provider {
		return newPerplexityProvider(settings)
	})
}

type PerplexityProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newPerplexityProvider(settings *config.ProviderSettings) *PerplexityProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &PerplexityProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderPerplexity, owner: p}
	return p
}

func (p *PerplexityProvider) Name() string            { return config.ProviderPerplexity }
func (p *PerplexityProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *PerplexityProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *PerplexityProvider) StaticModels() []ModelEntry { return nil }
func (p *PerplexityProvider) DefaultBaseURL() string     { return "https://api.perplexity.ai" }
func (p *PerplexityProvider) RequiresBaseURL() bool      { return false }
func (p *PerplexityProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *PerplexityProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "sonar-pro"
	}
	opts := []goai_perplexity.Option{goai_perplexity.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_perplexity.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_perplexity.Chat(model, opts...), false, nil
}

func (p *PerplexityProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderPerplexity, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *PerplexityProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderPerplexity, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *PerplexityProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

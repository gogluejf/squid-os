package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_cerebras "github.com/zendev-sh/goai/provider/cerebras"
)

func init() {
	Register(config.ProviderCerebras, func(settings *config.ProviderSettings) Provider {
		return newCerebrasProvider(settings)
	})
}

type CerebrasProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newCerebrasProvider(settings *config.ProviderSettings) *CerebrasProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &CerebrasProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderCerebras, owner: p}
	return p
}

func (p *CerebrasProvider) Name() string            { return config.ProviderCerebras }
func (p *CerebrasProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *CerebrasProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *CerebrasProvider) StaticModels() []ModelEntry { return nil }
func (p *CerebrasProvider) DefaultBaseURL() string     { return "https://api.cerebras.ai/v1" }
func (p *CerebrasProvider) RequiresBaseURL() bool      { return false }
func (p *CerebrasProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *CerebrasProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "llama3.1-8b"
	}
	opts := []goai_cerebras.Option{goai_cerebras.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_cerebras.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_cerebras.Chat(model, opts...), false, nil
}

func (p *CerebrasProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderCerebras, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *CerebrasProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderCerebras, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *CerebrasProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

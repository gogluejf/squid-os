package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_cohere "github.com/zendev-sh/goai/provider/cohere"
)

func init() {
	Register(config.ProviderCohere, func(settings *config.ProviderSettings) Provider {
		return newCohereProvider(settings)
	})
}

type CohereProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newCohereProvider(settings *config.ProviderSettings) *CohereProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &CohereProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderCohere, owner: p}
	return p
}

func (p *CohereProvider) Name() string            { return config.ProviderCohere }
func (p *CohereProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *CohereProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *CohereProvider) StaticModels() []ModelEntry { return nil }
func (p *CohereProvider) DefaultBaseURL() string     { return "https://api.cohere.com/v2" }
func (p *CohereProvider) RequiresBaseURL() bool      { return false }
func (p *CohereProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *CohereProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "command-a-03-2025"
	}
	opts := []goai_cohere.Option{goai_cohere.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_cohere.WithBaseURL(p.settings.BaseURL))
	}
	return goai_cohere.Chat(model, opts...), false, nil
}

func (p *CohereProvider) ListModels(ctx context.Context) ([]ModelEntry, error) { return nil, nil }

func (p *CohereProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	return &ModelEntry{ID: modelID, Provider: config.ProviderCohere}
}

func (p *CohereProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

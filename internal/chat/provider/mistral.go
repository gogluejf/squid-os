package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_mistral "github.com/zendev-sh/goai/provider/mistral"
)

func init() {
	Register(config.ProviderMistral, func(settings *config.ProviderSettings) Provider {
		return newMistralProvider(settings)
	})
}

type MistralProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newMistralProvider(settings *config.ProviderSettings) *MistralProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &MistralProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderMistral, owner: p}
	return p
}

func (p *MistralProvider) Name() string            { return config.ProviderMistral }
func (p *MistralProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *MistralProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *MistralProvider) StaticModels() []ModelEntry { return nil }
func (p *MistralProvider) DefaultBaseURL() string     { return "https://api.mistral.ai/v1" }
func (p *MistralProvider) RequiresBaseURL() bool      { return false }
func (p *MistralProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *MistralProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "mistral-large-latest"
	}
	opts := []goai_mistral.Option{goai_mistral.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_mistral.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_mistral.Chat(model, opts...), false, nil
}

func (p *MistralProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderMistral, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *MistralProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderMistral, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *MistralProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_deepseek "github.com/zendev-sh/goai/provider/deepseek"
)

func init() {
	Register(config.ProviderDeepSeek, func(settings *config.ProviderSettings) Provider {
		return newDeepSeekProvider(settings)
	})
}

type DeepSeekProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newDeepSeekProvider(settings *config.ProviderSettings) *DeepSeekProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &DeepSeekProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderDeepSeek, owner: p}
	return p
}

func (p *DeepSeekProvider) Name() string            { return config.ProviderDeepSeek }
func (p *DeepSeekProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *DeepSeekProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *DeepSeekProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "deepseek-chat", ContextLength: 64_000},
		{ID: "deepseek-reasoner", ContextLength: 64_000},
	}
}
func (p *DeepSeekProvider) DefaultBaseURL() string { return "https://api.deepseek.com" }
func (p *DeepSeekProvider) RequiresBaseURL() bool  { return false }
func (p *DeepSeekProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *DeepSeekProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "deepseek-chat"
	}
	opts := []goai_deepseek.Option{goai_deepseek.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_deepseek.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_deepseek.Chat(model, opts...), false, nil
}

func (p *DeepSeekProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderDeepSeek, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *DeepSeekProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderDeepSeek, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *DeepSeekProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

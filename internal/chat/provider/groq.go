package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_groq "github.com/zendev-sh/goai/provider/groq"
)

func init() {
	Register(config.ProviderGroq, func(settings *config.ProviderSettings) Provider {
		return newGroqProvider(settings)
	})
}

type GroqProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newGroqProvider(settings *config.ProviderSettings) *GroqProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &GroqProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderGroq, owner: p}
	return p
}

func (p *GroqProvider) Name() string            { return config.ProviderGroq }
func (p *GroqProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *GroqProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *GroqProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "llama-3.3-70b-versatile", ContextLength: 128_000},
		{ID: "llama-3.1-8b-instant", ContextLength: 128_000},
		{ID: "openai/gpt-oss-120b"},
		{ID: "openai/gpt-oss-20b"},
		{ID: "moonshotai/kimi-k2-instruct"},
	}
}
func (p *GroqProvider) DefaultBaseURL() string                                            { return "https://api.groq.com/openai/v1" }
func (p *GroqProvider) RequiresBaseURL() bool                                             { return false }
func (p *GroqProvider) RequestProviderOptions(model string, thinking bool) map[string]any { return nil }

func (p *GroqProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "llama-3.3-70b-versatile"
	}
	opts := []goai_groq.Option{goai_groq.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_groq.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_groq.Chat(model, opts...), false, nil
}

func (p *GroqProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderGroq, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *GroqProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderGroq, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *GroqProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

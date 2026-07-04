package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_requesty "github.com/zendev-sh/goai/provider/requesty"
)

func init() {
	Register(config.ProviderRequesty, func(settings *config.ProviderSettings) Provider {
		return newRequestyProvider(settings)
	})
}

type RequestyProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newRequestyProvider(settings *config.ProviderSettings) *RequestyProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &RequestyProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderRequesty, owner: p}
	return p
}

func (p *RequestyProvider) Name() string            { return config.ProviderRequesty }
func (p *RequestyProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *RequestyProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *RequestyProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "openai/gpt-4o-mini", ContextLength: 128_000},
		{ID: "openai/gpt-4o", ContextLength: 128_000},
		{ID: "anthropic/claude-sonnet-4-5"},
		{ID: "google/gemini-2.5-pro", ContextLength: 1_048_576},
		{ID: "xai/grok-4"},
	}
}
func (p *RequestyProvider) DefaultBaseURL() string { return "https://router.requesty.ai/v1" }
func (p *RequestyProvider) RequiresBaseURL() bool  { return false }
func (p *RequestyProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *RequestyProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "openai/gpt-4o-mini"
	}
	opts := []goai_requesty.Option{goai_requesty.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_requesty.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_requesty.Chat(model, opts...), false, nil
}

func (p *RequestyProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	headers := map[string]string{"HTTP-Referer": "https://github.com/getsquid/squid-os", "X-Title": "squid-os"}
	return listOpenAICompatModels(ctx, config.ProviderRequesty, baseURL, p.creds().APIKey, headers, likelyChatModel)
}

func (p *RequestyProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	headers := map[string]string{"HTTP-Referer": "https://github.com/getsquid/squid-os", "X-Title": "squid-os"}
	return openAICompatModelDetails(ctx, config.ProviderRequesty, baseURL, p.creds().APIKey, modelID, headers)
}

func (p *RequestyProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

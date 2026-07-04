package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_together "github.com/zendev-sh/goai/provider/together"
)

func init() {
	Register(config.ProviderTogether, func(settings *config.ProviderSettings) Provider {
		return newTogetherProvider(settings)
	})
}

type TogetherProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newTogetherProvider(settings *config.ProviderSettings) *TogetherProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &TogetherProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderTogether, owner: p}
	return p
}

func (p *TogetherProvider) Name() string            { return config.ProviderTogether }
func (p *TogetherProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *TogetherProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *TogetherProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", ContextLength: 131_072},
		{ID: "meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo", ContextLength: 131_072},
		{ID: "Qwen/Qwen2.5-Coder-32B-Instruct"},
		{ID: "deepseek-ai/DeepSeek-R1"},
		{ID: "mistralai/Mixtral-8x7B-Instruct-v0.1"},
	}
}
func (p *TogetherProvider) DefaultBaseURL() string { return "https://api.together.xyz/v1" }
func (p *TogetherProvider) RequiresBaseURL() bool  { return false }
func (p *TogetherProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *TogetherProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "meta-llama/Llama-3.3-70B-Instruct-Turbo"
	}
	opts := []goai_together.Option{goai_together.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_together.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_together.Chat(model, opts...), false, nil
}

func (p *TogetherProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderTogether, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *TogetherProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderTogether, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *TogetherProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

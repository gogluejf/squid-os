package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_deepinfra "github.com/zendev-sh/goai/provider/deepinfra"
)

func init() {
	Register(config.ProviderDeepInfra, func(settings *config.ProviderSettings) Provider {
		return newDeepInfraProvider(settings)
	})
}

type DeepInfraProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newDeepInfraProvider(settings *config.ProviderSettings) *DeepInfraProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &DeepInfraProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderDeepInfra, owner: p}
	return p
}

func (p *DeepInfraProvider) Name() string            { return config.ProviderDeepInfra }
func (p *DeepInfraProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *DeepInfraProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *DeepInfraProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "meta-llama/Meta-Llama-3.1-405B-Instruct", ContextLength: 32_768},
		{ID: "meta-llama/Meta-Llama-3.1-70B-Instruct", ContextLength: 131_072},
		{ID: "meta-llama/Meta-Llama-3.1-8B-Instruct", ContextLength: 131_072},
		{ID: "Qwen/Qwen2.5-Coder-32B-Instruct"},
		{ID: "deepseek-ai/DeepSeek-R1"},
	}
}
func (p *DeepInfraProvider) DefaultBaseURL() string { return "https://api.deepinfra.com/v1/openai" }
func (p *DeepInfraProvider) RequiresBaseURL() bool  { return false }
func (p *DeepInfraProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *DeepInfraProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "meta-llama/Meta-Llama-3.1-70B-Instruct"
	}
	opts := []goai_deepinfra.Option{goai_deepinfra.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_deepinfra.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_deepinfra.Chat(model, opts...), false, nil
}

func (p *DeepInfraProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderDeepInfra, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *DeepInfraProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderDeepInfra, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *DeepInfraProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

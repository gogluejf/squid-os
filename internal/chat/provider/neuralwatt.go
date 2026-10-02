package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_openai "github.com/zendev-sh/goai/provider/openai"
)

func init() {
	Register(config.ProviderNeuralWatt, func(settings *config.ProviderSettings) Provider {
		return newNeuralWattProvider(settings)
	})
}

type NeuralWattProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newNeuralWattProvider(settings *config.ProviderSettings) *NeuralWattProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &NeuralWattProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderNeuralWatt, owner: p}
	return p
}

func (p *NeuralWattProvider) Name() string            { return config.ProviderNeuralWatt }
func (p *NeuralWattProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *NeuralWattProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *NeuralWattProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "deepseek-v4-flash", ContextLength: 1_048_576},
		{ID: "deepseek-v4.1-flash", ContextLength: 262_128},
		{ID: "deepseek-v4-flash-flex", ContextLength: 1_048_576},
		{ID: "deepseek-v4-pro", ContextLength: 1_048_576},
		{ID: "gemma-4-31b", ContextLength: 262_144},
		{ID: "glm-5.2", ContextLength: 1_048_576},
		{ID: "glm-5.2-fast", ContextLength: 1_048_576},
		{ID: "glm-5.2-flex", ContextLength: 1_048_576},
		{ID: "glm-5.2-short", ContextLength: 200_000},
		{ID: "glm-5.2-short-fast", ContextLength: 200_000},
		{ID: "glm-5.2-short-fast-flex", ContextLength: 200_000},
		{ID: "glm-5.2-short-flex", ContextLength: 200_000},
		{ID: "kimi-k2.7-code", ContextLength: 262_144},
		{ID: "kimi-k2.7-code-fast", ContextLength: 262_144},
		{ID: "kimi-k2.7-code-flex", ContextLength: 262_144},
		{ID: "mimo-v2.6-pro", ContextLength: 1_048_576},
		{ID: "kimi-k3", ContextLength: 1_048_576},
		{ID: "kimi-k3-fast", ContextLength: 1_048_576},
		{ID: "kimi-k3-flex", ContextLength: 1_048_576},
		{ID: "qwen3.6-35b", ContextLength: 131_072},
		{ID: "qwen3.6-35b-fast", ContextLength: 131_072},
		{ID: "glm-5.3", ContextLength: 1_048_576},
		{ID: "glm-5.3-flash", ContextLength: 1_048_576},
		{ID: "qwen-3.8-27b", ContextLength: 262_144},
	}
}
func (p *NeuralWattProvider) DefaultBaseURL() string { return "https://api.neuralwatt.com/v1" }
func (p *NeuralWattProvider) RequiresBaseURL() bool  { return false }
// RequestProviderOptions forces the Chat Completions API and toggles Qwen
// thinking via the vLLM nested form. Verified live against api.neuralwatt.com:
// on /responses, chat_template_kwargs is dropped and even reasoning.effort
// does not suppress reasoning; on /chat/completions, the nested
// enable_thinking form works both ways (vision included) while a top-level
// enable_thinking is silently ignored.
func (p *NeuralWattProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return map[string]any{
		"useResponsesAPI": false,
		"chat_template_kwargs": map[string]any{
			"enable_thinking": thinking,
		},
	}
}

func (p *NeuralWattProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "kimi-k3"
	}
	opts := []goai_openai.Option{
		goai_openai.WithAPIKey(p.creds().APIKey),
		goai_openai.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())),
	}
	return goai_openai.Chat(model, opts...), false, nil
}

func (p *NeuralWattProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderNeuralWatt, baseURL, p.creds().APIKey, nil, nil)
}

func (p *NeuralWattProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderNeuralWatt, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *NeuralWattProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

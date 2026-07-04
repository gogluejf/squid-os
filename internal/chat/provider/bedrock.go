package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_bedrock "github.com/zendev-sh/goai/provider/bedrock"
)

func init() {
	Register(config.ProviderBedrock, func(settings *config.ProviderSettings) Provider { return newBedrockProvider(settings) })
}

type BedrockProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newBedrockProvider(settings *config.ProviderSettings) *BedrockProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &BedrockProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderBedrock, owner: p}
	return p
}
func (p *BedrockProvider) Name() string            { return config.ProviderBedrock }
func (p *BedrockProvider) Dialect() config.Dialect { return config.DialectAnthropic }
func (p *BedrockProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *BedrockProvider) StaticModels() []ModelEntry { return nil }
func (p *BedrockProvider) DefaultBaseURL() string     { return "" }
func (p *BedrockProvider) RequiresBaseURL() bool      { return false }
func (p *BedrockProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if !thinking {
		return nil
	}
	return map[string]any{"reasoningConfig": map[string]any{"type": "enabled", "budgetTokens": 1024}}
}
func (p *BedrockProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "anthropic.claude-sonnet-4-20250514-v1:0"
	}
	opts := []goai_bedrock.Option{}
	if p.creds().APIKey != "" {
		opts = append(opts, goai_bedrock.WithBearerToken(p.creds().APIKey))
	}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_bedrock.WithBaseURL(p.settings.BaseURL))
	}
	return goai_bedrock.Chat(model, opts...), false, nil
}
func (p *BedrockProvider) ListModels(ctx context.Context) ([]ModelEntry, error) { return nil, nil }
func (p *BedrockProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	return &ModelEntry{ID: modelID, Provider: config.ProviderBedrock}
}
func (p *BedrockProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

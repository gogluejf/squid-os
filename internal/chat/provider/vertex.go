package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_vertex "github.com/zendev-sh/goai/provider/vertex"
)

func init() {
	Register(config.ProviderVertex, func(settings *config.ProviderSettings) Provider { return newVertexProvider(settings) })
}

type VertexProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newVertexProvider(settings *config.ProviderSettings) *VertexProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &VertexProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderVertex, owner: p}
	return p
}
func (p *VertexProvider) Name() string            { return config.ProviderVertex }
func (p *VertexProvider) Dialect() config.Dialect { return config.DialectGemini }
func (p *VertexProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *VertexProvider) StaticModels() []ModelEntry {
	return []ModelEntry{{ID: "gemini-2.5-pro", ContextLength: 1_048_576}, {ID: "gemini-2.5-flash", ContextLength: 1_048_576}, {ID: "gemini-2.5-flash-lite", ContextLength: 1_048_576}}
}
func (p *VertexProvider) DefaultBaseURL() string { return "" }
func (p *VertexProvider) RequiresBaseURL() bool  { return false }
func (p *VertexProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}
func (p *VertexProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	opts := []goai_vertex.Option{goai_vertex.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_vertex.WithBaseURL(p.settings.BaseURL))
	}
	return goai_vertex.Chat(model, opts...), false, nil
}
func (p *VertexProvider) ListModels(ctx context.Context) ([]ModelEntry, error) { return nil, nil }
func (p *VertexProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	return &ModelEntry{ID: modelID, Provider: config.ProviderVertex}
}
func (p *VertexProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

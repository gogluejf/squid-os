package provider

import (
	"context"
	"fmt"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_azure "github.com/zendev-sh/goai/provider/azure"
)

func init() {
	Register(config.ProviderAzure, func(settings *config.ProviderSettings) Provider { return newAzureProvider(settings) })
}

type AzureProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newAzureProvider(settings *config.ProviderSettings) *AzureProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &AzureProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderAzure, owner: p}
	return p
}
func (p *AzureProvider) Name() string            { return config.ProviderAzure }
func (p *AzureProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *AzureProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *AzureProvider) StaticModels() []ModelEntry { return nil }
func (p *AzureProvider) DefaultBaseURL() string     { return "" }
func (p *AzureProvider) RequiresBaseURL() bool      { return true }
func (p *AzureProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}
func (p *AzureProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "gpt-4o-mini"
	}
	if p.settings.BaseURL == "" {
		return nil, false, fmt.Errorf("azure: endpoint base URL required")
	}
	return goai_azure.Chat(model, goai_azure.WithAPIKey(p.creds().APIKey), goai_azure.WithEndpoint(p.settings.BaseURL)), false, nil
}
func (p *AzureProvider) ListModels(ctx context.Context) ([]ModelEntry, error) { return nil, nil }
func (p *AzureProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	return &ModelEntry{ID: modelID, Provider: config.ProviderAzure}
}
func (p *AzureProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

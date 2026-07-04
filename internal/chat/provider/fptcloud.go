package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_fptcloud "github.com/zendev-sh/goai/provider/fptcloud"
)

func init() {
	Register(config.ProviderFPTCloud, func(settings *config.ProviderSettings) Provider {
		return newFPTCloudProvider(settings)
	})
}

type FPTCloudProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newFPTCloudProvider(settings *config.ProviderSettings) *FPTCloudProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &FPTCloudProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderFPTCloud, owner: p}
	return p
}

func (p *FPTCloudProvider) Name() string            { return config.ProviderFPTCloud }
func (p *FPTCloudProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *FPTCloudProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *FPTCloudProvider) StaticModels() []ModelEntry { return nil }
func (p *FPTCloudProvider) DefaultBaseURL() string     { return "https://mkp-api.fptcloud.com/v1" }
func (p *FPTCloudProvider) RequiresBaseURL() bool      { return false }
func (p *FPTCloudProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *FPTCloudProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "gpt-4o-mini"
	}
	opts := []goai_fptcloud.Option{goai_fptcloud.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_fptcloud.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
	}
	return goai_fptcloud.Chat(model, opts...), false, nil
}

func (p *FPTCloudProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return listOpenAICompatModels(ctx, config.ProviderFPTCloud, baseURL, p.creds().APIKey, nil, likelyChatModel)
}

func (p *FPTCloudProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())
	return openAICompatModelDetails(ctx, config.ProviderFPTCloud, baseURL, p.creds().APIKey, modelID, nil)
}

func (p *FPTCloudProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

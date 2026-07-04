package provider

import (
	"context"
	"fmt"
	"strings"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_cloudflare "github.com/zendev-sh/goai/provider/cloudflare"
)

func init() {
	Register(config.ProviderCloudflare, func(settings *config.ProviderSettings) Provider {
		return newCloudflareProvider(settings)
	})
}

type CloudflareProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newCloudflareProvider(settings *config.ProviderSettings) *CloudflareProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &CloudflareProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderCloudflare, owner: p}
	return p
}

func (p *CloudflareProvider) Name() string            { return config.ProviderCloudflare }
func (p *CloudflareProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *CloudflareProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *CloudflareProvider) StaticModels() []ModelEntry { return nil }
func (p *CloudflareProvider) DefaultBaseURL() string     { return "" }
func (p *CloudflareProvider) RequiresBaseURL() bool      { return true }
func (p *CloudflareProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *CloudflareProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "@cf/meta/llama-3.1-8b-instruct"
	}
	if p.settings.BaseURL == "" {
		return nil, false, fmt.Errorf("cloudflare: base URL required")
	}
	opts := []goai_cloudflare.Option{goai_cloudflare.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_cloudflare.WithBaseURL(strings.TrimRight(p.settings.BaseURL, "/")))
	}
	return goai_cloudflare.Chat(model, opts...), false, nil
}

func (p *CloudflareProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	if p.settings.BaseURL == "" {
		return nil, fmt.Errorf("cloudflare: base URL required")
	}
	return listOpenAICompatModels(ctx, config.ProviderCloudflare, strings.TrimRight(p.settings.BaseURL, "/"), p.creds().APIKey, nil, likelyChatModel)
}

func (p *CloudflareProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	if p.settings.BaseURL == "" {
		return &ModelEntry{ID: modelID, Provider: config.ProviderCloudflare}
	}
	return openAICompatModelDetails(ctx, config.ProviderCloudflare, strings.TrimRight(p.settings.BaseURL, "/"), p.creds().APIKey, modelID, nil)
}

func (p *CloudflareProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

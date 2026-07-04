package provider

import (
	"context"
	"fmt"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_runpod "github.com/zendev-sh/goai/provider/runpod"
)

func init() {
	Register(config.ProviderRunPod, func(settings *config.ProviderSettings) Provider {
		return newRunPodProvider(settings)
	})
}

type RunPodProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newRunPodProvider(settings *config.ProviderSettings) *RunPodProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &RunPodProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderRunPod, owner: p}
	return p
}

func (p *RunPodProvider) Name() string            { return config.ProviderRunPod }
func (p *RunPodProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *RunPodProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *RunPodProvider) StaticModels() []ModelEntry { return nil }
func (p *RunPodProvider) DefaultBaseURL() string     { return "" }
func (p *RunPodProvider) RequiresBaseURL() bool      { return true }
func (p *RunPodProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *RunPodProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "default"
	}
	if p.settings.BaseURL == "" {
		return nil, false, fmt.Errorf("runpod: base URL required")
	}
	opts := []goai_runpod.Option{goai_runpod.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_runpod.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.settings.BaseURL)))
	}
	return goai_runpod.Chat("custom", model, opts...), false, nil
}
func (p *RunPodProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	if p.settings.BaseURL == "" {
		return nil, fmt.Errorf("runpod: base URL required")
	}
	return listOpenAICompatModels(ctx, config.ProviderRunPod, normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.settings.BaseURL), p.creds().APIKey, nil, likelyChatModel)
}
func (p *RunPodProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	if p.settings.BaseURL == "" {
		return &ModelEntry{ID: modelID, Provider: config.ProviderRunPod}
	}
	return openAICompatModelDetails(ctx, config.ProviderRunPod, normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.settings.BaseURL), p.creds().APIKey, modelID, nil)
}
func (p *RunPodProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

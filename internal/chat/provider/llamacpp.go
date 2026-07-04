package provider

import (
	"context"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_llamacpp "github.com/zendev-sh/goai/provider/llamacpp"
)

func init() {
	Register(config.ProviderLlamaCpp, func(settings *config.ProviderSettings) Provider {
		return newLlamaCppProvider(settings)
	})
}

type LlamaCppProvider struct {
	settings *config.ProviderSettings
}

func newLlamaCppProvider(settings *config.ProviderSettings) *LlamaCppProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &LlamaCppProvider{settings: settings}
}

func (p *LlamaCppProvider) Name() string            { return config.ProviderLlamaCpp }
func (p *LlamaCppProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *LlamaCppProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthNone, config.AuthAPIKey}
}
func (p *LlamaCppProvider) StaticModels() []ModelEntry { return nil }
func (p *LlamaCppProvider) DefaultBaseURL() string     { return "http://localhost:8080" }
func (p *LlamaCppProvider) RequiresBaseURL() bool      { return false }
func (p *LlamaCppProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (p *LlamaCppProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "local"
	}
	opts := []goai_llamacpp.Option{goai_llamacpp.WithBaseURL(p.baseURL())}
	if p.creds().APIKey != "" {
		opts = append(opts, goai_llamacpp.WithAPIKey(p.creds().APIKey))
	}
	return goai_llamacpp.Chat(model, opts...), true, nil
}

func (p *LlamaCppProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	apiKey := ""
	if p.creds() != nil {
		apiKey = p.creds().APIKey
	}
	return listOpenAICompatModels(ctx, config.ProviderLlamaCpp, p.baseURL(), apiKey, nil, likelyChatModel)
}

func (p *LlamaCppProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	apiKey := ""
	if p.creds() != nil {
		apiKey = p.creds().APIKey
	}
	return openAICompatModelDetails(ctx, config.ProviderLlamaCpp, p.baseURL(), apiKey, modelID, nil)
}

func (p *LlamaCppProvider) StartDeviceAuth() (string, string, error)      { return "", "", nil }
func (p *LlamaCppProvider) PollDeviceAuth() error                         { return nil }
func (p *LlamaCppProvider) StartOAuth(redirectURI string) (string, error) { return "", nil }
func (p *LlamaCppProvider) FinishOAuth(code, redirectURI string) error    { return nil }
func (p *LlamaCppProvider) GetCredentials() *config.ProviderCreds         { return p.creds() }
func (p *LlamaCppProvider) GetDeviceAuthID() string                       { return "" }
func (p *LlamaCppProvider) SetDeviceState(id, code string)                {}

func (p *LlamaCppProvider) baseURL() string {
	if p.settings != nil && p.settings.BaseURL != "" {
		return p.settings.BaseURL
	}
	return p.DefaultBaseURL()
}

func (p *LlamaCppProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{ActiveAuthMethod: config.AuthNone}}
	}
	return p.settings.Credentials
}

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"squid-os/internal/config"
	goai_provider "github.com/zendev-sh/goai/provider"
	"github.com/zendev-sh/goai/provider/ollama"
)

func init() {
	Register(config.ProviderOllama, func(settings *config.ProviderSettings) Provider {
		return newOllamaProvider(settings)
	})
}

type OllamaProvider struct {
	settings *config.ProviderSettings
}

func newOllamaProvider(settings *config.ProviderSettings) *OllamaProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &OllamaProvider{settings: settings}
}

func (o *OllamaProvider) Name() string                     { return config.ProviderOllama }
func (o *OllamaProvider) Dialect() config.Dialect          { return config.DialectOpenAICompatible }
func (o *OllamaProvider) SupportedAuth() []config.AuthMethod { return []config.AuthMethod{config.AuthNone} }
func (o *OllamaProvider) StaticModels() []string           { return nil }
func (o *OllamaProvider) DefaultBaseURL() string           { return "http://localhost:11434" }
func (o *OllamaProvider) RequiresBaseURL() bool            { return true }
func (o *OllamaProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if thinking {
		return map[string]any{"think": true}
	}
	return nil
}

func (o *OllamaProvider) StartDeviceAuth() (string, string, error)    { return "", "", fmt.Errorf("ollama: device auth not supported") }
func (o *OllamaProvider) PollDeviceAuth() error                       { return fmt.Errorf("ollama: device auth not supported") }
func (o *OllamaProvider) StartOAuth(redirectURI string) (string, error) { return "", fmt.Errorf("ollama: OAuth not supported") }
func (o *OllamaProvider) FinishOAuth(code, redirectURI string) error    { return fmt.Errorf("ollama: OAuth not supported") }
func (o *OllamaProvider) GetCredentials() *config.ProviderCreds          { return o.creds() }
func (o *OllamaProvider) GetDeviceAuthID() string                        { return "" }
func (o *OllamaProvider) SetDeviceState(id, code string)                 {}

func (o *OllamaProvider) creds() *config.ProviderCreds {
	if o.settings == nil || o.settings.Credentials == nil {
		o.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return o.settings.Credentials
}

func (o *OllamaProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		return nil, false, fmt.Errorf("ollama: no model configured")
	}
	base := o.settings.BaseURL
	if base == "" {
		base = o.DefaultBaseURL()
	}
	return ollama.Chat(model, ollama.WithBaseURL(base)), false, nil
}

func (o *OllamaProvider) ListModels(ctx context.Context) ([]string, error) {
	base := o.settings.BaseURL
	if base == "" {
		base = o.DefaultBaseURL()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama models endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

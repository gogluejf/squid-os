package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	goai_provider "github.com/zendev-sh/goai/provider"
	"github.com/zendev-sh/goai/provider/compat"
	"squid-os/internal/config"
)

func init() {
	Register(config.ProviderLiteLLM, func(settings *config.ProviderSettings) Provider {
		return newLiteLLMProvider(settings)
	})
}

type LiteLLMProvider struct {
	settings *config.ProviderSettings
}

func newLiteLLMProvider(settings *config.ProviderSettings) *LiteLLMProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &LiteLLMProvider{settings: settings}
}

func (l *LiteLLMProvider) Name() string            { return config.ProviderLiteLLM }
func (l *LiteLLMProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (l *LiteLLMProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthNone, config.AuthAPIKey}
}
func (l *LiteLLMProvider) StaticModels() []ModelEntry { return nil }
func (l *LiteLLMProvider) DefaultBaseURL() string     { return "http://localhost:4000" }
func (l *LiteLLMProvider) RequiresBaseURL() bool      { return true }
func (l *LiteLLMProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return map[string]any{
		"chat_template_kwargs": map[string]any{
			"enable_thinking": thinking,
		},
	}
}

func (l *LiteLLMProvider) StartDeviceAuth() (string, string, error) {
	return "", "", fmt.Errorf("litellm: device auth not supported")
}
func (l *LiteLLMProvider) PollDeviceAuth() error {
	return fmt.Errorf("litellm: device auth not supported")
}
func (l *LiteLLMProvider) StartOAuth(redirectURI string) (string, error) {
	return "", fmt.Errorf("litellm: OAuth not supported")
}
func (l *LiteLLMProvider) FinishOAuth(code, redirectURI string) error {
	return fmt.Errorf("litellm: OAuth not supported")
}
func (l *LiteLLMProvider) GetCredentials() *config.ProviderCreds { return l.settings.Credentials }
func (l *LiteLLMProvider) GetDeviceAuthID() string               { return "" }
func (l *LiteLLMProvider) SetDeviceState(id, code string)        {}

func (l *LiteLLMProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		return nil, false, fmt.Errorf("litellm: no model configured")
	}
	base := normalizeOpenAICompatBaseURL(l.settings.BaseURL, l.DefaultBaseURL())
	var opts []compat.Option
	opts = append(opts, compat.WithProviderID("litellm"), compat.WithBaseURL(base))
	if l.settings.Credentials != nil && l.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && l.settings.Credentials.APIKey != "" {
		opts = append(opts, compat.WithHeaders(map[string]string{
			"x-litellm-api-key": l.settings.Credentials.APIKey,
		}))
	}
	return compat.Chat(model, opts...), false, nil
}

func (l *LiteLLMProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	base := normalizeOpenAICompatBaseURL(l.settings.BaseURL, l.DefaultBaseURL())
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if l.settings.Credentials != nil && l.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && l.settings.Credentials.APIKey != "" {
		req.Header.Set("x-litellm-api-key", l.settings.Credentials.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("litellm models endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelEntry{
			ID:            m.ID,
			ContextLength: m.MaxModelLen,
		})
	}
	return models, nil
}

func (l *LiteLLMProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	return &ModelEntry{ID: modelID, Provider: config.ProviderLiteLLM}
}

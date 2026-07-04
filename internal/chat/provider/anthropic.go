package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_anthropic "github.com/zendev-sh/goai/provider/anthropic"
)

func init() {
	Register(config.ProviderAnthropic, func(settings *config.ProviderSettings) Provider {
		return newAnthropicProvider(settings)
	})
}

type AnthropicProvider struct {
	settings *config.ProviderSettings
}

func newAnthropicProvider(settings *config.ProviderSettings) *AnthropicProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &AnthropicProvider{settings: settings}
}

// --- Provider interface ---

func (a *AnthropicProvider) Name() string                     { return config.ProviderAnthropic }
func (a *AnthropicProvider) Dialect() config.Dialect          { return config.DialectAnthropic }
func (a *AnthropicProvider) SupportedAuth() []config.AuthMethod { return []config.AuthMethod{config.AuthAPIKey} }
func (a *AnthropicProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "claude-opus-4-6", ContextLength: 200_000},
		{ID: "claude-sonnet-4-20250514", ContextLength: 200_000},
		{ID: "claude-sonnet-4-20250514:1", ContextLength: 200_000},
		{ID: "claude-sonnet-4-20250514:2", ContextLength: 200_000},
		{ID: "claude-sonnet-4-20250514:ext", ContextLength: 200_000},
		{ID: "claude-sonnet-4-6", ContextLength: 200_000},
		{ID: "claude-sonnet-5-20260421", ContextLength: 1_000_000},
		{ID: "claude-sonnet-5-20260421:1", ContextLength: 1_000_000},
		{ID: "claude-sonnet-5-20260421:2", ContextLength: 1_000_000},
		{ID: "claude-sonnet-5-20260421:ext", ContextLength: 1_000_000},
		{ID: "claude-haiku-3-5", ContextLength: 200_000},
	}
}
func (a *AnthropicProvider) DefaultBaseURL() string { return "https://api.anthropic.com" }
func (a *AnthropicProvider) RequiresBaseURL() bool  { return false }
func (a *AnthropicProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if !thinking {
		return nil
	}
	return map[string]any{"thinking": map[string]any{"type": "enabled"}}
}

func (a *AnthropicProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	opts := []goai_anthropic.Option{goai_anthropic.WithAPIKey(a.creds().APIKey)}
	if a.settings.BaseURL != "" {
		opts = append(opts, goai_anthropic.WithBaseURL(a.settings.BaseURL))
	}
	return goai_anthropic.Chat(model, opts...), false, nil
}

func (a *AnthropicProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := a.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.creds().APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic models endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID             string `json:"id"`
			MaxInputTokens *int   `json:"max_input_tokens"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		entry := ModelEntry{ID: m.ID}
		if m.MaxInputTokens != nil {
			entry.ContextLength = *m.MaxInputTokens
		}
		models = append(models, entry)
	}
	return models, nil
}

func (a *AnthropicProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := a.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models/"+modelID, nil)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderAnthropic}
	}
	req.Header.Set("x-api-key", a.creds().APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderAnthropic}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ModelEntry{ID: modelID, Provider: config.ProviderAnthropic}
	}

	var result struct {
		ID             string `json:"id"`
		MaxInputTokens *int   `json:"max_input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderAnthropic}
	}

	entry := &ModelEntry{ID: result.ID, Provider: config.ProviderAnthropic}
	if result.MaxInputTokens != nil {
		entry.ContextLength = *result.MaxInputTokens
	}
	return entry
}

// --- Auth stubs ---

func (a *AnthropicProvider) StartDeviceAuth() (string, string, error)    { return "", "", fmt.Errorf("anthropic: device auth not supported") }
func (a *AnthropicProvider) PollDeviceAuth() error                       { return fmt.Errorf("anthropic: device auth not supported") }
func (a *AnthropicProvider) StartOAuth(redirectURI string) (string, error) { return "", fmt.Errorf("anthropic: OAuth not supported") }
func (a *AnthropicProvider) FinishOAuth(code, redirectURI string) error    { return fmt.Errorf("anthropic: OAuth not supported") }
func (a *AnthropicProvider) GetCredentials() *config.ProviderCreds          { return a.creds() }
func (a *AnthropicProvider) GetDeviceAuthID() string                        { return "" }
func (a *AnthropicProvider) SetDeviceState(id, code string)                 {}

func (a *AnthropicProvider) creds() *config.ProviderCreds {
	if a.settings == nil || a.settings.Credentials == nil {
		a.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return a.settings.Credentials
}

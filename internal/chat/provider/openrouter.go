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
	goai_openrouter "github.com/zendev-sh/goai/provider/openrouter"
)

func init() {
	Register(config.ProviderOpenRouter, func(settings *config.ProviderSettings) Provider {
		return newOpenRouterProvider(settings)
	})
}

type OpenRouterProvider struct {
	settings *config.ProviderSettings
}

func newOpenRouterProvider(settings *config.ProviderSettings) *OpenRouterProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &OpenRouterProvider{settings: settings}
}

// --- Provider interface ---

func (o *OpenRouterProvider) Name() string            { return config.ProviderOpenRouter }
func (o *OpenRouterProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (o *OpenRouterProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (o *OpenRouterProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "anthropic/claude-sonnet-4-20250514", ContextLength: 200_000},
		{ID: "anthropic/claude-sonnet-5", ContextLength: 1_000_000},
		{ID: "google/gemini-2.5-pro", ContextLength: 1_048_576},
		{ID: "google/gemini-2.5-flash", ContextLength: 1_048_576},
		{ID: "google/gemini-2.5-flash-lite", ContextLength: 1_048_576},
		{ID: "openai/gpt-4o", ContextLength: 128_000},
	}
}
func (o *OpenRouterProvider) DefaultBaseURL() string { return "https://openrouter.ai/api/v1" }
func (o *OpenRouterProvider) RequiresBaseURL() bool  { return false }
func (o *OpenRouterProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if !thinking {
		return nil
	}
	return map[string]any{
		"include_reasoning": true,
		"reasoning":         map[string]any{"effort": "medium"},
	}
}

func (o *OpenRouterProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "anthropic/claude-sonnet-4-20250514"
	}
	opts := []goai_openrouter.Option{goai_openrouter.WithAPIKey(o.creds().APIKey)}
	if o.settings.BaseURL != "" {
		opts = append(opts, goai_openrouter.WithBaseURL(o.settings.BaseURL))
	}
	return goai_openrouter.Chat(model, opts...), false, nil
}

func (o *OpenRouterProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := o.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.creds().APIKey)
	req.Header.Set("HTTP-Referer", "https://github.com/getsquid/squid-os")
	req.Header.Set("X-Title", "squid-os")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter models endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		entry := ModelEntry{ID: m.ID}
		if m.ContextLength > 0 {
			entry.ContextLength = m.ContextLength
		}
		models = append(models, entry)
	}
	return models, nil
}

func (o *OpenRouterProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := o.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models/"+modelID, nil)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOpenRouter}
	}
	req.Header.Set("Authorization", "Bearer "+o.creds().APIKey)
	req.Header.Set("HTTP-Referer", "https://github.com/getsquid/squid-os")
	req.Header.Set("X-Title", "squid-os")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOpenRouter}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOpenRouter}
	}

	var result struct {
		Data struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOpenRouter}
	}

	entry := &ModelEntry{ID: result.Data.ID, Provider: config.ProviderOpenRouter}
	if result.Data.ContextLength > 0 {
		entry.ContextLength = result.Data.ContextLength
	}
	return entry
}

// --- Auth stubs ---

func (o *OpenRouterProvider) StartDeviceAuth() (string, string, error) {
	return "", "", fmt.Errorf("openrouter: device auth not supported")
}
func (o *OpenRouterProvider) PollDeviceAuth() error {
	return fmt.Errorf("openrouter: device auth not supported")
}
func (o *OpenRouterProvider) StartOAuth(redirectURI string) (string, error) {
	return "", fmt.Errorf("openrouter: OAuth not supported")
}
func (o *OpenRouterProvider) FinishOAuth(code, redirectURI string) error {
	return fmt.Errorf("openrouter: OAuth not supported")
}
func (o *OpenRouterProvider) GetCredentials() *config.ProviderCreds { return o.creds() }
func (o *OpenRouterProvider) GetDeviceAuthID() string               { return "" }
func (o *OpenRouterProvider) SetDeviceState(id, code string)        {}

func (o *OpenRouterProvider) creds() *config.ProviderCreds {
	if o.settings == nil || o.settings.Credentials == nil {
		o.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return o.settings.Credentials
}

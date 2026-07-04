package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_google "github.com/zendev-sh/goai/provider/google"
)

func init() {
	Register(config.ProviderGemini, func(settings *config.ProviderSettings) Provider {
		return newGeminiProvider(settings)
	})
}

type GeminiProvider struct {
	settings *config.ProviderSettings
}

func newGeminiProvider(settings *config.ProviderSettings) *GeminiProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &GeminiProvider{settings: settings}
}

// --- Provider interface ---

func (g *GeminiProvider) Name() string                { return config.ProviderGemini }
func (g *GeminiProvider) Dialect() config.Dialect     { return config.DialectGemini }
func (g *GeminiProvider) SupportedAuth() []config.AuthMethod { return []config.AuthMethod{config.AuthAPIKey} }
func (g *GeminiProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "gemini-2.5-pro", ContextLength: 1_048_576},
		{ID: "gemini-2.5-flash", ContextLength: 1_048_576},
		{ID: "gemini-2.5-flash-lite", ContextLength: 1_048_576},
		{ID: "gemini-2.5-flash-preview-05-211", ContextLength: 1_048_576},
		{ID: "gemini-2.5-pro-preview-05-01", ContextLength: 1_048_576},
		{ID: "gemini-2.5-pro-exp-03-25", ContextLength: 1_048_576},
		{ID: "gemini-2.5-flash-exp-02-05", ContextLength: 1_048_576},
		{ID: "gemini-2.5-flash-lite-preview-06-17", ContextLength: 1_048_576},
		{ID: "gemini-2.0-flash", ContextLength: 1_048_576},
		{ID: "gemini-2.0-flash-lite", ContextLength: 1_048_576},
		{ID: "gemini-1.5-pro", ContextLength: 2_097_152},
		{ID: "gemini-1.5-flash", ContextLength: 1_048_576},
	}
}
func (g *GeminiProvider) DefaultBaseURL() string { return "https://generativelanguage.googleapis.com" }
func (g *GeminiProvider) RequiresBaseURL() bool  { return false }
func (g *GeminiProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if !thinking {
		return nil
	}
	return map[string]any{"thinking": map[string]any{"type": "enabled"}}
}

func (g *GeminiProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	opts := []goai_google.Option{goai_google.WithAPIKey(g.creds().APIKey)}
	if g.settings.BaseURL != "" {
		opts = append(opts, goai_google.WithBaseURL(g.settings.BaseURL))
	}
	return goai_google.Chat(model, opts...), false, nil
}

func (g *GeminiProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := g.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	reqURL := baseURL + "/v1beta/models?key=" + url.QueryEscape(g.creds().APIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini models endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name              string `json:"name"`
			BaseModelID       string `json:"baseModelId"`
			InputTokenLimit   int    `json:"inputTokenLimit"`
			OutputTokenLimit  int    `json:"outputTokenLimit"`
			SupportedMethods  []string `json:"supportedMethods"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Models))
	for _, m := range result.Models {
		id := m.BaseModelID
		if id == "" {
			// Strip "models/" prefix from name if baseModelId is empty
			if len(m.Name) > 7 {
				id = m.Name[7:]
			} else {
				id = m.Name
			}
		}
		entry := ModelEntry{ID: id}
		if m.InputTokenLimit > 0 {
			entry.ContextLength = m.InputTokenLimit
		}
		models = append(models, entry)
	}
	return models, nil
}

func (g *GeminiProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := g.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	reqURL := baseURL + "/v1beta/models/" + url.PathEscape(modelID) + "?key=" + url.QueryEscape(g.creds().APIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderGemini}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderGemini}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ModelEntry{ID: modelID, Provider: config.ProviderGemini}
	}

	var result struct {
		Name            string `json:"name"`
		BaseModelID     string `json:"baseModelId"`
		InputTokenLimit int    `json:"inputTokenLimit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderGemini}
	}

	entry := &ModelEntry{ID: modelID, Provider: config.ProviderGemini}
	if result.InputTokenLimit > 0 {
		entry.ContextLength = result.InputTokenLimit
	}
	return entry
}

// --- Auth stubs ---

func (g *GeminiProvider) StartDeviceAuth() (string, string, error)    { return "", "", fmt.Errorf("gemini: device auth not supported") }
func (g *GeminiProvider) PollDeviceAuth() error                       { return fmt.Errorf("gemini: device auth not supported") }
func (g *GeminiProvider) StartOAuth(redirectURI string) (string, error) { return "", fmt.Errorf("gemini: OAuth not supported") }
func (g *GeminiProvider) FinishOAuth(code, redirectURI string) error    { return fmt.Errorf("gemini: OAuth not supported") }
func (g *GeminiProvider) GetCredentials() *config.ProviderCreds          { return g.creds() }
func (g *GeminiProvider) GetDeviceAuthID() string                        { return "" }
func (g *GeminiProvider) SetDeviceState(id, code string)                 {}

func (g *GeminiProvider) creds() *config.ProviderCreds {
	if g.settings == nil || g.settings.Credentials == nil {
		g.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return g.settings.Credentials
}

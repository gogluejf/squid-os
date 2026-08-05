package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	goai_fireworks "github.com/zendev-sh/goai/provider/fireworks"
)

func init() {
	Register(config.ProviderFireworks, func(settings *config.ProviderSettings) Provider {
		return newFireworksProvider(settings)
	})
}

type FireworksProvider struct {
	settings *config.ProviderSettings
}

func newFireworksProvider(settings *config.ProviderSettings) *FireworksProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &FireworksProvider{settings: settings}
}

// --- Provider interface ---

func (f *FireworksProvider) Name() string            { return config.ProviderFireworks }
func (f *FireworksProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (f *FireworksProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (f *FireworksProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "accounts/fireworks/models/llama-v3p1-405b-instruct", ContextLength: 131_072},
		{ID: "accounts/fireworks/models/llama-v3p1-70b-instruct", ContextLength: 131_072},
		{ID: "accounts/fireworks/models/llama-v3p1-8b-instruct", ContextLength: 131_072},
		{ID: "accounts/fireworks/models/firefunction-v2", ContextLength: 131_072},
		{ID: "accounts/fireworks/models/mixtral-8x22b-instruct-hf", ContextLength: 65_536},
		{ID: "accounts/fireworks/models/qwen2p5-coder-32b-instruct", ContextLength: 131_072},
		{ID: "accounts/fireworks/models/deepseek-v3", ContextLength: 131_072},
		{ID: "accounts/fireworks/models/llama-v3p3-70b-instruct", ContextLength: 131_072},
	}
}
func (f *FireworksProvider) DefaultBaseURL() string { return "https://api.fireworks.ai/inference/v1" }
func (f *FireworksProvider) RequiresBaseURL() bool  { return false }
func (f *FireworksProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if !thinking {
		return nil
	}
	// Fireworks reasoning models use OpenAI-compatible reasoning_effort
	return map[string]any{"reasoning_effort": "medium"}
}

func (f *FireworksProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "accounts/fireworks/models/llama-v3p1-8b-instruct"
	}
	opts := []goai_fireworks.Option{goai_fireworks.WithAPIKey(f.creds().APIKey)}
	if f.settings.BaseURL != "" {
		opts = append(opts, goai_fireworks.WithBaseURL(normalizeOpenAICompatBaseURL(f.settings.BaseURL, f.DefaultBaseURL())))
	}
	return goai_fireworks.Chat(model, opts...), false, nil
}

func (f *FireworksProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := f.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://api.fireworks.ai/inference/v1"
	}
	baseURL = normalizeOpenAICompatBaseURL(baseURL, f.DefaultBaseURL())

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.creds().APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fireworks models endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID        string `json:"id"`
			Object    string `json:"object"`
			Created   int64  `json:"created"`
			OwnedBy   string `json:"owned_by"`
			MaxTokens int    `json:"max_tokens,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		// Filter to only chat/instruct models — skip embedding, reward, and other non-chat models
		if !isChatModel(m.ID) {
			continue
		}
		entry := ModelEntry{ID: m.ID}
		if m.MaxTokens > 0 {
			entry.ContextLength = m.MaxTokens
		}
		models = append(models, entry)
	}
	return models, nil
}

func (f *FireworksProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	baseURL := f.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://api.fireworks.ai/inference/v1"
	}
	baseURL = normalizeOpenAICompatBaseURL(baseURL, f.DefaultBaseURL())

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models/"+modelID, nil)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderFireworks}
	}
	req.Header.Set("Authorization", "Bearer "+f.creds().APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderFireworks}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ModelEntry{ID: modelID, Provider: config.ProviderFireworks}
	}

	var result struct {
		ID        string `json:"id"`
		MaxTokens int    `json:"max_tokens,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderFireworks}
	}

	entry := &ModelEntry{ID: result.ID, Provider: config.ProviderFireworks}
	if result.MaxTokens > 0 {
		entry.ContextLength = result.MaxTokens
	}
	return entry
}

// isChatModel filters out non-chat models from the Fireworks catalog.
func isChatModel(id string) bool {
	lower := strings.ToLower(id)
	// Skip embedding models
	if strings.Contains(lower, "embedding") {
		return false
	}
	// Skip reward models
	if strings.Contains(lower, "reward") {
		return false
	}
	// Skip E5 models (embeddings)
	if strings.Contains(lower, "e5-") {
		return false
	}
	return true
}

// --- Auth stubs ---

func (f *FireworksProvider) StartDeviceAuth() (string, string, error) {
	return "", "", fmt.Errorf("fireworks: device auth not supported")
}
func (f *FireworksProvider) PollDeviceAuth() error {
	return fmt.Errorf("fireworks: device auth not supported")
}
func (f *FireworksProvider) StartOAuth(redirectURI string) (string, error) {
	return "", fmt.Errorf("fireworks: OAuth not supported")
}
func (f *FireworksProvider) FinishOAuth(code, redirectURI string) error {
	return fmt.Errorf("fireworks: OAuth not supported")
}
func (f *FireworksProvider) GetCredentials() *config.ProviderCreds { return f.creds() }
func (f *FireworksProvider) GetDeviceAuthID() string               { return "" }
func (f *FireworksProvider) SetDeviceState(id, code string)        {}

func (f *FireworksProvider) creds() *config.ProviderCreds {
	if f.settings == nil || f.settings.Credentials == nil {
		f.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return f.settings.Credentials
}

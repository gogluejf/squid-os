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
	goai_minimax "github.com/zendev-sh/goai/provider/minimax"
)

func init() {
	Register(config.ProviderMiniMax, func(settings *config.ProviderSettings) Provider {
		return newMiniMaxProvider(settings)
	})
}

type MiniMaxProvider struct {
	apiKeyAuthProvider
	settings *config.ProviderSettings
}

func newMiniMaxProvider(settings *config.ProviderSettings) *MiniMaxProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	p := &MiniMaxProvider{settings: settings}
	p.apiKeyAuthProvider = apiKeyAuthProvider{providerName: config.ProviderMiniMax, owner: p}
	return p
}

func (p *MiniMaxProvider) Name() string            { return config.ProviderMiniMax }
func (p *MiniMaxProvider) Dialect() config.Dialect { return config.DialectAnthropic }
func (p *MiniMaxProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthAPIKey}
}
func (p *MiniMaxProvider) StaticModels() []ModelEntry {
	return []ModelEntry{
		{ID: "MiniMax-M3", ContextLength: 1_000_000},
		{ID: "MiniMax-M2.7", ContextLength: 204_800},
		{ID: "MiniMax-M2.7-highspeed", ContextLength: 204_800},
		{ID: "MiniMax-M2.5", ContextLength: 204_800},
		{ID: "MiniMax-M2.5-highspeed", ContextLength: 204_800},
		{ID: "MiniMax-M2.1"},
		{ID: "MiniMax-M2.1-highspeed"},
		{ID: "MiniMax-M2"},
	}
}
func (p *MiniMaxProvider) DefaultBaseURL() string { return "https://api.minimax.io/anthropic" }
func (p *MiniMaxProvider) RequiresBaseURL() bool  { return false }
func (p *MiniMaxProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	if !thinking {
		return nil
	}
	return map[string]any{"thinking": map[string]any{"type": "enabled"}}
}

func (p *MiniMaxProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		model = "MiniMax-M3"
	}
	opts := []goai_minimax.Option{goai_minimax.WithAPIKey(p.creds().APIKey)}
	if p.settings.BaseURL != "" {
		opts = append(opts, goai_minimax.WithBaseURL(p.settings.BaseURL))
	}
	return goai_minimax.Chat(model, opts...), false, nil
}

func (p *MiniMaxProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	baseURL := minimaxAnthropicAPIBaseURL(p.settings.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", p.creds().APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("minimax models endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, minimaxModelEntry(m.ID))
		}
	}
	return models, nil
}

func (p *MiniMaxProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	entry := minimaxModelEntry(modelID)
	entry.Provider = config.ProviderMiniMax

	baseURL := minimaxAnthropicAPIBaseURL(p.settings.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models/"+modelID, nil)
	if err != nil {
		return &entry
	}
	req.Header.Set("X-Api-Key", p.creds().APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &entry
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &entry
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &entry
	}
	if result.ID != "" {
		entry = minimaxModelEntry(result.ID)
		entry.Provider = config.ProviderMiniMax
	}
	return &entry
}

func minimaxAnthropicAPIBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		base = "https://api.minimaxi.com/anthropic"
	}
	base = strings.TrimSuffix(base, "/v1")
	return base
}

func minimaxModelEntry(id string) ModelEntry {
	entry := ModelEntry{ID: id}
	switch id {
	case "MiniMax-M3":
		entry.ContextLength = 1_000_000
	case "MiniMax-M2.7", "MiniMax-M2.7-highspeed", "MiniMax-M2.5", "MiniMax-M2.5-highspeed":
		entry.ContextLength = 204_800
	}
	return entry
}

func (p *MiniMaxProvider) creds() *config.ProviderCreds {
	if p.settings == nil || p.settings.Credentials == nil {
		p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return p.settings.Credentials
}

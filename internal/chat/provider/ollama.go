package provider

import (
	"bytes"
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
func (o *OllamaProvider) StaticModels() []ModelEntry           { return nil }
func (o *OllamaProvider) DefaultBaseURL() string           { return "http://localhost:11434" }
func (o *OllamaProvider) RequiresBaseURL() bool            { return true }
func (o *OllamaProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	// GoAI Ollama expects the native "think" request field to always be present.
	return map[string]any{"think": thinking}
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

func (o *OllamaProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
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

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelEntry{ID: m.ID})
	}
	return models, nil
}

func (o *OllamaProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	base := o.settings.BaseURL
	if base == "" {
		base = o.DefaultBaseURL()
	}
	payload, err := json.Marshal(map[string]string{"model": modelID})
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOllama}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/show", bytes.NewReader(payload))
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOllama}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOllama}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOllama}
	}

	var result struct {
		ModelInfo map[string]interface{} `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &ModelEntry{ID: modelID, Provider: config.ProviderOllama}
	}

	entry := &ModelEntry{ID: modelID, Provider: config.ProviderOllama}

	if result.ModelInfo != nil {
		if ctxLen, ok := result.ModelInfo["llama.context_length"]; ok {
			if v, ok := ctxLen.(float64); ok {
				entry.ContextLength = int(v)
			}
		}
	}
	return entry
}

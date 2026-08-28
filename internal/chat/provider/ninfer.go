package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"squid-os/internal/config"

	goai_provider "github.com/zendev-sh/goai/provider"
	"github.com/zendev-sh/goai/provider/vllm"
)

func init() {
	Register(config.ProviderNInfer, func(settings *config.ProviderSettings) Provider {
		return newNInferProvider(settings)
	})
}

// NInferProvider connects to NInfer's OpenAI-compatible HTTP API.
type NInferProvider struct {
	settings *config.ProviderSettings
}

func newNInferProvider(settings *config.ProviderSettings) *NInferProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &NInferProvider{settings: settings}
}

func (n *NInferProvider) Name() string            { return config.ProviderNInfer }
func (n *NInferProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (n *NInferProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthNone, config.AuthAPIKey}
}
func (n *NInferProvider) StaticModels() []ModelEntry { return nil }
func (n *NInferProvider) DefaultBaseURL() string     { return "http://localhost:8080" }
func (n *NInferProvider) RequiresBaseURL() bool      { return true }

// RequestProviderOptions deliberately uses NInfer's top-level alias. Some
// NInfer versions reject vLLM's nested chat_template_kwargs form.
func (n *NInferProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return map[string]any{"enable_thinking": thinking}
}

func (n *NInferProvider) StartDeviceAuth() (string, string, error) {
	return "", "", fmt.Errorf("ninfer: device auth not supported")
}
func (n *NInferProvider) PollDeviceAuth() error {
	return fmt.Errorf("ninfer: device auth not supported")
}
func (n *NInferProvider) StartOAuth(redirectURI string) (string, error) {
	return "", fmt.Errorf("ninfer: OAuth not supported")
}
func (n *NInferProvider) FinishOAuth(code, redirectURI string) error {
	return fmt.Errorf("ninfer: OAuth not supported")
}
func (n *NInferProvider) GetCredentials() *config.ProviderCreds { return n.settings.Credentials }
func (n *NInferProvider) GetDeviceAuthID() string               { return "" }
func (n *NInferProvider) SetDeviceState(id, code string)        {}

func (n *NInferProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		return nil, false, fmt.Errorf("ninfer: no model configured")
	}
	base := normalizeOpenAICompatBaseURL(n.settings.BaseURL, n.DefaultBaseURL())
	opts := []vllm.Option{vllm.WithBaseURL(base)}
	if n.settings.Credentials != nil && n.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && n.settings.Credentials.APIKey != "" {
		opts = append(opts, vllm.WithAPIKey(n.settings.Credentials.APIKey))
	}
	return vllm.Chat(model, opts...), true, nil
}

func (n *NInferProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
	base := normalizeOpenAICompatBaseURL(n.settings.BaseURL, n.DefaultBaseURL())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if n.settings.Credentials != nil && n.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && n.settings.Credentials.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.settings.Credentials.APIKey)
	}

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ninfer models endpoint returned %d", resp.StatusCode)
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
	for _, model := range result.Data {
		models = append(models, ModelEntry{ID: model.ID, Provider: config.ProviderNInfer, ContextLength: model.MaxModelLen})
	}
	return models, nil
}

func (n *NInferProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
	return &ModelEntry{ID: modelID, Provider: config.ProviderNInfer}
}

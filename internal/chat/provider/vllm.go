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
	Register(config.ProviderVLLM, func(settings *config.ProviderSettings) Provider {
		return newVLLMProvider(settings)
	})
}

type VLLMProvider struct {
	settings *config.ProviderSettings
}

func newVLLMProvider(settings *config.ProviderSettings) *VLLMProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &VLLMProvider{settings: settings}
}

func (v *VLLMProvider) Name() string            { return config.ProviderVLLM }
func (v *VLLMProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (v *VLLMProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthNone, config.AuthAPIKey}
}
func (v *VLLMProvider) StaticModels() []string { return nil }
func (v *VLLMProvider) DefaultBaseURL() string { return "http://localhost:8000" }
func (v *VLLMProvider) RequiresBaseURL() bool  { return true }
func (v *VLLMProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	return nil
}

func (v *VLLMProvider) StartDeviceAuth() (string, string, error) {
	return "", "", fmt.Errorf("vllm: device auth not supported")
}
func (v *VLLMProvider) PollDeviceAuth() error { return fmt.Errorf("vllm: device auth not supported") }
func (v *VLLMProvider) StartOAuth(redirectURI string) (string, error) {
	return "", fmt.Errorf("vllm: OAuth not supported")
}
func (v *VLLMProvider) FinishOAuth(code, redirectURI string) error {
	return fmt.Errorf("vllm: OAuth not supported")
}
func (v *VLLMProvider) GetCredentials() *config.ProviderCreds { return v.settings.Credentials }
func (v *VLLMProvider) GetDeviceAuthID() string               { return "" }
func (v *VLLMProvider) SetDeviceState(id, code string)        {}

func (v *VLLMProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
	if model == "" {
		return nil, false, fmt.Errorf("vllm: no model configured")
	}
	base := normalizeOpenAICompatBaseURL(v.settings.BaseURL, v.DefaultBaseURL())
	var opts []vllm.Option
	opts = append(opts, vllm.WithBaseURL(base))
	if v.settings.Credentials != nil && v.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && v.settings.Credentials.APIKey != "" {
		opts = append(opts, vllm.WithAPIKey(v.settings.Credentials.APIKey))
	}
	return vllm.Chat(model, opts...), false, nil
}

func (v *VLLMProvider) ListModels(ctx context.Context) ([]string, error) {
	base := normalizeOpenAICompatBaseURL(v.settings.BaseURL, v.DefaultBaseURL())
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if v.settings.Credentials != nil && v.settings.Credentials.ActiveAuthMethod == config.AuthAPIKey && v.settings.Credentials.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.settings.Credentials.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vllm models endpoint returned %d", resp.StatusCode)
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

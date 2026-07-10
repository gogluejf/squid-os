package config

import (
	"encoding/json"
	"os"
	"time"
)

// AuthMethod defines how a provider authenticates requests.
type AuthMethod string

const (
	AuthNone   AuthMethod = "none"
	AuthAPIKey AuthMethod = "api_key"
	AuthOAuth  AuthMethod = "oauth"
)

// Dialect defines the API format a provider uses.
type Dialect string

const (
	DialectOpenAICompatible Dialect = "openai"
	DialectOpenAICodex      Dialect = "openai-codex"
	DialectAnthropic        Dialect = "anthropic"
	DialectGemini           Dialect = "gemini"
)

// Known provider name constants.
const (
	ProviderVLLM        = "vllm"
	ProviderOllama      = "ollama"
	ProviderOpenAI      = "openai"
	ProviderOpenAICodex = "openai-codex"
	ProviderLiteLLM     = "litellm"
	ProviderAnthropic   = "anthropic"
	ProviderGemini      = "gemini"
	ProviderOpenRouter  = "openrouter"
	ProviderFireworks   = "fireworks"
	ProviderXAI         = "xai"
	ProviderGroq        = "groq"
	ProviderDeepSeek    = "deepseek"
	ProviderMiniMax     = "minimax"
	ProviderTogether    = "together"
	ProviderDeepInfra   = "deepinfra"
	ProviderRequesty    = "requesty"
	ProviderCohere      = "cohere"
	ProviderMistral     = "mistral"
	ProviderPerplexity  = "perplexity"
	ProviderCerebras    = "cerebras"
	ProviderNVIDIA      = "nvidia"
	ProviderRunPod      = "runpod"
	ProviderFPTCloud    = "fptcloud"
	ProviderCloudflare  = "cloudflare"
	ProviderLlamaCpp    = "llamacpp"
	ProviderAzure       = "azure"
	ProviderBedrock     = "bedrock"
	ProviderVertex      = "vertex"
)

// ProviderSettings holds what the user configured for a provider — stored in endpoints.json.
type ProviderSettings struct {
	Name        string         `json:"name"`
	BaseURL     string         `json:"base_url,omitempty"`
	Credentials *ProviderCreds `json:"credentials,omitempty"`
}

// AuthStatus indicates the credential state for persistence purposes.
type AuthStatus string

const (
	AuthStatusOK       AuthStatus = ""           // default, no action needed
	AuthStatusFailed   AuthStatus = "failed"     // auth failed, show sentinel
	AuthStatusRefreshed AuthStatus = "refreshed" // token was refreshed, needs saving
)

// ProviderCreds holds the active credentials for a provider.
type ProviderCreds struct {
	ActiveAuthMethod AuthMethod  `json:"active_auth_method"`
	APIKey           string      `json:"api_key,omitempty"`
	OAuth            *OAuthCreds `json:"oauth,omitempty"`
	AuthStatus       AuthStatus  `json:"auth_status,omitempty"` // OK, failed, or refreshed
}

// OAuthCreds holds OAuth2 tokens for a provider.
type OAuthCreds struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id,omitempty"` // ChatGPT-Account-Id from JWT
	ExpiresAt    time.Time `json:"expires_at"`
}

// IsConfigured returns true if the user settings have valid credentials for the active auth method.
func (s ProviderSettings) IsConfigured() bool {
	if s.Credentials == nil {
		return false
	}
	switch s.Credentials.ActiveAuthMethod {
	case AuthNone:
		return true
	case AuthAPIKey:
		return s.Credentials.APIKey != ""
	case AuthOAuth:
		return s.Credentials.OAuth != nil && s.Credentials.OAuth.AccessToken != ""
	default:
		return false
	}
}

// EndpointsConfig holds the list of user-configured providers.
type EndpointsConfig struct {
	Providers []ProviderSettings `json:"providers"`
}

// LoadEndpoints loads endpoints.json from the given Paths.
// Returns only user-saved settings. May be empty.
func LoadEndpoints(p Paths) EndpointsConfig {
	data, err := os.ReadFile(p.EndpointsFile())
	if err != nil {
		return EndpointsConfig{}
	}

	var e EndpointsConfig
	if err := json.Unmarshal(data, &e); err != nil {
		return EndpointsConfig{}
	}
	return e
}

// ResolveProviderSettings finds a provider's user settings by name.
func ResolveProviderSettings(endpoints EndpointsConfig, name string) *ProviderSettings {
	for i := range endpoints.Providers {
		if endpoints.Providers[i].Name == name {
			return &endpoints.Providers[i]
		}
	}
	return nil
}

// SaveEndpoints writes endpoints.json
func SaveEndpoints(p Paths, e EndpointsConfig) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.EndpointsFile(), data, 0644)
}

// SaveProviderSettings loads the full endpoints config, updates a single provider's
// settings by name (using the live pointer), and writes back.
func SaveProviderSettings(p Paths, settings *ProviderSettings) error {
	e := LoadEndpoints(p)
	for i := range e.Providers {
		if e.Providers[i].Name == settings.Name {
			e.Providers[i] = *settings
			break
		}
	}
	return SaveEndpoints(p, e)
}

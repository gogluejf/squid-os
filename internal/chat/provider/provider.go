package provider

import (
	"context"
	"sync"

	"squid-os/internal/config"
	goai_provider "github.com/zendev-sh/goai/provider"
)

// Provider is the unified interface for a provider.
// It handles authentication, model construction, and exposes metadata.
type Provider interface {
	// Identity
	Name() string
	Dialect() config.Dialect

	// Auth flow — generic, called by provider_setup.go
	StartDeviceAuth() (string, string, error)
	PollDeviceAuth() error
	StartOAuth(redirectURI string) (string, error)
	FinishOAuth(code, redirectURI string) error
	GetCredentials() *config.ProviderCreds
	GetDeviceAuthID() string
	SetDeviceState(id, code string)

	// GoAI integration
	// BuildGoAIModel returns a GoAI LanguageModel for the given model ID.
	// The bool indicates whether the provider needs text-level think tag parsing
	// (e.g. reasoning models that embed thinking in normal text rather than as native reasoning chunks).
	BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error)

	// ListModels returns available model entries via the provider's API.
	// Each entry includes the model ID and optional context length.
	ListModels(ctx context.Context) ([]ModelEntry, error)

	// ModelDetails attempts to resolve additional metadata for a model
	// (e.g. context length) from the provider. Always returns a non-nil entry
	// with at least ID and Provider set.
	ModelDetails(ctx context.Context, modelID string) *ModelEntry

	// RequestProviderOptions returns provider-specific GoAI request options.
	// Most providers return nil. This is used for backend-specific request-shaping
	// without leaking provider conditionals into engine.go.
	RequestProviderOptions(model string, thinking bool) map[string]any

	// Configuration
	SupportedAuth() []config.AuthMethod
	StaticModels() []ModelEntry
	DefaultBaseURL() string

	// RequiresBaseURL returns true if this provider needs a user-provided
	// base URL (e.g. vllm, ollama, litellm).  Known cloud providers
	// (openai, openai-codex) return false.
	RequiresBaseURL() bool
}

// Registry of provider factories.
var (
	registry = make(map[string]Factory)
	regMu    sync.RWMutex
)

// Factory creates a Provider from user settings (single struct).
type Factory func(*config.ProviderSettings) Provider

// Register registers a provider factory by name. Called from each provider's init().
func Register(name string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[name] = f
}

// Lookup finds a provider factory by name and creates an instance.
// Pass nil settings for reading static metadata only.
func Lookup(name string, settings *config.ProviderSettings) Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	if f, ok := registry[name]; ok {
		return f(settings)
	}
	return nil
}

// All returns all registered provider names.
func All() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// IsKnown returns true if a provider with this name is registered.
func IsKnown(name string) bool {
	regMu.RLock()
	defer regMu.RUnlock()
	_, ok := registry[name]
	return ok
}

// GetByName returns a provider for reading static metadata.
func GetByName(name string) Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	if f, ok := registry[name]; ok {
		return f(nil)
	}
	return nil
}

// IsConfigured checks if settings have valid credentials for the active auth method.
func IsConfigured(s *config.ProviderSettings) bool {
	if s == nil || s.Credentials == nil {
		return false
	}
	switch s.Credentials.ActiveAuthMethod {
	case config.AuthNone:
		return true
	case config.AuthAPIKey:
		return s.Credentials.APIKey != ""
	case config.AuthOAuth:
		return s.Credentials.OAuth != nil && s.Credentials.OAuth.AccessToken != ""
	default:
		return false
	}
}

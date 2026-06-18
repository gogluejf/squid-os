package provider

import (
	"net/http"
	"sync"

	"squid-os/internal/config"
)

// Provider is the unified interface for a provider.
// It handles authentication, URL resolution, and exposes metadata.
type Provider interface {
	// Identity
	Name() string
	Dialect() config.Dialect

	// Auth
	PrepareRequest(req *http.Request) error
	IsExpired() bool
	Refresh() error

	// Endpoints — reads from the provider's own settings
	GetChatURL() string
	GetModelsURL() string

	// Configuration
	SupportedAuth() []config.AuthMethod
	StaticModels() []string
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

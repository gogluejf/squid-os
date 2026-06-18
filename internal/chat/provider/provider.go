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

	// Endpoints
	GetChatURL(settings *config.ProviderSettings) string
	GetModelsURL(settings *config.ProviderSettings) string

	// Configuration
	SupportedAuth() []config.AuthMethod
	StaticModels() []string
	DefaultBaseURL() string
}

// Registry of provider factories.
var (
	registry = make(map[string]Factory)
	regMu    sync.RWMutex
)

// Factory creates a Provider from user credentials.
type Factory func(creds *config.ProviderCreds) Provider

// Register registers a provider factory by name. Called from each provider's init().
func Register(name string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[name] = f
}

// Lookup finds a provider factory by name and creates an instance.
// Returns nil if the provider is not registered.
func Lookup(name string, creds *config.ProviderCreds) Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	if f, ok := registry[name]; ok {
		return f(creds)
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

// GetByName returns a provider by creating it with nil credentials.
// Useful for reading static metadata without user settings.
func GetByName(name string) Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	if f, ok := registry[name]; ok {
		return f(nil)
	}
	return nil
}

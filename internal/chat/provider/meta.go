package provider

import (
	"net/http"
	"sync"

	"squid-os/internal/config"
)

// ProviderImpl handles authentication preparation for a specific provider.
type ProviderImpl interface {
	PrepareRequest(req *http.Request) error
	IsExpired() bool
	Refresh() error
}

// ProviderMeta defines what a provider supports — read-only, defined in code.
type ProviderMeta struct {
	Name            string
	ChatURL         string
	ModelsURL       string
	DefaultBaseURL  string             // default base URL for wizard prompt (used when ChatURL is empty)
	Dialect         config.Dialect
	SupportedAuth   []config.AuthMethod
	New             func(*config.ProviderCreds) ProviderImpl // factory; nil = unsupported
}

var (
	metaRegistry = make(map[string]ProviderMeta)
	metaMu       sync.RWMutex
)

// RegisterMeta registers a provider's metadata and factory. Called from each provider's init().
func RegisterMeta(m ProviderMeta) {
	metaMu.Lock()
	defer metaMu.Unlock()
	metaRegistry[m.Name] = m
}

// GetMeta looks up provider metadata by name. Returns zero value if not found.
func GetMeta(name string) ProviderMeta {
	metaMu.RLock()
	defer metaMu.RUnlock()
	return metaRegistry[name]
}

// AllMeta returns all registered provider metadata.
func AllMeta() []ProviderMeta {
	metaMu.RLock()
	defer metaMu.RUnlock()
	metas := make([]ProviderMeta, 0, len(metaRegistry))
	for _, m := range metaRegistry {
		metas = append(metas, m)
	}
	return metas
}

// IsKnownProvider returns true if a provider with this name is registered.
func IsKnownProvider(name string) bool {
	metaMu.RLock()
	defer metaMu.RUnlock()
	_, ok := metaRegistry[name]
	return ok
}

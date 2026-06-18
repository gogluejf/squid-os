package provider

import (
	"sync"

	"squid-os/internal/config"
)

// ProviderMeta defines what a provider supports — read-only, defined in code.
type ProviderMeta struct {
	Name          string
	ChatURL       string           // hardcoded for known providers, empty for custom
	ModelsURL     string           // same
	Dialect       config.Dialect
	SupportedAuth []config.AuthMethod
}

var (
	metaRegistry = make(map[string]ProviderMeta)
	metaMu       sync.RWMutex
)

// RegisterMeta registers a provider's metadata. Called from each provider's init().
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

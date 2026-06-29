package chat

import (
	"context"
	"sort"
	"strings"
	"sync"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
)

// ModelEntry represents a discovered model with its provider
type ModelEntry struct {
	ID            string
	Provider      string
	ContextLength int  // 0 if unknown
	NeedsConfig   bool // true for sentinel entries (unconfigured/expired)
}

// ScanModels fetches models from all registered providers.
// Returns sentinel entries for unconfigured providers.
func ScanModels(ctx context.Context, endpoints config.EndpointsConfig) []ModelEntry {
	var (
		mu     sync.Mutex
		models []ModelEntry
		wg     sync.WaitGroup
	)

	for _, name := range provider.All() {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			// Look up user settings for this provider
			settings := config.ResolveProviderSettings(endpoints, name)
			if settings == nil {
				mu.Lock()
				models = append(models, ModelEntry{
					ID:          "<not configured>",
					Provider:    name,
					NeedsConfig: true,
				})
				mu.Unlock()
				return
			}

			s := *settings
			if s.Name == "" {
				s.Name = name
			}

			if !provider.IsConfigured(&s) {
				mu.Lock()
				models = append(models, ModelEntry{
					ID:          "<not configured>",
					Provider:    name,
					NeedsConfig: true,
				})
				mu.Unlock()
				return
			}

			p := provider.Lookup(name, &s)
			if p == nil {
				mu.Lock()
				models = append(models, ModelEntry{
					ID:          "<not supported>",
					Provider:    name,
					NeedsConfig: true,
				})
				mu.Unlock()
				return
			}

			// 1. Add static models — always available
			for _, id := range p.StaticModels() {
				mu.Lock()
				models = append(models, ModelEntry{ID: id, Provider: name})
				mu.Unlock()
			}

			// 2. Fetch models via the provider's ListModels
			modelIDs, err := p.ListModels(ctx)
			if err != nil {
				mu.Lock()
				if isAuthError(err) {
					label := "<auth failed>"
					if s.Credentials.ActiveAuthMethod == config.AuthOAuth {
						label = "<auth expired>"
					} else if s.Credentials.ActiveAuthMethod == config.AuthAPIKey {
						label = "<key invalid>"
					}
					models = append(models, ModelEntry{
						ID:          label,
						Provider:    name,
						NeedsConfig: true,
					})
				} else {
					models = append(models, ModelEntry{
						ID:          "<unreachable>",
						Provider:    name,
						NeedsConfig: true,
					})
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			existing := make(map[string]bool)
			for _, e := range models {
				if e.Provider == name {
					existing[e.ID] = true
				}
			}
			for _, id := range modelIDs {
				if !existing[id] {
					models = append(models, ModelEntry{ID: id, Provider: name})
				}
			}
			mu.Unlock()
		}(name)
	}

	wg.Wait()

	// Sort: real models first (by provider, then ID), sentinel entries at the end.
	sort.Slice(models, func(i, j int) bool {
		a, b := models[i], models[j]
		aSentinel := a.NeedsConfig
		bSentinel := b.NeedsConfig

		// Sentinels always go to the end
		if aSentinel != bSentinel {
			return !aSentinel
		}
		if aSentinel {
			// Both sentinels: sort by provider, then ID
			if a.Provider != b.Provider {
				return a.Provider < b.Provider
			}
			return a.ID < b.ID
		}
		// Both real models: sort by provider, then ID
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.ID < b.ID
	})

	return models
}

// ModelIDs extracts just the IDs from model entries
func ModelIDs(entries []ModelEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

// isAuthError returns true if the error indicates an authentication failure.
// Only 401 is a clear auth failure.  403 can mean "insufficient scopes"
// (e.g. ChatGPT subscription token hitting api.openai.com) — not a bad token.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "no credentials")
}

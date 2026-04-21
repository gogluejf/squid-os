package chat

import (
	"context"
	"sync"

	"rig-chat/internal/config"
)

// ModelEntry represents a discovered model with its provider
type ModelEntry struct {
	ID       string
	Provider string
}

// ScanModels fetches models from all providers (always fresh, no cache)
func ScanModels(ctx context.Context, endpoints config.EndpointsConfig) []ModelEntry {
	var (
		mu     sync.Mutex
		models []ModelEntry
		wg     sync.WaitGroup
	)

	for _, provider := range endpoints.Providers {
		wg.Add(1)
		go func(p config.ProviderConfig) {
			defer wg.Done()
			ids, err := FetchModels(ctx, p.ModelsURL)
			if err != nil {
				return // silently skip unavailable providers
			}
			mu.Lock()
			for _, id := range ids {
				models = append(models, ModelEntry{ID: id, Provider: p.Name})
			}
			mu.Unlock()
		}(provider)
	}

	wg.Wait()
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

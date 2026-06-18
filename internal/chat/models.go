package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

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

// codexOAuthModels is the list of models available via OpenAI Codex OAuth.
// Sourced from OpenCode's allowed model set — these are the models the Codex
// OAuth token can actually use (ChatGPT subscription, not Platform API).
var codexOAuthModels = []string{
	"gpt-5.1-codex",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex-mini",
	"gpt-5.2",
	"gpt-5.2-codex",
	"gpt-5.3-codex",
	"gpt-5.4",
	"gpt-5.4-mini",
}

// ScanModels fetches models from all registered providers.
// Returns sentinel entries for unconfigured providers.
func ScanModels(ctx context.Context, endpoints config.EndpointsConfig) []ModelEntry {
	var (
		mu     sync.Mutex
		models []ModelEntry
		wg     sync.WaitGroup
	)

	for _, meta := range provider.AllMeta() {
		wg.Add(1)
		go func(m provider.ProviderMeta) {
			defer wg.Done()

			// Look up user settings for this provider
			settings := config.ResolveProviderSettings(endpoints, m.Name)
			if settings == nil {
				mu.Lock()
				models = append(models, ModelEntry{
					ID:          "<not configured>",
					Provider:    m.Name,
					NeedsConfig: true,
				})
				mu.Unlock()
				return
			}

			s := *settings
			if s.Name == "" {
				s.Name = m.Name
			}

			if !IsConfigured(s) {
				mu.Lock()
				models = append(models, ModelEntry{
					ID:          "<not configured>",
					Provider:    m.Name,
					NeedsConfig: true,
				})
				mu.Unlock()
				return
			}

			impl := LoadProviderImpl(s)
			if impl == nil {
				mu.Lock()
				models = append(models, ModelEntry{
					ID:          "<not supported>",
					Provider:    m.Name,
					NeedsConfig: true,
				})
				mu.Unlock()
				return
			}

			modelsURL := ResolveModelsURL(s)
			if modelsURL == "" {
				return
			}

			// Codex OAuth tokens can't query the standard /v1/models endpoint
			// (missing api.model.read scope). Use the hardcoded model list instead.
			if s.Name == config.ProviderOpenAI && s.Credentials.ActiveAuthMethod == config.AuthOAuth {
				for _, id := range codexOAuthModels {
					mu.Lock()
					models = append(models, ModelEntry{ID: id, Provider: m.Name})
					mu.Unlock()
				}
				return
			}

			entries, err := fetchModelsDetail(ctx, modelsURL, impl, m.Name)
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
						Provider:    m.Name,
						NeedsConfig: true,
					})
				} else {
					models = append(models, ModelEntry{
						ID:          "<unreachable>",
						Provider:    m.Name,
						NeedsConfig: true,
					})
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			models = append(models, entries...)
			mu.Unlock()
		}(meta)
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

// fetchModelsDetail fetches models using the provider's auth implementation.
func fetchModelsDetail(ctx context.Context, modelsURL string, impl ProviderImpl, providerName string) ([]ModelEntry, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, err
	}

	if impl != nil {
		if err := impl.PrepareRequest(req); err != nil {
			return nil, err
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID            string `json:"id"`
			MaxModelLen   *int   `json:"max_model_len"`
			ContextLength *int   `json:"context_length"`
			MaxTokens     *int   `json:"max_tokens"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	entries := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		ctxLen := 0
		if m.MaxModelLen != nil {
			ctxLen = *m.MaxModelLen
		} else if m.ContextLength != nil {
			ctxLen = *m.ContextLength
		} else if m.MaxTokens != nil {
			ctxLen = *m.MaxTokens
		}
		entries = append(entries, ModelEntry{ID: m.ID, Provider: providerName, ContextLength: ctxLen})
	}
	return entries, nil
}

// isAuthError returns true if the error indicates an authentication failure.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "no credentials")
}

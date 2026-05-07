package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"squid-os/internal/config"
)

// ModelEntry represents a discovered model with its provider
type ModelEntry struct {
	ID            string
	Provider      string
	ContextLength int // 0 if unknown
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
			entries, err := FetchModelsDetail(ctx, p.ModelsURL, p.Name)
			if err != nil {
				return // silently skip unavailable providers
			}
			mu.Lock()
			models = append(models, entries...)
			mu.Unlock()
		}(provider)
	}

	wg.Wait()
	return models
}

// FetchModelsDetail queries /v1/models endpoint and returns full model entries
// with context length. It handles vLLM (max_model_len), Ollama (context_length),
// and generic backends (max_tokens).
func FetchModelsDetail(ctx context.Context, modelsURL string, provider string) ([]ModelEntry, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	// OpenAI-compatible response; different backends expose context in different fields
	var result struct {
		Data []struct {
			ID            string `json:"id"`
			MaxModelLen   *int   `json:"max_model_len"`       // vLLM
			ContextLength *int   `json:"context_length"`      // Ollama
			MaxTokens     *int   `json:"max_tokens"`          // some backends
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
		entries = append(entries, ModelEntry{ID: m.ID, Provider: provider, ContextLength: ctxLen})
	}
	return entries, nil
}

// ModelIDs extracts just the IDs from model entries
func ModelIDs(entries []ModelEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

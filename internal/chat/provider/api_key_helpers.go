package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"squid-os/internal/config"
)

type providerCredentialer interface {
	creds() *config.ProviderCreds
}

type apiKeyAuthProvider struct {
	providerName string
	owner        providerCredentialer
}

func (a apiKeyAuthProvider) StartDeviceAuth() (string, string, error) {
	return "", "", fmt.Errorf("%s: device auth not supported", a.providerName)
}

func (a apiKeyAuthProvider) PollDeviceAuth() error {
	return fmt.Errorf("%s: device auth not supported", a.providerName)
}

func (a apiKeyAuthProvider) StartOAuth(redirectURI string) (string, error) {
	return "", fmt.Errorf("%s: OAuth not supported", a.providerName)
}

func (a apiKeyAuthProvider) FinishOAuth(code, redirectURI string) error {
	return fmt.Errorf("%s: OAuth not supported", a.providerName)
}

func (a apiKeyAuthProvider) GetCredentials() *config.ProviderCreds { return a.owner.creds() }
func (a apiKeyAuthProvider) GetDeviceAuthID() string               { return "" }
func (a apiKeyAuthProvider) SetDeviceState(id, code string)        {}

func listOpenAICompatModels(ctx context.Context, providerName, baseURL, apiKey string, headers map[string]string, filter func(string) bool) ([]ModelEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s models endpoint returned %d: %s", providerName, resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length,omitempty"`
			MaxTokens     int    `json:"max_tokens,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelEntry, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID == "" {
			continue
		}
		if filter != nil && !filter(m.ID) {
			continue
		}
		entry := ModelEntry{ID: m.ID}
		switch {
		case m.ContextLength > 0:
			entry.ContextLength = m.ContextLength
		case m.MaxTokens > 0:
			entry.ContextLength = m.MaxTokens
		}
		models = append(models, entry)
	}
	return models, nil
}

func openAICompatModelDetails(ctx context.Context, providerName, baseURL, apiKey, modelID string, headers map[string]string) *ModelEntry {
	entry := &ModelEntry{ID: modelID, Provider: providerName}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/models/"+modelID, nil)
	if err != nil {
		return entry
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return entry
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return entry
	}

	var result struct {
		ID            string `json:"id"`
		ContextLength int    `json:"context_length,omitempty"`
		MaxTokens     int    `json:"max_tokens,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return entry
	}
	if result.ID != "" {
		entry.ID = result.ID
	}
	switch {
	case result.ContextLength > 0:
		entry.ContextLength = result.ContextLength
	case result.MaxTokens > 0:
		entry.ContextLength = result.MaxTokens
	}
	return entry
}

func likelyChatModel(id string) bool {
	lower := strings.ToLower(id)
	blocked := []string{"embed", "embedding", "rerank", "reward", "moderation", "audio", "whisper", "tts", "image", "stable-diffusion"}
	for _, term := range blocked {
		if strings.Contains(lower, term) {
			return false
		}
	}
	return true
}

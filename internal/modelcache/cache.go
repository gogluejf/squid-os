package modelcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
)

const FreshFor = 5 * time.Minute
const RefreshTimeout = 5 * time.Second

type Cache struct {
	UpdatedAt            time.Time             `json:"updated_at"`
	EndpointsFingerprint string                `json:"endpoints_fingerprint"`
	Models               []provider.ModelEntry `json:"models"`
}

type Store struct{ Dir string }

func (s Store) Path() string     { return filepath.Join(s.Dir, "models.json") }
func (s Store) lockPath() string { return filepath.Join(s.Dir, "models.refresh.lock") }

func Fingerprint(endpoints config.EndpointsConfig) string {
	data, _ := json.Marshal(endpoints)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s Store) Load() (Cache, error) {
	data, err := os.ReadFile(s.Path())
	if err != nil {
		return Cache{}, err
	}
	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return Cache{}, fmt.Errorf("decode model cache: %w", err)
	}
	return cache, nil
}

func (s Store) Save(endpoints config.EndpointsConfig, models []provider.ModelEntry) error {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return err
	}
	cache := Cache{UpdatedAt: time.Now().UTC(), EndpointsFingerprint: Fingerprint(endpoints), Models: realModels(models)}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "models-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0644)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.Path())
}

func (s Store) Candidates(endpoints config.EndpointsConfig, prefix string, now time.Time) ([]string, bool) {
	cache, err := s.Load()
	if err != nil || cache.EndpointsFingerprint != Fingerprint(endpoints) {
		return providerPrefixes(endpoints, prefix), false
	}
	var candidates []string
	for _, model := range cache.Models {
		value := model.Provider + "/" + model.ID
		if strings.HasPrefix(value, prefix) {
			candidates = append(candidates, value)
		}
	}
	return candidates, now.Sub(cache.UpdatedAt) < FreshFor
}

func (s Store) Refresh(ctx context.Context, endpoints config.EndpointsConfig) ([]provider.ModelEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, RefreshTimeout)
	defer cancel()
	models := provider.ScanModels(ctx, endpoints)
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err := s.Save(endpoints, models); err != nil {
		return nil, err
	}
	return models, nil
}

func (s Store) LockPath() string { return s.lockPath() }
func (s Store) TryLock() bool {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return false
	}
	file, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}
func (s Store) Unlock() { _ = os.Remove(s.lockPath()) }

func (s Store) RefreshBackground(endpoints config.EndpointsConfig) {
	if !s.TryLock() {
		return
	}
	go func() { defer s.Unlock(); _, _ = s.Refresh(context.Background(), endpoints) }()
}

func realModels(models []provider.ModelEntry) []provider.ModelEntry {
	result := make([]provider.ModelEntry, 0, len(models))
	for _, model := range models {
		if !model.NeedsConfig && model.ID != "" {
			result = append(result, model)
		}
	}
	return result
}

func providerPrefixes(endpoints config.EndpointsConfig, prefix string) []string {
	set := map[string]bool{}
	for _, endpoint := range endpoints.Providers {
		if endpoint.Name != "" {
			set[endpoint.Name+"/"] = true
		}
	}
	var result []string
	for value := range set {
		if strings.HasPrefix(value, prefix) {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

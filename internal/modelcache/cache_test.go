package modelcache

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
)

func TestCandidatesFreshStaleAndFingerprintInvalidation(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	endpoints := config.EndpointsConfig{Providers: []config.ProviderSettings{{Name: "vllm", BaseURL: "http://one"}}}
	models := []provider.ModelEntry{{Provider: "vllm", ID: "Lorbus/Qwen"}, {Provider: "openai", ID: "<not configured>", NeedsConfig: true}}
	if err := store.Save(endpoints, models); err != nil {
		t.Fatal(err)
	}

	cache, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	fresh, ok := store.Candidates(endpoints, "vllm/Lor", cache.UpdatedAt.Add(time.Minute))
	if !ok || !reflect.DeepEqual(fresh, []string{"vllm/Lorbus/Qwen"}) {
		t.Fatalf("fresh=%v ok=%v", fresh, ok)
	}

	stale, ok := store.Candidates(endpoints, "vllm/", cache.UpdatedAt.Add(FreshFor+time.Second))
	if ok || !reflect.DeepEqual(stale, []string{"vllm/Lorbus/Qwen"}) {
		t.Fatalf("stale=%v ok=%v", stale, ok)
	}

	changed := endpoints
	changed.Providers[0].BaseURL = "http://two"
	fallback, ok := store.Candidates(changed, "v", time.Now())
	if ok || !reflect.DeepEqual(fallback, []string{"vllm/"}) {
		t.Fatalf("fallback=%v ok=%v", fallback, ok)
	}
}

func TestSaveIsAtomicAndFiltersSentinels(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	endpoints := config.EndpointsConfig{}
	if err := store.Save(endpoints, []provider.ModelEntry{{Provider: "p", ID: "real"}, {Provider: "p", ID: "<unreachable>", NeedsConfig: true}}); err != nil {
		t.Fatal(err)
	}
	cache, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Models) != 1 || cache.Models[0].ID != "real" {
		t.Fatalf("%+v", cache.Models)
	}
	matches, err := filepath.Glob(filepath.Join(store.Dir, "*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files: %v %v", matches, err)
	}
}

func TestRefreshLock(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if !store.TryLock() {
		t.Fatal("first lock should succeed")
	}
	if store.TryLock() {
		t.Fatal("second lock should fail")
	}
	if _, err := os.Stat(store.LockPath()); err != nil {
		t.Fatal(err)
	}
	store.Unlock()
	if !store.TryLock() {
		t.Fatal("lock should be reusable")
	}
	store.Unlock()
}

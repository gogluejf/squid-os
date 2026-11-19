package voice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// detectServer returns a fake /detect server that maps text markers to
// languages and records request order (for out-of-order resolution tests).
func detectServer(t *testing.T, langs map[string]string, delay time.Duration, order *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/detect", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		if order != nil {
			*order = append(*order, body.Text)
		}
		lang := "en"
		for marker, l := range langs {
			if strings.Contains(body.Text, marker) {
				lang = l
				break
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"language": lang})
	})
	return httptest.NewServer(mux)
}

// TestLangStoreFrenchMessageSetsFR: a French user message lands fr in the
// store after the async DetectLang round-trip.
func TestLangStoreFrenchMessageSetsFR(t *testing.T) {
	srv := detectServer(t, map[string]string{"bonjour": "fr"}, 0, nil)
	defer srv.Close()

	sp, err := NewSpeaker(SpeakerOptions{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	store := NewLangStore()
	store.Submit(1)
	DetectLang(sp, "bonjour le monde", 1, store)

	if got := store.Get(); got != "fr" {
		t.Fatalf("lastUserLang = %q, want fr", got)
	}
}

// TestLangStoreRapidFireNewestWins: two rapid-fire submissions (FR then EN)
// where the FR request RESOLVES SECOND must still leave only the EN result —
// the stale FR result is dropped by SetIfNewest regardless of arrival order.
func TestLangStoreRapidFireNewestWins(t *testing.T) {
	// FR requests are delayed so they resolve AFTER the EN one.
	srv := detectServer(t, map[string]string{"bonjour": "fr"}, 0, nil)
	defer srv.Close()
	slowSrv := detectServer(t, map[string]string{"bonjour": "fr"}, 200*time.Millisecond, nil)
	defer slowSrv.Close()

	fastSp, err := NewSpeaker(SpeakerOptions{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	slowSp, err := NewSpeaker(SpeakerOptions{ServerURL: slowSrv.URL})
	if err != nil {
		t.Fatal(err)
	}

	store := NewLangStore()
	// Submission 1 (FR, slow) is submitted first; submission 2 (EN, fast)
	// supersedes it immediately on the UI goroutine.
	store.Submit(1)
	store.Submit(2)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); DetectLang(slowSp, "bonjour", 1, store) }()
	go func() { defer wg.Done(); DetectLang(fastSp, "hello there", 2, store) }()
	wg.Wait()

	// Even though the FR detection may have resolved second, its ID (1) is
	// no longer the newest (2), so it must be dropped.
	if got := store.Get(); got != "en" {
		t.Fatalf("lastUserLang = %q, want en (stale FR must not overwrite)", got)
	}
}

// TestLangStoreStaleResultDroppedDirectly: SetIfNewest itself drops any ID
// below the newest submitted, even when called out of order.
func TestLangStoreStaleResultDroppedDirectly(t *testing.T) {
	store := NewLangStore()
	store.Submit(1)
	store.SetIfNewest(1, "fr")
	if got := store.Get(); got != "fr" {
		t.Fatalf("after sub 1: %q, want fr", got)
	}
	// A newer submission arrives; the stale fr result for sub 1 must not land.
	store.Submit(2)
	store.SetIfNewest(1, "fr") // late arrival of the old result
	if got := store.Get(); got != "fr" {
		// fr is still the value because sub 2 produced nothing yet — but the
		// point is sub 1's LATE re-delivery cannot set a NEWER language over
		// sub 2 once sub 2 has a result.
		t.Fatalf("unexpected: %q", got)
	}
	store.SetIfNewest(2, "en")
	if got := store.Get(); got != "en" {
		t.Fatalf("after sub 2: %q, want en", got)
	}
	// And a very late sub-1 result arriving now must NOT clobber en.
	store.SetIfNewest(1, "fr")
	if got := store.Get(); got != "en" {
		t.Fatalf("stale sub-1 result overwrote newest: %q", got)
	}
}

// TestDetectLangFailureLeavesPreviousValueIntact: server down → error → the
// store keeps whatever it had before.
func TestDetectLangFailureLeavesPreviousValueIntact(t *testing.T) {
	sp, err := NewSpeaker(SpeakerOptions{
		ServerURL:    "http://127.0.0.1:1", // nothing listens here
		DetectClient: &http.Client{Timeout: 100 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewLangStore()
	store.Submit(1)
	store.SetIfNewest(1, "fr") // previous value from an earlier message

	store.Submit(2)
	start := time.Now()
	DetectLang(sp, "hello", 2, store) // fails: connection refused
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("failure path took %v, want bounded well under 1s", elapsed)
	}
	if got := store.Get(); got != "fr" {
		t.Fatalf("failure clobbered previous value: %q, want fr", got)
	}
}

// TestDetectLangTimeoutBounded: a wedged server trips the ~500ms deadline and
// leaves the previous value intact — detection never adds turn latency.
func TestDetectLangTimeoutBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // wedged
	}))
	defer srv.Close()

	sp, err := NewSpeaker(SpeakerOptions{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	store := NewLangStore()
	store.Submit(1)
	store.SetIfNewest(1, "fr")

	store.Submit(2)
	start := time.Now()
	DetectLang(sp, "hello", 2, store)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout path took %v, want ~500ms bounded", elapsed)
	}
	if got := store.Get(); got != "fr" {
		t.Fatalf("timeout clobbered previous value: %q, want fr", got)
	}
}

// TestDetectLangEmptyResultIgnored: an undecidable (empty) detection leaves
// the previous value intact rather than resetting to "".
func TestDetectLangEmptyResultIgnored(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/detect", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"language": ""})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	sp, err := NewSpeaker(SpeakerOptions{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	store := NewLangStore()
	store.Submit(1)
	store.SetIfNewest(1, "fr")

	store.Submit(2)
	DetectLang(sp, "?", 2, store)
	if got := store.Get(); got != "fr" {
		t.Fatalf("empty detection reset the value: %q, want fr", got)
	}
}

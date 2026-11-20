package voice

import (
	"context"
	"errors"
	"sync"
	"time"

	"squid-os/internal/log"
)

// detectLangDeadline bounds one user-language detection round-trip. Shorter
// than the TTS speak timeout: this runs on every user submission and must
// never be felt; a miss just keeps the previous language.
const detectLangDeadline = 500 * time.Millisecond

// LangStore holds the last detected user language for alert lead-ins.
//
// Detection is asynchronous: a submission fires DetectLang in a goroutine
// with a short deadline, and the result lands here only if its submission ID
// is still the newest — a stale (out-of-order) result never overwrites a
// newer one. Failures and timeouts leave the previous value intact.
type LangStore struct {
	mu       sync.Mutex
	lastID   int64  // newest submission ID that has been submitted
	lastLang string // last accepted detection ("en"/"fr"; "" = unknown)
}

// NewLangStore builds an empty store; the first successful detection sets
// the language until a newer submission's detection replaces it.
func NewLangStore() *LangStore { return &LangStore{} }

// Submit marks subID as the newest submission. It must be called on the UI
// goroutine at every user-message admission, BEFORE the async detect runs,
// so a later submission always supersedes an earlier one regardless of when
// the HTTP round-trips resolve.
func (s *LangStore) Submit(subID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if subID > s.lastID {
		s.lastID = subID
	}
}

// SetIfNewest stores lang for subID only when subID is still the newest
// submission. Stale results are dropped silently. Empty lang (undecidable
// or failed detection) leaves the previous value intact.
func (s *LangStore) SetIfNewest(subID int64, lang string) {
	if lang == "" {
		return
	}
	s.mu.Lock()
	accepted := subID >= s.lastID
	if accepted {
		s.lastLang = lang
	}
	newest, retained := s.lastID, s.lastLang
	s.mu.Unlock()
	log.LogVoice("detect.store", "submission=%d newest=%d accepted=%t detected=%q retained=%q", subID, newest, accepted, lang, retained)
}

// Get returns the current last-user-language. The caller applies the
// fallback chain (previous value, then "en") at use time.
func (s *LangStore) Get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastLang
}

// DetectLang POSTs text to the TTS server's /detect endpoint with a bounded
// deadline (~500 ms: detection is ~75 ms locally, so this only trips when
// the server is wedged) and stores the result via store.SetIfNewest. It runs
// in a goroutine by convention — callers fire-and-forget it at submission
// time so no turn latency is added. All failures are swallowed: detection is
// best-effort and must never surface to the user.
func DetectLang(sp *Speaker, text string, subID int64, store *LangStore) {
	log.LogVoice("detect.start", "submission=%d text=%q", subID, text)
	ctx, cancel := context.WithTimeout(context.Background(), detectLangDeadline)
	defer cancel()
	lang, err := sp.DetectLanguage(ctx, text)
	if err != nil || lang == "" {
		outcome := "empty"
		if err != nil {
			outcome = "failed"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			outcome = "timeout"
		}
		// Do not log raw HTTP errors: URLs/response bodies can contain secrets.
		log.LogVoice("detect.result", "submission=%d outcome=%s retained=%q", subID, outcome, store.Get())
		return
	}
	store.SetIfNewest(subID, lang)
}

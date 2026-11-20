package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/voice"
)

// langDetectServer returns a fake /detect server mapping text markers to
// languages, plus a speaker pointing at it.
func langDetectServer(t *testing.T, langs map[string]string) (*httptest.Server, func()) {
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
		lang := "en"
		for marker, l := range langs {
			if strings.Contains(body.Text, marker) {
				lang = l
				break
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"language": lang})
	})
	srv := httptest.NewServer(mux)
	return srv, srv.Close
}

// wireLangDetectForTest wires a model's async language detection to a fake
// /detect server with an in-memory push channel standing in for program.Send.
// Returns the push channel; tests drain it with a bounded deadline.
func wireLangDetectForTest(t *testing.T, m *Model, langs map[string]string) <-chan tea.Msg {
	t.Helper()
	srv, closeSrv := langDetectServer(t, langs)
	t.Cleanup(closeSrv)

	sp, err := voiceNewSpeakerForLang(srv.URL)
	if err != nil {
		t.Fatalf("NewSpeaker: %v", err)
	}
	cfg := config.Voice{Enabled: true, Voices: map[string]string{"en": "jane", "fr": "jane"}}
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))
	// Detection is gated on the runtime voice toggle (task 3.1 refinement):
	// these tests exercise detection itself, so voice must be ON.
	m.settings.Voice.Enabled = true
	if m.langStore == nil {
		m.langStore = voice.NewLangStore()
	}

	ch := make(chan tea.Msg, 16)
	if m.voiceWiring == nil {
		m.voiceWiring = &voiceWiringState{}
	}
	m.voiceWiring.langPush = func(msg tea.Msg) { ch <- msg }
	return ch
}

// waitLangMsg drains the push channel until a langDetectedMsg arrives or the
// bounded deadline trips (established bounded-drain pattern).
func waitLangMsg(t *testing.T, ch <-chan tea.Msg) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case msg := <-ch:
			if lm, ok := msg.(langDetectedMsg); ok {
				return lm.lang
			}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatal("no langDetectedMsg within 2s")
	return ""
}

// TestLastUserLangFrenchMessageSetsFR: a French user message submitted
// through fireUserLangDetect lands fr in the Model state via Update — the
// alert lead-in language follows the user.
func TestLastUserLangFrenchMessageSetsFR(t *testing.T) {
	m := newQueueTestModel(t)
	ch := wireLangDetectForTest(t, &m, map[string]string{"bonjour": "fr"})

	m.fireUserLangDetect("bonjour le monde")
	got := waitLangMsg(t, ch)
	if got != "fr" {
		t.Fatalf("posted lang = %q, want fr", got)
	}

	updated, _ := m.Update(langDetectedMsg{subID: 1, lang: got})
	m2 := updated.(Model)
	if m2.lastUserLang != "fr" {
		t.Fatalf("lastUserLang = %q, want fr", m2.lastUserLang)
	}
	if m2.langStore.Get() != "fr" {
		t.Fatalf("langStore = %q, want fr", m2.langStore.Get())
	}
}

// TestLastUserLangRapidFireOnlyNewestLands: rapid-fire FR then EN where the
// FR round-trip resolves SECOND — only the EN result may land in Model state.
// The store drops the stale FR result before it is ever posted.
func TestLastUserLangRapidFireOnlyNewestLands(t *testing.T) {
	m := newQueueTestModel(t)
	ch := wireLangDetectForTest(t, &m, map[string]string{"bonjour": "fr"})

	// Submission 1 (FR) is superseded by submission 2 (EN) before either
	// round-trip resolves. Both goroutines post their (post-store) value.
	m.fireUserLangDetect("bonjour")     // sub 1 → fr
	m.fireUserLangDetect("hello there") // sub 2 → en

	// Drain both messages; the LAST one applied must be en. Whatever order
	// they arrive in, the stale fr posting carries the store's value AT ITS
	// COMPLETION — if FR completed second after EN already set en, the store
	// still holds en (stale SetIfNewest dropped), so even the late posting
	// carries en.
	last := ""
	deadline := time.Now().Add(2 * time.Second)
	drained := 0
	for drained < 2 && time.Now().Before(deadline) {
		select {
		case msg := <-ch:
			if lm, ok := msg.(langDetectedMsg); ok {
				last = lm.lang
				drained++
			}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if drained < 2 {
		t.Fatalf("drained %d of 2 lang results", drained)
	}
	if last != "en" {
		t.Fatalf("final posted lang = %q, want en (stale FR must not overwrite)", last)
	}

	// Apply the final result through Update as the UI loop would.
	updated, _ := m.Update(langDetectedMsg{subID: 2, lang: last})
	m2 := updated.(Model)
	if m2.lastUserLang != "en" {
		t.Fatalf("lastUserLang = %q, want en", m2.lastUserLang)
	}
}

// TestLastUserLangFailureLeavesPreviousValueIntact: when the detect server is
// down, no result is stored and the previous lastUserLang survives — the
// posted value is still the old one, and Update keeps it.
func TestLastUserLangFailureLeavesPreviousValueIntact(t *testing.T) {
	m := newQueueTestModel(t)
	m.langStore = voice.NewLangStore()

	// Seed a previous detection as if an earlier message resolved to fr.
	m.langStore.Submit(1)
	m.langStore.SetIfNewest(1, "fr")
	m.lastUserLang = "fr"

	// Wire detection against a dead server: every DetectLang fails fast.
	sp, err := voiceNewSpeakerForLang("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Voice{Enabled: true, Voices: map[string]string{"en": "jane", "fr": "jane"}}
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))
	m.settings.Voice.Enabled = true // detection is gated on the runtime toggle
	ch := make(chan tea.Msg, 4)
	if m.voiceWiring == nil {
		m.voiceWiring = &voiceWiringState{}
	}
	m.voiceWiring.langPush = func(msg tea.Msg) { ch <- msg }

	m.fireUserLangDetect("hello") // sub 2: fails (server down)
	got := waitLangMsg(t, ch)
	if got != "fr" {
		t.Fatalf("failure path posted %q, want previous value fr intact", got)
	}
	// Apply through Update exactly as the UI loop would; the field must keep
	// the previous value (the failure stored nothing).
	updated, _ := m.Update(langDetectedMsg{subID: 1, lang: got})
	m2 := updated.(Model)
	if m2.lastUserLang != "fr" {
		t.Fatalf("lastUserLang = %q after failure, want fr", m2.lastUserLang)
	}
}

// TestLastUserLangNoVoiceIsNoOp: without a coordinator (voice unavailable),
// fireUserLangDetect is a silent no-op — no panic, no posting.
func TestLastUserLangNoVoiceIsNoOp(t *testing.T) {
	m := newQueueTestModel(t)
	m.langStore = voice.NewLangStore()
	// narrator has nil coord (default from newNarrator(nil)) → no-op.
	m.fireUserLangDetect("bonjour")
	if m.langSeq != 0 {
		t.Fatalf("submission ID bumped without voice: %d", m.langSeq)
	}
}

// voiceNewSpeakerForLang builds a Speaker pointed at the given URL with a
// short detect timeout (test seam; keeps failures fast).
func voiceNewSpeakerForLang(serverURL string) (*voice.Speaker, error) {
	return voice.NewSpeaker(voice.SpeakerOptions{
		ServerURL:    serverURL,
		DetectClient: &http.Client{Timeout: 300 * time.Millisecond},
	})
}

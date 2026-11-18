package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/voice"
)

// TestVoiceToggleFlipsRuntimeState: alt+v and /voice flip the same runtime
// flag; settings are never written (startup source of truth only).
func TestVoiceToggleFlipsRuntimeState(t *testing.T) {
	m := newVoiceTestModel(t, false)
	if m.voiceOn {
		t.Fatal("expected voice off initially")
	}

	// Bare toggle flips on.
	m2, _ := m.toggleVoice()
	if !m2.voiceOn {
		t.Fatal("bare toggle did not turn voice on")
	}

	// Bare toggle flips back off.
	m3, _ := m2.toggleVoice()
	if m3.voiceOn {
		t.Fatal("second bare toggle did not turn voice off")
	}

	// And on again.
	m4, _ := m3.toggleVoice()
	if !m4.voiceOn {
		t.Fatal("third bare toggle did not turn voice on")
	}
}

// TestVoiceToggleOffKillsPlayback: toggling off mid-speech cancels the active
// group immediately — the coordinator has no current group afterwards.
func TestVoiceToggleOffKillsPlayback(t *testing.T) {
	m := newVoiceTestModel(t, true)
	// Detect server that answers instantly so Speak succeeds and a real
	// player process is in flight (fake TTS CLI reads stdin to EOF).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"language":"en"}`)
	}))
	defer srv.Close()

	sp, err := voice.NewSpeaker(voice.SpeakerOptions{
		CLIPath:      writeFakeTTSCli(t),
		ServerURL:    srv.URL,
		DetectClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatalf("NewSpeaker: %v", err)
	}
	cfg := config.Voice{Enabled: true, Voices: map[string]string{"en": "jane"}}
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))

	m.narrator.beginMessage(true)
	// Drive the narrator's own group through feedDelta so a real chunk is in
	// flight (the fake TTS CLI holds stdin open until killed). Two sentences
	// are needed: the first (3 words) is held under minWords and merged with
	// the second before emission.
	m.narrator.feedDelta("speaking now. ")
	m.narrator.feedDelta("This is the rest of the reply. ")
	gid := m.narrator.coord.Current()
	if gid == 0 {
		t.Fatal("chunk did not reach the coordinator")
	}

	m2, _ := m.toggleVoice()
	if m2.voiceOn {
		t.Fatal("voice still on after toggle off")
	}
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("active group %d survived toggle off", got)
	}
	// Late chunks for the cancelled group must be dropped.
	m2.narrator.emit(gid, "late chunk must be dropped.")
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("late chunk resurrected group %d", got)
	}
}

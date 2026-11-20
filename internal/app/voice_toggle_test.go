package app

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/voice"
)

// TestVoiceToggleFlipsRuntimeState: alt+v and /voice flip the same runtime
// setting, persisted for the next launch.
func TestVoiceToggleFlipsRuntimeState(t *testing.T) {
	m := newVoiceTestModel(t, false)
	if m.settings.Voice.Enabled {
		t.Fatal("expected voice off initially")
	}

	// Bare toggle flips on.
	m2, _ := m.toggleVoice()
	if !m2.settings.Voice.Enabled {
		t.Fatal("bare toggle did not turn voice on")
	}

	// Bare toggle flips back off.
	m3, _ := m2.toggleVoice()
	if m3.settings.Voice.Enabled {
		t.Fatal("second bare toggle did not turn voice off")
	}

	// And on again.
	m4, _ := m3.toggleVoice()
	if !m4.settings.Voice.Enabled {
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
	if m2.settings.Voice.Enabled {
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

func TestVoiceToggleCommandsPersistWithoutDisturbingStream(t *testing.T) {
	for _, viaKey := range []bool{false, true} {
		t.Run(fmt.Sprint(viaKey), func(t *testing.T) {
			m := newVoiceTestModel(t, false)
			m.settings.Model = "preserve-model"
			m.settings.NormalizeVoice()
			m.mode = ModeStreaming
			m.turnActive = true
			m.session.Stream.Active = true
			m.session.UIStream.ID = "active-stream"
			m.textarea.SetValue("draft")
			for _, want := range []bool{true, false} {
				var updated tea.Model
				var cmd tea.Cmd
				if viaKey {
					updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
				} else {
					updated, cmd = m.executeCommandByName("voice")
				}
				m = updated.(Model)
				if cmd != nil {
					t.Fatal("toggle scheduled stream work")
				}
				saved := config.LoadSettings(m.paths.Root)
				if saved.Voice.Enabled != want || m.settings.Voice.Enabled != want {
					t.Fatal("enabled state not persisted")
				}
				if saved.Model != "preserve-model" || saved.Voice.Voices["en"] != "jane" {
					t.Fatal("unrelated settings lost")
				}
				if m.mode != ModeStreaming || !m.turnActive || !m.session.Stream.Active || m.session.UIStream.ID != "active-stream" || m.textarea.Value() != "draft" {
					t.Fatal("toggle disturbed stream")
				}
			}
		})
	}
}

func TestVoiceToggleSaveFailureKeepsState(t *testing.T) {
	m := newVoiceTestModel(t, false)
	// A directory at the file path gives a deterministic failure, even as root.
	if err := os.Mkdir(m.paths.SettingsFile(), 0700); err != nil {
		t.Fatal(err)
	}
	m, _ = m.toggleVoice()
	if m.settings.Voice.Enabled {
		t.Fatal("failed save changed enabled state")
	}
	if m.notification.Message == "voice on" {
		t.Fatal("save failure reported success")
	}
}

func TestVoiceToggleOffCancelsBufferedNarration(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrator.beginMessage(true)
	gid := m.narrator.groupID
	if gid == 0 || m.narrator.coord.Current() != 0 {
		t.Fatal("expected allocated, not yet playing group")
	}
	m, _ = m.toggleVoice()
	if m.narrator.groupID != 0 || m.narrator.chunker != nil {
		t.Fatal("buffer survived mute")
	}
	m.narrator.emit(gid, "late chunk must not start playback")
	if m.narrator.coord.Current() != 0 {
		t.Fatal("late chunk resurrected muted speech")
	}
}

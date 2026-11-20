package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadSettingsMissingVoiceSectionUsesDefaults(t *testing.T) {
	s := LoadSettings(writeSettings(t, `{"provider":"openai","model":"gpt-5.5"}`))

	if s.Voice.VoiceEnabled() {
		t.Fatal("voice should be enabled by default (mode=step)")
	}
	if got, want := s.Voice.Voices["en"], "jane"; got != want {
		t.Fatalf("voices.en = %q, want %q", got, want)
	}
	if got, want := s.Voice.Voices["fr"], "jane"; got != want {
		t.Fatalf("voices.fr = %q, want %q", got, want)
	}
}

func TestLoadSettingsNoSettingsFileUsesVoiceDefaults(t *testing.T) {
	s := LoadSettings(t.TempDir())

	if s.Voice.VoiceEnabled() {
		t.Fatal("voice should be enabled by default (mode=step)")
	}
	if got, want := s.Voice.Voices["en"], "jane"; got != want {
		t.Fatalf("voices.en = %q, want %q", got, want)
	}
	if got, want := s.Voice.Voices["fr"], "jane"; got != want {
		t.Fatalf("voices.fr = %q, want %q", got, want)
	}
}

func TestLoadSettingsPartialVoiceSectionFillsMissingLanguage(t *testing.T) {
	s := LoadSettings(writeSettings(t, `{
		"voice": {
			"mode": "step",
			"voices": {"en": "tom"}
		}
	}`))

	if !s.Voice.VoiceEnabled() {
		t.Fatal("voice should be disabled (mode=off)")
	}
	if got, want := s.Voice.Voices["en"], "tom"; got != want {
		t.Fatalf("voices.en = %q, want %q", got, want)
	}
	if got, want := s.Voice.Voices["fr"], "jane"; got != want {
		t.Fatalf("voices.fr = %q, want %q", got, want)
	}
}

func TestLoadSettingsEmptyVoicesMapFillsDefaults(t *testing.T) {
	s := LoadSettings(writeSettings(t, `{"voice": {"mode": "step", "voices": {}}}`))

	if !s.Voice.VoiceEnabled() {
		t.Fatal("voice should be disabled (mode=off)")
	}
	if got, want := s.Voice.Voices["en"], "jane"; got != want {
		t.Fatalf("voices.en = %q, want %q", got, want)
	}
	if got, want := s.Voice.Voices["fr"], "jane"; got != want {
		t.Fatalf("voices.fr = %q, want %q", got, want)
	}
}

func TestLoadSettingsInvalidVoiceNamePassesThrough(t *testing.T) {
	s := LoadSettings(writeSettings(t, `{
		"voice": {
			"mode": "step",
			"voices": {"en": "not-a-real-voice", "fr": "also-bogus"}
		}
	}`))

	if got, want := s.Voice.Voices["en"], "not-a-real-voice"; got != want {
		t.Fatalf("voices.en = %q, want %q", got, want)
	}
	if got, want := s.Voice.Voices["fr"], "also-bogus"; got != want {
		t.Fatalf("voices.fr = %q, want %q", got, want)
	}
}

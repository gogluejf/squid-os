package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHistorySupportsLegacyStringEntries(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{Root: dir}
	path := filepath.Join(dir, "history.json")
	if err := os.WriteFile(path, []byte(`{"entries":["git status","go test"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	h := LoadHistory(paths)

	if got, want := len(h.Entries), 2; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if got, want := h.Entries[0].Text, "git status"; got != want {
		t.Fatalf("entry[0].Text = %q, want %q", got, want)
	}
	if h.Entries[0].CreatedAt.Location().String() != "UTC" {
		t.Fatalf("entry[0].CreatedAt location = %q, want UTC", h.Entries[0].CreatedAt.Location())
	}
}

func TestAddHistoryEntryAddsTimestampedEntry(t *testing.T) {
	var h History

	AddHistoryEntry(&h, "git status", 100)

	if got, want := len(h.Entries), 1; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if got, want := h.Entries[0].Text, "git status"; got != want {
		t.Fatalf("entry text = %q, want %q", got, want)
	}
	if h.Entries[0].CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero")
	}
	if h.Entries[0].CreatedAt.Location().String() != "UTC" {
		t.Fatalf("CreatedAt location = %q, want UTC", h.Entries[0].CreatedAt.Location())
	}
}

func TestAddHistoryEntrySkipsDuplicateMostRecentText(t *testing.T) {
	var h History

	AddHistoryEntry(&h, "git status", 100)
	AddHistoryEntry(&h, "git status", 100)

	if got, want := len(h.Entries), 1; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
}

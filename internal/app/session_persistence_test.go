package app

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

func testRootModel(t *testing.T, name string, autosave bool) Model {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{Root: root, Sessions: filepath.Join(root, "sessions")}
	cfg := config.SessionConfig{Autosave: config.SessionAutosave{Enabled: autosave, Name: name}}
	return Model{
		paths:   paths,
		session: &UISession{Session: chat.NewRootSession(cfg, paths, runtimeconfig.Catalog{})},
	}
}

func TestNewRootSessionAlwaysHasCanonicalDirectory(t *testing.T) {
	m := testRootModel(t, "", false)
	if m.session.SessionDir == "" {
		t.Fatal("fresh root SessionDir is empty")
	}
	if filepath.Dir(m.session.SessionDir) != m.paths.Sessions {
		t.Fatalf("SessionDir %q is not below sessions root %q", m.session.SessionDir, m.paths.Sessions)
	}
	if m.session.Info.Name != "" {
		t.Fatalf("fresh unsaved session displays name %q", m.session.Info.Name)
	}
	if _, err := os.Stat(config.SessionFilePath(m.session.SessionDir)); !os.IsNotExist(err) {
		t.Fatalf("fresh session should not be persisted yet: %v", err)
	}
}

func TestAutoSaveWritesCurrentDirectoryInPlace(t *testing.T) {
	m := testRootModel(t, "autosaved", true)
	originalDir := m.session.SessionDir
	originalID := m.session.Doc.Identity.ID
	m.session.Append(chat.NewUserMessage("msg_1", "hello", ""))

	m, _ = m.autoSave()

	if m.session.SessionDir != originalDir {
		t.Fatalf("autosave changed SessionDir from %q to %q", originalDir, m.session.SessionDir)
	}
	if m.session.Doc.Identity.ID != originalID {
		t.Fatal("autosave forked the session identity")
	}
	if _, err := os.Stat(config.SessionFilePath(originalDir)); err != nil {
		t.Fatalf("autosave did not write current session: %v", err)
	}
	if m.session.Info.Name != "autosaved" {
		t.Fatalf("saved session display name = %q", m.session.Info.Name)
	}
	if m.settings.LastSessionName != "autosaved" {
		t.Fatalf("LastSessionName = %q, want autosaved", m.settings.LastSessionName)
	}
}

func TestSaveToDifferentUnpersistedDirectoryIsFirstSave(t *testing.T) {
	m := testRootModel(t, "generated", false)
	oldID := m.session.Doc.Identity.ID
	destination := config.RootSessionDir(m.paths, "chosen")

	m, _ = m.saveTo(destination)

	if m.session.SessionDir != destination {
		t.Fatalf("SessionDir = %q, want %q", m.session.SessionDir, destination)
	}
	if m.session.Doc.Identity.ID != oldID {
		t.Fatal("first save unexpectedly forked identity")
	}
	if _, err := os.Stat(config.SessionFilePath(destination)); err != nil {
		t.Fatalf("first save missing destination file: %v", err)
	}
}

func TestSaveToDifferentPersistedDirectoryForksAndReloads(t *testing.T) {
	m := testRootModel(t, "source", true)
	m.session.Append(chat.NewUserMessage("msg_1", "hello", ""))
	if err := m.session.Save(); err != nil {
		t.Fatalf("save source: %v", err)
	}
	sourceID := m.session.Doc.Identity.ID
	destination := config.RootSessionDir(m.paths, "fork")

	m, _ = m.saveTo(destination)

	if m.session.SessionDir != destination {
		t.Fatalf("SessionDir = %q, want %q", m.session.SessionDir, destination)
	}
	if m.session.Doc.Identity.ID == sourceID {
		t.Fatal("persisted save-to did not fork identity")
	}
	loaded, err := config.LoadSessionDoc(destination)
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}
	if loaded.Identity.ID != m.session.Doc.Identity.ID {
		t.Fatalf("in-memory fork ID %q differs from disk %q", m.session.Doc.Identity.ID, loaded.Identity.ID)
	}
}

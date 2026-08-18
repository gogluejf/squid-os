package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/media"
	runtimeconfig "squid-os/internal/runtime"
)

func TestUnsavedSessionInitWorkspaceCreatesTempWorkspace(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root, TempFolder: t.TempDir()}

	session := NewRootSession(config.SessionConfig{}, paths, runtimeconfig.Catalog{})
	session.InitWorkspace()

	if session.Workspace == nil {
		t.Fatal("workspace should be initialized")
	}

	// Should be a TempWorkspace
	if _, ok := session.Workspace.(*media.TempWorkspace); !ok {
		t.Fatal("unsaved session should have a TempWorkspace")
	}

	// Temp directory should be under temp folder, not sessions
	mediaDir := session.Workspace.Dir()
	if filepath.HasPrefix(mediaDir, root) {
		t.Fatalf("temp workspace media dir %q should not be under sessions dir %q", mediaDir, root)
	}
}

func TestLoadedSessionInitWorkspaceCreatesPersistentWorkspace(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root, TempFolder: t.TempDir()}

	// Create a saved session
	doc := config.NewSessionDoc(config.SessionConfig{})
	sessionDir := config.RootSessionDir(paths, "saved-session")
	if err := config.SaveSessionDoc(sessionDir, doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc: %v", err)
	}

	// Load and init workspace
	loadedDoc, err := config.LoadSessionDoc(sessionDir)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRootSession(loadedDoc, "saved-session", paths, runtimeconfig.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	loaded.InitWorkspace()

	if _, ok := loaded.Workspace.(*media.PersistentWorkspace); !ok {
		t.Fatal("loaded session should have a PersistentWorkspace")
	}

	// Media dir should be under the session directory
	mediaDir := loaded.Workspace.Dir()
	if !filepath.HasPrefix(mediaDir, sessionDir) {
		t.Fatalf("persistent workspace media dir %q should be under session dir %q", mediaDir, sessionDir)
	}
}

func TestSaveMigratesTempWorkspace(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root, TempFolder: t.TempDir()}

	session := NewRootSession(config.SessionConfig{}, paths, runtimeconfig.Catalog{})
	session.InitWorkspace()

	// Ingest a file into the temp workspace
	tempDir := session.tempWorkspaceDir
	mediaDir := filepath.Join(tempDir, "media")
	if err := os.WriteFile(filepath.Join(mediaDir, "test.png"), []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
	}

	// Register the attachment
	att := media.Attachment{
		ID:           "test-attach",
		FileName: "test.png",
		MIME:         "image/png",
		Kind:         media.KindImage,
	}
	session.AddAttachment(att)

	// Set the destination
	destinationDir := config.RootSessionDir(paths, "first-save")
	session.SessionDir = destinationDir

	// First save should migrate
	err := session.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify chat.json exists
	if _, err := os.Stat(config.SessionFilePath(destinationDir)); os.IsNotExist(err) {
		t.Fatal("chat.json should exist after first save")
	}

	// Verify media was migrated to the session directory
	if _, err := os.Stat(filepath.Join(destinationDir, "media", "test.png")); os.IsNotExist(err) {
		t.Fatal("media file should exist in destination after migration")
	}

	// Verify workspace is now persistent
	if _, ok := session.Workspace.(*media.PersistentWorkspace); !ok {
		t.Fatal("workspace should be persistent after first save")
	}

	// Verify temp dir tracking is cleared
	if session.tempWorkspaceDir != "" {
		t.Fatal("tempWorkspaceDir should be empty after migration")
	}
}

func TestSaveNoTempWorkspace(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root, TempFolder: t.TempDir()}

	// Create a saved session
	doc := config.NewSessionDoc(config.SessionConfig{})
	sessionDir := config.RootSessionDir(paths, "existing")
	if err := config.SaveSessionDoc(sessionDir, doc, nil); err != nil {
		t.Fatal(err)
	}

	loadedDoc, err := config.LoadSessionDoc(sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRootSession(loadedDoc, "existing", paths, runtimeconfig.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	loaded.InitWorkspace()

	// Save on an already-saved session should work fine
	err = loaded.Save()
	if err != nil {
		t.Fatalf("Save on existing session: %v", err)
	}
}

func TestSaveNoAttachments(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root, TempFolder: t.TempDir()}

	session := NewRootSession(config.SessionConfig{}, paths, runtimeconfig.Catalog{})
	session.InitWorkspace()

	// No attachments — first save should still work
	destinationDir := config.RootSessionDir(paths, "no-attachments")
	session.SessionDir = destinationDir

	err := session.Save()
	if err != nil {
		t.Fatalf("Save (no attachments): %v", err)
	}

	// Verify chat.json exists
	if _, err := os.Stat(config.SessionFilePath(destinationDir)); os.IsNotExist(err) {
		t.Fatal("chat.json should exist")
	}
}

func TestAutoSaveMigratesTempWorkspace(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root, TempFolder: t.TempDir()}

	session := NewRootSession(config.SessionConfig{
		Autosave: config.SessionAutosave{Enabled: true, Name: "autosave-test"},
	}, paths, runtimeconfig.Catalog{})
	session.InitWorkspace()

	// Ingest a file into the temp workspace
	tempDir := session.tempWorkspaceDir
	mediaDir := filepath.Join(tempDir, "media")
	if err := os.WriteFile(filepath.Join(mediaDir, "auto.png"), []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
	}

	att := media.Attachment{
		ID:           "auto-attach",
		FileName: "auto.png",
		MIME:         "image/png",
		Kind:         media.KindImage,
	}
	session.AddAttachment(att)

	// Regular Save (autosave) — should migrate temp workspace to session dir
	// so media files aren't lost when the process exits.
	err := session.Save()
	if err != nil {
		t.Fatalf("Save (autosave): %v", err)
	}

	// Workspace should now be persistent after migration
	if _, ok := session.Workspace.(*media.PersistentWorkspace); !ok {
		t.Fatal("autosave should migrate workspace to PersistentWorkspace")
	}

	// Verify temp dir tracking is cleared
	if session.tempWorkspaceDir != "" {
		t.Fatal("tempWorkspaceDir should be empty after migration")
	}

	// Verify media file exists in the session directory
	destinationDir := session.SessionDir
	if _, err := os.Stat(filepath.Join(destinationDir, "media", "auto.png")); os.IsNotExist(err) {
		t.Fatal("media file should exist in session directory after autosave migration")
	}
}

func TestMigrateTempWorkspaceRollbackOnFailure(t *testing.T) {
	// Test that a failed migration doesn't leave partial state
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := media.NewTempWorkspace(srcDir)

	// Create a file in the media dir
	srcMedia := filepath.Join(srcDir, "media")
	if err := os.WriteFile(filepath.Join(srcMedia, "a.png"), []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
	}

	// Try to migrate to a destination that's a file (will fail)
	blocker := filepath.Join(dstDir, "media")
	if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := media.MigrateTempWorkspace(context.Background(), src, dstDir)
	if err == nil {
		t.Fatal("expected error for invalid destination")
	}

	// Verify source file still exists (rollback happened)
	if _, err := os.Stat(filepath.Join(srcMedia, "a.png")); os.IsNotExist(err) {
		t.Fatal("source file should still exist after failed migration")
	}
}

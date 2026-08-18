package run

import (
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/media"
	runtimeconfig "squid-os/internal/runtime"
)

func TestBootstrapFreshRootAutosaveOffHasDirectoryButDoesNotCheckpoint(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	session, _, err := bootstrapSession(Request{Session: runtimeconfig.SessionRequest{
		Paths:  paths,
		Config: config.SessionConfig{Autosave: config.SessionAutosave{Enabled: false}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionDir == "" {
		t.Fatal("autosave-off root has no canonical directory")
	}
	if session.Info.Name != "" {
		t.Fatalf("unsaved root has persisted display name %q", session.Info.Name)
	}
	if err := checkpointSave(session); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadSessionDoc(session.SessionDir); err == nil {
		t.Fatal("autosave-off checkpoint persisted the session")
	}
}

func TestBootstrapChildAutosaveOffHasDirectoryButDoesNotCheckpoint(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	parentDir := config.RootSessionDir(paths, "parent")
	session, _, err := bootstrapSession(Request{
		Session: runtimeconfig.SessionRequest{Paths: paths, Config: config.SessionConfig{Autosave: config.SessionAutosave{Enabled: false}}},
		ChildSession: &config.ChildSessionOptions{
			ID: "child", ParentID: "parent", RootID: "parent", ParentToolCallID: "tool", Depth: 1, ParentSessionDir: parentDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionDir == "" {
		t.Fatal("autosave-off child has no canonical directory")
	}
	if session.Info.Name != "" {
		t.Fatalf("unsaved child has persisted display name %q", session.Info.Name)
	}
	if filepath.Dir(session.SessionDir) != filepath.Join(parentDir, "agents") {
		t.Fatalf("child directory = %q", session.SessionDir)
	}
	if err := checkpointSave(session); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadSessionDoc(session.SessionDir); err == nil {
		t.Fatal("autosave-off child checkpoint persisted the session")
	}
}

func TestBootstrapFreshRootUsesAutosaveNameDirectory(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	cfg := config.SessionConfig{Autosave: config.SessionAutosave{Enabled: true, Name: "fresh-root"}}

	session, _, err := bootstrapSession(Request{Session: runtimeconfig.SessionRequest{Paths: paths, Config: cfg}})
	if err != nil {
		t.Fatal(err)
	}
	want := config.RootSessionDir(paths, "fresh-root")
	if session.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", session.SessionDir, want)
	}
}

func TestBootstrapContinuedRootForksToExplicitAutosaveName(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	sourceDir := config.RootSessionDir(paths, "foo")
	doc := config.NewSessionDoc(config.SessionConfig{Autosave: config.SessionAutosave{Enabled: true, Name: "bar"}})
	sourceID := doc.Identity.ID
	if err := config.SaveSessionDoc(sourceDir, doc, nil); err != nil {
		t.Fatal(err)
	}

	session, _, err := bootstrapSession(Request{Session: runtimeconfig.SessionRequest{
		Paths:           paths,
		ExistingSession: &doc,
		SessionName:     "foo",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := config.RootSessionDir(paths, "bar")
	if session.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", session.SessionDir, want)
	}
	if session.Doc.Identity.ID == sourceID {
		t.Fatal("save-name continuation did not fork identity")
	}
	persisted, err := config.LoadSessionDoc(want)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Config.Autosave.Name != "bar" {
		t.Fatalf("forked autosave name = %q, want bar", persisted.Config.Autosave.Name)
	}
}

func TestBootstrapContinuedRootUsesLoadedNameDirectory(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	doc := config.NewSessionDoc(config.SessionConfig{Autosave: config.SessionAutosave{Enabled: true, Name: "continued"}})

	session, _, err := bootstrapSession(Request{Session: runtimeconfig.SessionRequest{
		Paths:           paths,
		ExistingSession: &doc,
		SessionName:     "continued",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(paths.Sessions, "continued")
	if session.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", session.SessionDir, want)
	}
}

// --- Task 5.1: Attachment workspace initialization on bootstrap ---

func TestBootstrapFreshRootInitializesTempWorkspace(t *testing.T) {
	paths := config.Paths{
		Sessions:   t.TempDir(),
		TempFolder: t.TempDir(),
	}
	session, _, err := bootstrapSession(Request{Session: runtimeconfig.SessionRequest{
		Paths:  paths,
		Config: config.SessionConfig{Autosave: config.SessionAutosave{Enabled: false}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// Initialize workspace
	session.InitWorkspace()
	if session.Workspace == nil {
		t.Fatal("workspace should be initialized")
	}

	// Verify it's a temp workspace
	if _, ok := session.Workspace.(*media.PersistentWorkspace); ok {
		t.Fatal("fresh unsaved session should have a TempWorkspace")
	}

	// Verify the workspace media dir is NOT under the session dir
	if session.Workspace.Dir() == filepath.Join(session.SessionDir, "media") {
		t.Fatal("temp workspace media should not be in session dir")
	}

	// Cleanup
	if err := session.CleanupWorkspace(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func TestBootstrapIncognitoSessionUsesIncognitoWorkspace(t *testing.T) {
	paths := config.Paths{
		Sessions:   t.TempDir(),
		TempFolder: t.TempDir(),
	}
	session, _, err := bootstrapSession(Request{Session: runtimeconfig.SessionRequest{
		Paths:  paths,
		Config: config.SessionConfig{Autosave: config.SessionAutosave{Enabled: false}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	session.SetIncognito(true)
	session.InitWorkspace()
	if session.Workspace == nil {
		t.Fatal("incognito session should have a workspace")
	}

	// Verify workspace dir is incognito
	wsDir := filepath.Dir(session.Workspace.Dir())
	baseName := filepath.Base(wsDir)
	if !media.IsIncognitoDir(baseName) {
		t.Fatalf("incognito workspace dir %q should have incognito prefix", baseName)
	}

	// Cleanup
	if err := session.CleanupWorkspace(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

package run

import (
	"path/filepath"
	"testing"

	"squid-os/internal/config"
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
	if session.SessionDir == "" || session.Info.Name == "" {
		t.Fatal("autosave-off root has no canonical name/directory")
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
	if session.SessionDir == "" || session.Info.Name == "" {
		t.Fatal("autosave-off child has no canonical name/directory")
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

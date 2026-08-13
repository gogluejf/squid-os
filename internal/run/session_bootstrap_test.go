package run

import (
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

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

func TestBootstrapContinuedRootUsesLoadedNameDirectory(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	doc := config.NewSessionDoc(config.SessionConfig{Autosave: config.SessionAutosave{Enabled: true, Name: "original"}})

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

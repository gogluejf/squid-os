package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSessionDocKeepsInitialConfigIndependent(t *testing.T) {
	cfg := SessionConfig{
		Tools:  []string{"read"},
		Skills: []CapabilityRef{{Scope: "global", Name: "review"}},
		Agents: []CapabilityRef{{Scope: "global", Name: "researcher"}},
	}
	doc := NewSessionDoc(cfg)

	doc.Config.Tools[0] = "bash"
	doc.Config.Skills[0] = CapabilityRef{Scope: "global", Name: "plan"}
	doc.Config.Agents[0] = CapabilityRef{Scope: "global", Name: "coder"}

	if doc.Initial.Tools[0] != "read" || doc.Initial.Skills[0].Name != "review" || doc.Initial.Agents[0].Name != "researcher" {
		t.Fatalf("initial config mutated with current config: %+v", doc.Initial)
	}
}

func TestParseAuthorizationMode(t *testing.T) {
	for _, value := range []AuthorizationMode{AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll, AuthorizationEndOnWrite, AuthorizationEndOnAll} {
		got, err := ParseAuthorizationMode(string(value))
		if err != nil || got != value {
			t.Fatalf("value=%q got=%q err=%v", value, got, err)
		}
	}
	if _, err := ParseAuthorizationMode("wrong"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestSessionStorageUsesFolderAndChatJSON(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Sessions: root}
	doc := NewSessionDoc(SessionConfig{})

	if err := SaveSessionDoc(paths, "newest", doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}
	want := filepath.Join(root, "newest", "chat.json")
	if got := SessionPath(paths, "newest"); got != want {
		t.Fatalf("SessionPath() = %q, want %q", got, want)
	}
	chatInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("chat.json was not created: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(want))
	if err != nil {
		t.Fatalf("session folder was not created: %v", err)
	}
	if !dirInfo.ModTime().Equal(chatInfo.ModTime()) {
		t.Fatalf("folder mtime %v does not match chat.json mtime %v", dirInfo.ModTime(), chatInfo.ModTime())
	}
	if _, err := LoadSessionDoc(paths, "newest"); err != nil {
		t.Fatalf("LoadSessionDoc failed: %v", err)
	}
}

func TestListSessionsSortsByChatJSONModTime(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Sessions: root}
	for _, name := range []string{"older", "newer"} {
		if err := SaveSessionDoc(paths, name, NewSessionDoc(SessionConfig{}), nil); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	older := time.Now().Add(-time.Hour)
	if err := os.Chtimes(SessionPath(paths, "older"), older, older); err != nil {
		t.Fatalf("set older chat time: %v", err)
	}

	sessions := ListSessions(paths)
	if len(sessions) != 2 {
		t.Fatalf("ListSessions returned %d sessions, want 2", len(sessions))
	}
	if sessions[0].Name != "newer" || sessions[1].Name != "older" {
		t.Fatalf("unexpected order: %q, %q", sessions[0].Name, sessions[1].Name)
	}
}

package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanupStaleRemovesOldIncognitoDirs(t *testing.T) {
	root := t.TempDir()

	// Create a stale incognito dir
	staleDir := filepath.Join(root, IncognitoPrefix+"abc12345-ef01")
	if err := os.MkdirAll(staleDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Make it old
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(staleDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create a fresh incognito dir (should NOT be removed)
	freshDir := filepath.Join(root, IncognitoPrefix+"xyz98765-ab12")
	if err := os.MkdirAll(freshDir, 0755); err != nil {
		t.Fatal(err)
	}

	policy := CleanupPolicy{
		Root:      root,
		OlderThan: 24 * time.Hour,
		MaxEntries: 10,
	}
	if err := CleanupStale(policy); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	// Stale dir should be gone
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale incognito dir should have been removed")
	}
	// Fresh dir should still exist
	if _, err := os.Stat(freshDir); os.IsNotExist(err) {
		t.Error("fresh incognito dir should not have been removed")
	}
}

func TestCleanupStaleRemovesOldSessionDirs(t *testing.T) {
	root := t.TempDir()

	// Create a stale session dir
	staleDir := filepath.Join(root, SessionPrefix+"abc12345")
	if err := os.MkdirAll(staleDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(staleDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	policy := CleanupPolicy{
		Root:      root,
		OlderThan: 24 * time.Hour,
		MaxEntries: 10,
	}
	if err := CleanupStale(policy); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale session dir should have been removed")
	}
}

func TestCleanupStaleIgnoresNonSquidDirs(t *testing.T) {
	root := t.TempDir()

	// Create a non-Squid dir (should NOT be removed even if old)
	otherDir := filepath.Join(root, "random-project")
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(otherDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	policy := CleanupPolicy{
		Root:      root,
		OlderThan: 24 * time.Hour,
		MaxEntries: 10,
	}
	if err := CleanupStale(policy); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	// Non-Squid dir should still exist
	if _, err := os.Stat(otherDir); os.IsNotExist(err) {
		t.Error("non-Squid dir should not have been removed")
	}
}

func TestCleanupStaleRespectsMaxEntries(t *testing.T) {
	root := t.TempDir()

	// Create 5 stale incognito dirs
	for i := 0; i < 5; i++ {
		dir := filepath.Join(root, IncognitoPrefix+"test"+string(rune('0'+i)))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		oldTime := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(dir, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}

	policy := CleanupPolicy{
		Root:      root,
		OlderThan: 24 * time.Hour,
		MaxEntries: 2,
	}
	if err := CleanupStale(policy); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	// At least 3 should remain (only 2 removed)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	remaining := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), IncognitoPrefix) {
			remaining++
		}
	}
	if remaining < 3 {
		t.Errorf("expected at least 3 dirs remaining, got %d", remaining)
	}
}

func TestCleanupStaleValidatesInputs(t *testing.T) {
	policy := CleanupPolicy{Root: "", OlderThan: time.Hour, MaxEntries: 10}
	if err := CleanupStale(policy); err == nil {
		t.Error("expected error for empty root")
	}

	policy = CleanupPolicy{Root: "/tmp", OlderThan: 0, MaxEntries: 10}
	if err := CleanupStale(policy); err == nil {
		t.Error("expected error for zero OlderThan")
	}

	policy = CleanupPolicy{Root: "/tmp", OlderThan: time.Hour, MaxEntries: 0}
	if err := CleanupStale(policy); err == nil {
		t.Error("expected error for zero MaxEntries")
	}
}

func TestIncognitoWorkspaceDir(t *testing.T) {
	root := t.TempDir()
	dir, err := IncognitoWorkspaceDir(root)
	if err != nil {
		t.Fatalf("IncognitoWorkspaceDir: %v", err)
	}
	if !strings.HasPrefix(dir, root) {
		t.Errorf("dir %q should be under root %q", dir, root)
	}
	if !strings.Contains(dir, IncognitoPrefix) {
		t.Errorf("dir %q should contain %q", dir, IncognitoPrefix)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("created dir should be a directory")
	}
}

func TestRemoveWorkspace(t *testing.T) {
	// Empty dir should be no-op
	if err := RemoveWorkspace(""); err != nil {
		t.Fatalf("RemoveWorkspace(\"\") should be no-op: %v", err)
	}

	// Non-existent dir should be no-op
	if err := RemoveWorkspace("/nonexistent/path"); err != nil {
		t.Fatalf("RemoveWorkspace on non-existent path: %v", err)
	}

	// Existing dir should be removed
	root := t.TempDir()
	dir := filepath.Join(root, "test-workspace")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorkspace(dir); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("workspace dir should have been removed")
	}
}

func TestIsSquidTempDir(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{IncognitoPrefix + "abc", true},
		{SessionPrefix + "abc", true},
		{"random", false},
		{"squid-other-abc", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsSquidTempDir(tt.name); got != tt.want {
			t.Errorf("IsSquidTempDir(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsIncognitoDir(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{IncognitoPrefix + "abc", true},
		{SessionPrefix + "abc", false},
		{"random", false},
	}
	for _, tt := range tests {
		if got := IsIncognitoDir(tt.name); got != tt.want {
			t.Errorf("IsIncognitoDir(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

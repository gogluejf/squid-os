package tools

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/util"
)

func TestReadFileFullReadUnchanged(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{"path": "test.txt"}, rt)

	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Result != content {
		t.Fatalf("expected full content, got %q", result.Result)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(result.Files))
	}
	// Checksum should be computed from full file
	expectedChecksum := util.ComputeChecksum([]byte(content))
	if result.Files[0].Checksum != expectedChecksum {
		t.Fatalf("expected checksum %s, got %s", expectedChecksum, result.Files[0].Checksum)
	}
	if result.Files[0].Path != file {
		t.Fatalf("expected path %s, got %s", file, result.Files[0].Path)
	}
}

func TestReadFileMissingPath(t *testing.T) {
	rt := RuntimeContext{Config: config.SessionConfig{}}
	result := ReadFile.Execute(map[string]interface{}{}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !contains(result.Error, "path is required") {
		t.Fatalf("expected path error, got %q", result.Error)
	}
}

func TestReadFileMissingFile(t *testing.T) {
	dir := t.TempDir()
	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path": "nonexistent.txt",
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !contains(result.Error, "failed to read file") {
		t.Fatalf("expected file read error, got %q", result.Error)
	}
}

func TestReadFileEmptyPath(t *testing.T) {
	rt := RuntimeContext{Config: config.SessionConfig{}}
	result := ReadFile.Execute(map[string]interface{}{"path": ""}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && search(s, sub) }
func search(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

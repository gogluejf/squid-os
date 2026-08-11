package tools

import (
	"os"
	"path/filepath"
	"strings"
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

func TestReadFileRangedReadValid(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(2),
		"end_line":   float64(4),
	}, rt)

	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	expected := "line2\nline3\nline4"
	if result.Result != expected {
		t.Fatalf("expected %q, got %q", expected, result.Result)
	}
	// Checksum should be computed from the FULL file, not the range
	expectedChecksum := util.ComputeChecksum([]byte(content))
	if result.Files[0].Checksum != expectedChecksum {
		t.Fatalf("expected full-file checksum %s, got %s", expectedChecksum, result.Files[0].Checksum)
	}
}

func TestReadFileRangedReadSingleLine(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(2),
		"end_line":   float64(2),
	}, rt)

	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Result != "line2" {
		t.Fatalf("expected %q, got %q", "line2", result.Result)
	}
}

func TestReadFileRangedReadEntireFile(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(1),
		"end_line":   float64(3),
	}, rt)

	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Result != content {
		t.Fatalf("expected full content, got %q", result.Result)
	}
}

func TestReadFileRangedReadEndBeyondFile(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(2),
		"end_line":   float64(100),
	}, rt)

	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected end_line to clamp to EOF, got error %q", result.Error)
	}
	if result.Result != "line2\nline3" {
		t.Fatalf("expected lines 2 through EOF, got %q", result.Result)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected one file entry, got %d", len(result.Files))
	}
}

func TestReadFileRangedReadMissingPath(t *testing.T) {
	rt := RuntimeContext{Config: config.SessionConfig{}}
	result := ReadFile.Execute(map[string]interface{}{
		"start_line": float64(1),
		"end_line":   float64(3),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !strings.Contains(result.Error, "path is required") {
		t.Fatalf("expected path error, got %q", result.Error)
	}
}

func TestReadFileRangedReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "nonexistent.txt",
		"start_line": float64(1),
		"end_line":   float64(3),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !strings.Contains(result.Error, "failed to read file") {
		t.Fatalf("expected file read error, got %q", result.Error)
	}
}

func TestReadFileRangedReadOnlyStart(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(2),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !strings.Contains(result.Error, "both start_line and end_line must be provided together") {
		t.Fatalf("expected pair error, got %q", result.Error)
	}
}

func TestReadFileRangedReadOnlyEnd(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":     "test.txt",
		"end_line": float64(2),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !strings.Contains(result.Error, "both start_line and end_line must be provided together") {
		t.Fatalf("expected pair error, got %q", result.Error)
	}
}

func TestReadFileRangedReadZeroLines(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(0),
		"end_line":   float64(2),
	}, rt)

	// 0 is normalized to 1, so lines 1-2 succeed.
	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success (0 normalized to 1), got error: %s", result.Error)
	}
	if result.Result != "line1\nline2" {
		t.Fatalf("expected lines 1-2, got %q", result.Result)
	}
}

func TestReadFileRangedReadReversed(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(3),
		"end_line":   float64(1),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !strings.Contains(result.Error, "start_line") || !strings.Contains(result.Error, "end_line") {
		t.Fatalf("expected range error, got %q", result.Error)
	}
}

func TestReadFileRangedReadStartBeyondFile(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(10),
		"end_line":   float64(15),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if !strings.Contains(result.Error, "exceeds file length") {
		t.Fatalf("expected exceeds error, got %q", result.Error)
	}
}

func TestReadFileRangedReadNoFileStateUpdate(t *testing.T) {
	// Verify that a ranged read error does NOT produce any FileEntry
	// (uses an out-of-bounds range to trigger an error)
	dir := t.TempDir()
	content := "line1\nline2\nline3"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(10),
		"end_line":   float64(20),
	}, rt)

	if result.Status != ResultStatusError {
		t.Fatalf("expected error, got success")
	}
	if len(result.Files) != 0 {
		t.Fatalf("expected no file entries on error, got %d", len(result.Files))
	}
}

func TestReadFileRangedReadFirstAndLastLine(t *testing.T) {
	dir := t.TempDir()
	content := "first\nmiddle\nlast"
	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte(content), 0644)

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}

	// First line
	result := ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(1),
		"end_line":   float64(1),
	}, rt)
	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Result != "first" {
		t.Fatalf("expected %q, got %q", "first", result.Result)
	}

	// Last line
	result = ReadFile.Execute(map[string]interface{}{
		"path":       "test.txt",
		"start_line": float64(3),
		"end_line":   float64(3),
	}, rt)
	if result.Status != ResultStatusSuccess {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Result != "last" {
		t.Fatalf("expected %q, got %q", "last", result.Result)
	}
}

func TestReadFileRangedReadAcceptsIntegralGoNumericForms(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4"
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	cases := []struct {
		name  string
		start interface{}
		end   interface{}
	}{
		{"int", int(2), int(3)},
		{"int64", int64(2), int64(3)},
		{"uint", uint(2), uint(3)},
		{"float64 integral", float64(2), float64(3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ReadFile.Execute(map[string]interface{}{
				"path":       "test.txt",
				"start_line": tc.start,
				"end_line":   tc.end,
			}, rt)
			if result.Status != ResultStatusSuccess {
				t.Fatalf("expected success, got %s", result.Error)
			}
			if result.Result != "line2\nline3" {
				t.Fatalf("expected line2-line3, got %q", result.Result)
			}
		})
	}
}

func TestReadFileRangedReadRejectsNonIntegralValues(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("line1\nline2"), 0644); err != nil {
		t.Fatal(err)
	}

	rt := RuntimeContext{Config: config.SessionConfig{WorkingDir: dir}}
	cases := []struct {
		name  string
		start interface{}
	}{
		{"fraction", float64(1.5)},
		{"string", "1"},
		{"bool", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ReadFile.Execute(map[string]interface{}{
				"path":       "test.txt",
				"start_line": tc.start,
				"end_line":   2,
			}, rt)
			if result.Status != ResultStatusError {
				t.Fatalf("expected error, got result %q", result.Result)
			}
			if !strings.Contains(result.Error, "start_line") || !strings.Contains(result.Error, "integer") {
				t.Fatalf("expected clear integral error, got %q", result.Error)
			}
			if len(result.Files) != 0 {
				t.Fatalf("expected no file entries on parse error, got %d", len(result.Files))
			}
		})
	}
}

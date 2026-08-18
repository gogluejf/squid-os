package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- Attachment Model Tests ---

func TestAttachmentValidate(t *testing.T) {
	valid := Attachment{
		ID:           "test-id",
		FileName: "test.png",
		MIME:         "image/png",
		Kind:         KindImage,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid attachment failed validation: %v", err)
	}
}

func TestAttachmentValidateRejectsBadPaths(t *testing.T) {
	cases := []struct {
		a    Attachment
		want string
	}{
		{Attachment{}, "ID is empty"},
		{Attachment{ID: "x"}, "FileName is empty"},
		{Attachment{ID: "x", FileName: "x"}, "MIME is empty"},
		{Attachment{ID: "x", FileName: "../escape", MIME: "image/png"}, "bare filename"},
		{Attachment{ID: "x", FileName: "sub/x.png", MIME: "image/png"}, "bare filename"},
	}
	for _, tc := range cases {
		err := tc.a.Validate()
		if err == nil {
			t.Fatalf("expected error for %s, got nil", tc.want)
		}
		if tc.want != "" && !contains(err.Error(), tc.want) {
			t.Fatalf("expected error containing %q, got %v", tc.want, err)
		}
	}
}

func TestAttachmentCanonicalRef(t *testing.T) {
	a := Attachment{ID: "abc-123", FileName: "abc-123.png"}
	ref := a.CanonicalRef()
	if ref != "@file:abc-123.png" {
		t.Fatalf("CanonicalRef() = %q, want @file:abc-123.png", ref)
	}
}

func TestResolveRefByID(t *testing.T) {
	registry := []Attachment{
		{ID: "a", FileName: "a.png", MIME: "image/png"},
		{ID: "b", FileName: "b.pdf", MIME: "application/pdf"},
	}
	a, ok := ResolveRef(registry, "b")
	if !ok || a.ID != "b" {
		t.Fatalf("ResolveRef(registry, b) = %+v, %v", a, ok)
	}
	_, ok = ResolveRef(registry, "missing")
	if ok {
		t.Fatal("ResolveRef should return false for missing ID")
	}
}

func TestResolveRefBySHA256(t *testing.T) {
	registry := []Attachment{
		{ID: "a", SHA256: "abc123"},
		{ID: "b", SHA256: "def456"},
	}
	a, ok := ResolveRefBySHA256(registry, "def456")
	if !ok || a.ID != "b" {
		t.Fatalf("ResolveRefBySHA256 = %+v, %v", a, ok)
	}
}

func TestDeduplicateByHash(t *testing.T) {
	registry := []Attachment{
		{ID: "a", SHA256: "hash1"},
		{ID: "b", SHA256: "hash1"}, // duplicate
		{ID: "c", SHA256: "hash2"},
		{ID: "d", SHA256: ""},      // no hash, kept
	}
	deduped := DeduplicateByHash(registry)
	if len(deduped) != 3 {
		t.Fatalf("DeduplicateByHash returned %d entries, want 3", len(deduped))
	}
	if deduped[0].ID != "a" || deduped[1].ID != "c" || deduped[2].ID != "d" {
		t.Fatalf("unexpected deduplication order: %v", deduped)
	}
}

func TestIsMediaKind(t *testing.T) {
	if !IsMediaKind(KindImage) {
		t.Fatal("KindImage should be media")
	}
	if !IsMediaKind(KindPDF) {
		t.Fatal("KindPDF should be media")
	}
	if IsMediaKind(KindText) {
		t.Fatal("KindText should not be media")
	}
	if IsMediaKind(KindAudio) {
		t.Fatal("KindAudio should not be media")
	}
}

// --- Kind & MIME Tests ---

func TestMIMEForPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.png", "image/png"},
		{"photo.gif", "image/gif"},
		{"photo.webp", "image/webp"},
		{"doc.pdf", "application/pdf"},
		{"readme.md", "text/plain"},
		{"code.go", "text/plain"},
		{"data.bin", "application/octet-stream"},
	}
	for _, tc := range tests {
		got := MIMEForPath(tc.path)
		if got != tc.want {
			t.Errorf("MIMEForPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestKindForMIME(t *testing.T) {
	tests := []struct {
		mime string
		want Kind
	}{
		{"image/jpeg", KindImage},
		{"image/png", KindImage},
		{"application/pdf", KindPDF},
		{"text/plain", KindText},
		{"audio/mpeg", KindAudio},
		{"video/mp4", KindVideo},
		{"application/octet-stream", KindFile},
		{"application/json", KindFile},
	}
	for _, tc := range tests {
		got := KindForMIME(tc.mime)
		if got != tc.want {
			t.Errorf("KindForMIME(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}

// --- Workspace Tests ---

func TestPersistentWorkspaceDir(t *testing.T) {
	w := NewPersistentWorkspace("/sessions/test-session")
	if w.Dir() != "/sessions/test-session/media" {
		t.Fatalf("Dir() = %q, want /sessions/test-session/media", w.Dir())
	}
}

func TestTempWorkspaceDir(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)
	expected := filepath.Join(tempDir, "media")
	if w.Dir() != expected {
		t.Fatalf("Dir() = %q, want %q", w.Dir(), expected)
	}
	// Verify the media directory was created
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatal("TempWorkspace should create media directory")
	}
}

func TestTempWorkspaceDoesNotWritePersistentSessions(t *testing.T) {
	// TempWorkspace should use its own temp directory, not the sessions dir.
	sessionsDir := t.TempDir()
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	mediaDir := w.Dir()
	if !filepath.HasPrefix(mediaDir, tempDir) {
		t.Fatalf("TempWorkspace Dir should be under tempDir, got %q", mediaDir)
	}
	if filepath.HasPrefix(mediaDir, sessionsDir) {
		t.Fatalf("TempWorkspace Dir should NOT be under sessions dir: %q", mediaDir)
	}
}

// --- Path Safety Tests ---

func TestIsTraversalSafe(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"photo.png", true},
		{"file-abc123.png", true},
		{"../etc/passwd", false},
		{"sub/photo.png", false},
		{"", false},
		{".", false},
		{"..", false},
		{"a\\b.png", false},
	}
	for _, tc := range tests {
		got := IsTraversalSafe(tc.name)
		if got != tc.want {
			t.Errorf("IsTraversalSafe(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestValidateAttachmentName(t *testing.T) {
	if err := ValidateAttachmentName("safe.png"); err != nil {
		t.Fatalf("safe name rejected: %v", err)
	}
	if err := ValidateAttachmentName("../etc/passwd"); err == nil {
		t.Fatal("traversal name accepted")
	}
	if err := ValidateAttachmentName("sub/escape.png"); err == nil {
		t.Fatal("path name accepted")
	}
}

func TestIngestInContext(t *testing.T) {
	ctx := context.Background()
	if err := IngestInContext(ctx); err != nil {
		t.Fatalf("IngestInContext(background) = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := IngestInContext(ctx); err == nil {
		t.Fatal("IngestInContext(canceled) should return error")
	}
}

// --- Attachment JSON Round-Trip ---

func TestAttachmentJSONRoundTrip(t *testing.T) {
	a := Attachment{
		ID:           "test-123",
		FileName: "photo.png",
		DisplayName:  "screenshot.png",
		MIME:         "image/png",
		Kind:         KindImage,
		Size:         1024,
		SHA256:       "abc123",
		Source:       SourceFile,
		CreatedAt:    "2025-01-01T00:00:00Z",
	}

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Attachment
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ID != a.ID || decoded.FileName != a.FileName ||
		decoded.MIME != a.MIME || decoded.Kind != a.Kind ||
		decoded.Size != a.Size || decoded.SHA256 != a.SHA256 ||
		decoded.Source != a.Source {
		t.Fatalf("round-trip mismatch: %+v != %+v", decoded, a)
	}
}

func TestKindJSONRoundTrip(t *testing.T) {
	for _, k := range []Kind{KindImage, KindPDF, KindText, KindAudio, KindVideo, KindFile} {
		data, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("marshal %v: %v", k, err)
		}
		var decoded Kind
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %v: %v", k, err)
		}
		if decoded != k {
			t.Fatalf("round-trip mismatch: %v != %v", decoded, k)
		}
	}
	// Empty kind
	var empty Kind
	data, _ := json.Marshal(empty)
	var decoded Kind
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal empty kind: %v", err)
	}
	if decoded != "" {
		t.Fatalf("empty kind round-trip: got %q", decoded)
	}
}

func TestSourceJSONRoundTrip(t *testing.T) {
	for _, s := range []Source{SourceFile, SourceURL, SourceClipboard, SourceTool, SourceMigrated} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %v: %v", s, err)
		}
		var decoded Source
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %v: %v", s, err)
		}
		if decoded != s {
			t.Fatalf("round-trip mismatch: %v != %v", decoded, s)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

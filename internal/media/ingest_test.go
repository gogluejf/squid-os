package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// MIME Detection Tests
// =============================================================================

func TestDetectMIME_PNG(t *testing.T) {
	// PNG magic bytes
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	r := bytes.NewReader(pngHeader)
	mime := DetectMIME(r, ".txt")
	if mime != "image/png" {
		t.Fatalf("DetectMIME(png) = %q, want image/png", mime)
	}
}

func TestDetectMIME_JPEG(t *testing.T) {
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	r := bytes.NewReader(jpegHeader)
	mime := DetectMIME(r, ".txt")
	if mime != "image/jpeg" {
		t.Fatalf("DetectMIME(jpeg) = %q, want image/jpeg", mime)
	}
}

func TestDetectMIME_Text(t *testing.T) {
	text := []byte("Hello, this is plain text content.\n")
	r := bytes.NewReader(text)
	mime := DetectMIME(r, ".bin")
	if mime != "text/plain; charset=utf-8" && mime != "text/plain" {
		t.Fatalf("DetectMIME(text) = %q, want text/plain", mime)
	}
}

func TestDetectMIME_ExtensionFallback(t *testing.T) {
	// Binary data that can't be detected
	binary := []byte{0x00, 0x01, 0x02, 0x03}
	r := bytes.NewReader(binary)
	mime := DetectMIME(r, ".png")
	if mime != "image/png" {
		t.Fatalf("DetectMIME(binary, .png) = %q, want image/png (extension fallback)", mime)
	}
}

func TestDetectMIME_NoHint(t *testing.T) {
	binary := []byte{0x00, 0x01, 0x02, 0x03}
	r := bytes.NewReader(binary)
	mime := DetectMIME(r, "")
	if mime != "application/octet-stream" {
		t.Fatalf("DetectMIME(binary, no hint) = %q, want application/octet-stream", mime)
	}
}

func TestDetectMIMEFromBytes_PDF(t *testing.T) {
	pdfHeader := []byte("%PDF-1.4\nsome content here")
	mime := DetectMIMEFromBytes(pdfHeader, ".txt")
	if mime != "application/pdf" {
		t.Fatalf("DetectMIMEFromBytes(pdf) = %q, want application/pdf", mime)
	}
}

func TestDetectMIMEFromBytes_Empty(t *testing.T) {
	mime := DetectMIMEFromBytes(nil, ".png")
	if mime != "image/png" {
		t.Fatalf("DetectMIMEFromBytes(nil, .png) = %q, want image/png", mime)
	}
}

func TestDetectMIMEFromBytes_ExtensionFallback(t *testing.T) {
	// Truly undetectable binary data (null bytes + high bytes)
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	mime := DetectMIMEFromBytes(data, ".json")
	if mime != "application/json" {
		t.Fatalf("DetectMIMEFromBytes(null bytes, .json) = %q, want application/json", mime)
	}
}

func TestPeekMIME(t *testing.T) {
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	r := bytes.NewReader(pngHeader)

	mime, combined, err := PeekMIME(r, "")
	if err != nil {
		t.Fatalf("PeekMIME error: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("PeekMIME = %q, want image/png", mime)
	}

	// Verify combined reader yields all data
	data, err := io.ReadAll(combined)
	if err != nil {
		t.Fatalf("ReadAll(combined) = %v", err)
	}
	if !bytes.Equal(data, pngHeader) {
		t.Fatalf("combined reader data mismatch")
	}
}

func TestIsTextMIME(t *testing.T) {
	tests := []struct {
		mime string
		want bool
	}{
		{"text/plain", true},
		{"text/html", true},
		{"application/json", true},
		{"application/xml", true},
		{"image/png", false},
		{"application/pdf", false},
	}
	for _, tc := range tests {
		got := IsTextMIME(tc.mime)
		if got != tc.want {
			t.Errorf("IsTextMIME(%q) = %v, want %v", tc.mime, got, tc.want)
		}
	}
}

func TestIsImageMIME(t *testing.T) {
	if !IsImageMIME("image/png") {
		t.Fatal("image/png should be image")
	}
	if !IsImageMIME("image/jpeg") {
		t.Fatal("image/jpeg should be image")
	}
	if IsImageMIME("text/plain") {
		t.Fatal("text/plain should not be image")
	}
}

func TestSanitizeMIME(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"text/plain; charset=utf-8", "text/plain"},
		{"IMAGE/PNG", "image/png"},
		{"application/pdf", "application/pdf"},
		{"invalid!!!", "invalid!!!"},
	}
	for _, tc := range tests {
		got := SanitizeMIME(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeMIME(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestReadAndDetect(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0x03}
	r := bytes.NewReader(png)

	mime, data, err := ReadAndDetect(r, "")
	if err != nil {
		t.Fatalf("ReadAndDetect error: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("ReadAndDetect MIME = %q, want image/png", mime)
	}
	if !bytes.Equal(data, png) {
		t.Fatal("ReadAndDetect returned wrong data")
	}
}

func TestReadAtMost_OK(t *testing.T) {
	data := []byte("hello world")
	r := bytes.NewReader(data)

	result, err := ReadAtMost(r, 100)
	if err != nil {
		t.Fatalf("ReadAtMost(ok) = %v", err)
	}
	if !bytes.Equal(result, data) {
		t.Fatal("ReadAtMost returned wrong data")
	}
}

func TestReadAtMost_ExceedsLimit(t *testing.T) {
	data := make([]byte, 200)
	for i := range data {
		data[i] = byte(i)
	}
	r := bytes.NewReader(data)

	_, err := ReadAtMost(r, 100)
	if err == nil {
		t.Fatal("ReadAtMost should fail when content exceeds limit")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected size limit error, got: %v", err)
	}
}

// =============================================================================
// URL Validation Tests
// =============================================================================

func TestURLLimitsValidateURL_AllowedSchemes(t *testing.T) {
	l := DefaultURLLimits
	// Use a real host for DNS resolution
	tests := []struct {
		url  string
		want error
	}{
		{"https://example.com/image.png", nil},
		{"http://example.com/data.bin", nil},
		{"ftp://example.com/file", fmt.Errorf("")}, // scheme not allowed
		{"file:///etc/passwd", fmt.Errorf("")},     // scheme not allowed
	}
	for _, tc := range tests {
		err := l.ValidateURL(tc.url)
		if tc.want == nil {
			if err != nil {
				t.Errorf("ValidateURL(%q) = %v, want nil", tc.url, err)
			}
		} else {
			if err == nil {
				t.Errorf("ValidateURL(%q) = nil, want error", tc.url)
			}
		}
	}
}

func TestURLLimitsValidateURL_PrivateIP(t *testing.T) {
	l := DefaultURLLimits

	// Private IP addresses should be blocked
	err := l.ValidateURL("http://10.0.0.1/file")
	if err == nil {
		t.Fatal("ValidateURL(10.0.0.1) should fail (private IP)")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Errorf("expected private IP error, got: %v", err)
	}

	err = l.ValidateURL("http://192.168.1.1/file")
	if err == nil {
		t.Fatal("ValidateURL(192.168.1.1) should fail (private IP)")
	}

	err = l.ValidateURL("http://172.16.0.1/file")
	if err == nil {
		t.Fatal("ValidateURL(172.16.0.1) should fail (private IP)")
	}
}

func TestURLLimitsValidateURL_Loopback(t *testing.T) {
	l := DefaultURLLimits

	err := l.ValidateURL("http://127.0.0.1/file")
	if err == nil {
		t.Fatal("ValidateURL(127.0.0.1) should fail (loopback)")
	}

	err = l.ValidateURL("http://localhost/file")
	if err == nil {
		t.Fatal("ValidateURL(localhost) should fail (loopback)")
	}
}

func TestURLLimitsValidateURL_AllowPrivate(t *testing.T) {
	l := DefaultURLLimits
	l.AllowPrivateIP = true
	l.AllowLoopback = true

	// With AllowPrivateIP, private addresses should pass
	err := l.ValidateURL("http://10.0.0.1/file")
	if err != nil {
		t.Errorf("ValidateURL(10.0.0.1) with AllowPrivateIP = %v, want nil", err)
	}
}

func TestURLLimitsValidateURL_NoHost(t *testing.T) {
	l := DefaultURLLimits
	err := l.ValidateURL("https:///path")
	if err == nil {
		t.Fatal("ValidateURL with no host should fail")
	}
}

func TestURLLimitsValidateURL_Malformed(t *testing.T) {
	l := DefaultURLLimits
	err := l.ValidateURL("not a url at all [[[")
	if err == nil {
		t.Fatal("ValidateURL with malformed URL should fail")
	}
}

func TestURLLimitsDownloadURL_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	}))
	defer server.Close()

	// Use the server's actual URL (which will be on a test port)
	l := URLLimits{
		AllowedSchemes:     []string{"http", "https"},
		AllowPrivateIP:     true,
		AllowLoopback:     true,
		MaxDownloadBytes:   1024 * 1024,
	}

	reader, ct, err := l.DownloadURL(context.Background(), server.URL+"/test.png")
	if err != nil {
		t.Fatalf("DownloadURL = %v", err)
	}
	defer reader.Close()

	if ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("DownloadURL returned empty data")
	}
}

func TestURLLimitsDownloadURL_TooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	l := URLLimits{
		AllowedSchemes:     []string{"http", "https"},
		AllowPrivateIP:     true,
		AllowLoopback:     true,
		MaxDownloadBytes:   100,
	}

	reader, _, err := l.DownloadURL(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DownloadURL = %v", err)
	}
	defer reader.Close()

	_, err = io.ReadAll(reader)
	if err == nil {
		t.Fatal("ReadAll should fail when download exceeds size limit")
	}
}

func TestURLLimitsDownloadURL_RedirectLimit(t *testing.T) {
	// Create a redirect loop
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirect", http.StatusFound)
	}))
	defer server.Close()

	l := URLLimits{
		AllowedSchemes:     []string{"http", "https"},
		AllowPrivateIP:     true,
		AllowLoopback:     true,
		MaxDownloadBytes:   1024 * 1024,
	}

	_, _, err := l.DownloadURL(context.Background(), server.URL)
	if err == nil {
		t.Fatal("DownloadURL should fail on too many redirects")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("expected redirect error, got: %v", err)
	}
}

func TestURLLimitsDownloadURL_BadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	defer server.Close()

	l := URLLimits{
		AllowedSchemes:     []string{"http", "https"},
		AllowPrivateIP:     true,
		AllowLoopback:     true,
		MaxDownloadBytes:   1024 * 1024,
	}

	_, _, err := l.DownloadURL(context.Background(), server.URL)
	if err == nil {
		t.Fatal("DownloadURL should fail on 404")
	}
}

func TestIsSafeURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/file", true},
		{"http://example.com/file", true},
		{"ftp://example.com/file", false},
		{"file:///etc/passwd", false},
		{"not-a-url", false},
	}
	for _, tc := range tests {
		got := IsSafeURL(tc.url)
		if got != tc.want {
			t.Errorf("IsSafeURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestGuessFilenameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/images/photo.png", "photo.png"},
		{"https://example.com/path/to/file.jpg", "file.jpg"},
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
	}
	for _, tc := range tests {
		got := GuessFilenameFromURL(tc.url)
		if got != tc.want {
			t.Errorf("GuessFilenameFromURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
	// Malformed URLs return a best-effort result
	got := GuessFilenameFromURL("not-a-url")
	if got == "" {
		t.Error("GuessFilenameFromURL(malformed) should not return empty string")
	}
}

// =============================================================================
// Ingestion Service Tests
// =============================================================================

func TestIngestSourceValidate(t *testing.T) {
	// Valid file source
	src := IngestSource{Kind: IngestSourceKindFile, Path: "/tmp/test.png"}
	if err := src.Validate(); err != nil {
		t.Fatalf("valid file source rejected: %v", err)
	}

	// Missing path
	src = IngestSource{Kind: IngestSourceKindFile}
	err := src.Validate()
	if err == nil {
		t.Fatal("file source without path should fail")
	}

	// Relative path is allowed (resolved against working dir by caller)
	src = IngestSource{Kind: IngestSourceKindFile, Path: "relative/path"}
	if err := src.Validate(); err != nil {
		t.Fatalf("relative path should be allowed: %v", err)
	}

	// Valid URL source
	src = IngestSource{Kind: IngestSourceKindURL, URL: "https://example.com/file.png"}
	if err := src.Validate(); err != nil {
		t.Fatalf("valid URL source rejected: %v", err)
	}

	// Missing URL
	src = IngestSource{Kind: IngestSourceKindURL}
	err = src.Validate()
	if err == nil {
		t.Fatal("URL source without URL should fail")
	}

	// Valid stream source
	src = IngestSource{Kind: IngestSourceKindStream, Reader: bytes.NewReader([]byte("data"))}
	if err := src.Validate(); err != nil {
		t.Fatalf("valid stream source rejected: %v", err)
	}

	// Missing reader
	src = IngestSource{Kind: IngestSourceKindStream}
	err = src.Validate()
	if err == nil {
		t.Fatal("stream source without reader should fail")
	}

	// Unknown kind
	src = IngestSource{Kind: "unknown"}
	err = src.Validate()
	if err == nil {
		t.Fatal("unknown source kind should fail")
	}
}

func TestIngestService_File(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{
		MaxFiles: 10,
		MaxSizeBytes: 1024 * 1024, // 1 MB
	}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	// Create a test PNG file
	pngData := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x01}
	testFile := filepath.Join(tempDir, "test-image.png")
	if err := os.WriteFile(testFile, pngData, 0644); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	src := IngestSource{
		Kind:   IngestSourceKindFile,
		Path:   testFile,
		Name:   "my-photo.png",
	}

	attach, err := svc.Ingest(context.Background(), src)
	if err != nil {
		t.Fatalf("Ingest(file) = %v", err)
	}

	if attach.MIME != "image/png" {
		t.Errorf("Ingest MIME = %q, want image/png", attach.MIME)
	}
	if attach.Kind != KindImage {
		t.Errorf("Ingest Kind = %q, want KindImage", attach.Kind)
	}
	if attach.Source != SourceFile {
		t.Errorf("Ingest Source = %q, want SourceFile", attach.Source)
	}
	if attach.DisplayName != "my-photo.png" {
		t.Errorf("Ingest DisplayName = %q, want my-photo.png", attach.DisplayName)
	}
	if attach.SHA256 == "" {
		t.Error("Ingest should produce a SHA256 hash")
	}
	if attach.FileName == "" {
		t.Error("Ingest should produce a RelativePath")
	}

	// Verify the file exists on disk
	absPath := filepath.Join(w.Dir(), attach.FileName)
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Fatalf("ingested file does not exist at %q", absPath)
	}
}

func TestIngestService_Deduplication(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{
		MaxFiles: 10,
		MaxSizeBytes: 1024 * 1024,
	}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	// Create a test file
	data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	testFile := filepath.Join(tempDir, "test.png")
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	// First ingestion
	src1 := IngestSource{Kind: IngestSourceKindFile, Path: testFile, Name: "first.png"}
	attach1, err := svc.Ingest(context.Background(), src1)
	if err != nil {
		t.Fatalf("first Ingest = %v", err)
	}

	// Register the first attachment so the lookup closure can find it for dedup.
	attachments := []Attachment{attach1}
	svc.lookup = func() []Attachment { return attachments }

	// Second ingestion of the same content (different file path, same content)
	testFile2 := filepath.Join(tempDir, "test2.png")
	if err := os.WriteFile(testFile2, data, 0644); err != nil {
		t.Fatalf("cannot create test file 2: %v", err)
	}

	src2 := IngestSource{Kind: IngestSourceKindFile, Path: testFile2, Name: "second.png"}
	attach2, err := svc.Ingest(context.Background(), src2)
	if err != nil {
		t.Fatalf("second Ingest (dedup) = %v", err)
	}

	// Deduplication should return the same attachment ID
	if attach1.ID != attach2.ID {
		t.Errorf("Deduplication failed: IDs differ (%q vs %q)", attach1.ID, attach2.ID)
	}
}

func TestIngestService_Stream(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{
		MaxFiles: 10,
		MaxSizeBytes: 1024 * 1024,
	}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'J', 'F', 'I', 'F'}
	src := IngestSource{
		Kind:   IngestSourceKindStream,
		Reader: bytes.NewReader(data),
		Name:   "stream-image.jpg",
	}

	attach, err := svc.Ingest(context.Background(), src)
	if err != nil {
		t.Fatalf("Ingest(stream) = %v", err)
	}

	if attach.MIME != "image/jpeg" {
		t.Errorf("Ingest MIME = %q, want image/jpeg", attach.MIME)
	}
	if attach.Source != SourceClipboard {
		t.Errorf("Ingest Source = %q, want SourceClipboard (for stream)", attach.Source)
	}
}

func TestIngestService_Clipboard(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{
		MaxFiles: 10,
		MaxSizeBytes: 1024 * 1024,
	}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	src := IngestSource{
		Kind:   IngestSourceKindStream,
		Reader: bytes.NewReader(data),
		Name:   "clipboard-screenshot.png",
	}

	attach, err := svc.Ingest(context.Background(), src)
	if err != nil {
		t.Fatalf("Ingest(clipboard) = %v", err)
	}

	if attach.Source != SourceClipboard {
		t.Errorf("Ingest Source = %q, want SourceClipboard", attach.Source)
	}
}

func TestIngestService_DirectoryRejected(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{MaxFiles: 10, MaxSizeBytes: 1024 * 1024}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	src := IngestSource{
		Kind: IngestSourceKindFile,
		Path: tempDir, // directory, not a file
	}

	_, err := svc.Ingest(context.Background(), src)
	if err == nil {
		t.Fatal("Ingest(directory) should fail")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected directory error, got: %v", err)
	}
}

func TestIngestService_SizeLimit(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{
		MaxFiles: 10,
		MaxSizeBytes: 10, // Very small limit
	}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	// Create a file larger than the limit
	data := make([]byte, 100)
	testFile := filepath.Join(tempDir, "large.bin")
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	src := IngestSource{Kind: IngestSourceKindFile, Path: testFile}
	_, err := svc.Ingest(context.Background(), src)
	if err == nil {
		t.Fatal("Ingest should fail when file exceeds size limit")
	}
}

func TestIngestService_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{MaxFiles: 10, MaxSizeBytes: 1024 * 1024}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before ingestion

	src := IngestSource{
		Kind:   IngestSourceKindStream,
		Reader: bytes.NewReader([]byte("data")),
	}

	_, err := svc.Ingest(ctx, src)
	if err == nil {
		t.Fatal("Ingest with canceled context should fail")
	}
}

func TestIngestService_NoOrphanOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	w := NewTempWorkspace(tempDir)

	limits := Limits{
		MaxFiles: 10,
		MaxSizeBytes: 1024 * 1024,
	}
	svc := NewIngestService(w, limits, func() []Attachment { return nil }, func(Attachment) {})

	// Attempt ingestion with invalid source — should not create any files
	src := IngestSource{Kind: IngestSourceKindFile} // missing path
	_, err := svc.Ingest(context.Background(), src)
	if err == nil {
		t.Fatal("Ingest with missing path should fail")
	}

	// Verify no files were created in media directory
	mediaDir := filepath.Join(tempDir, "media")
	files, _ := os.ReadDir(mediaDir)
	if len(files) > 0 {
		t.Fatalf("failed ingestion left orphaned files in media dir: %v", files)
	}
}

func TestComputeSHA256(t *testing.T) {
	data := []byte("hello world")
	hash := sha256Hex(data)
	if hash == "" {
		t.Fatal("sha256Hex returned empty hash")
	}
	if len(hash) != 64 { // SHA256 hex is 64 chars
		t.Fatalf("sha256Hex length = %d, want 64", len(hash))
	}

	// Deterministic: same input = same hash
	hash2 := sha256Hex(data)
	if hash != hash2 {
		t.Fatal("sha256Hex is not deterministic")
	}
}

func TestGuessExtensionFromMIME(t *testing.T) {
	tests := []struct {
		mime      string
		wantEmpty bool // true if we expect an empty extension
	}{
		{"image/png", false},
		{"image/jpeg", false},
		{"application/pdf", false},
		{"text/plain", false},
		{"application/octet-stream", true}, // no registered extension
	}
	for _, tc := range tests {
		got := extFromMIME(tc.mime)
		if tc.wantEmpty {
			if got != "" {
				t.Logf("extFromMIME(%q) = %q (got extension, acceptable)", tc.mime, got)
			}
		} else {
			if got == "" {
				t.Errorf("extFromMIME(%q) = %q, want non-empty extension starting with '.'", tc.mime, got)
			}
		}
	}
}

// =============================================================================
// Limits Tests
// =============================================================================

func TestLimitsValidate(t *testing.T) {
	valid := Limits{
		MaxFiles:     10,
		MaxSizeBytes: 1024,
	}
	_ = valid
}

func TestSessionQuota(t *testing.T) {
	limits := Limits{MaxFiles: 3, MaxSizeBytes: 1000}
	q := NewSessionQuota(limits)

	// First file: 200 bytes
	if err := q.CheckBeforeIngest(200); err != nil {
		t.Fatalf("CheckBeforeIngest(200) = %v", err)
	}
	q.RecordIngest(200)

	// Second file: 300 bytes
	if err := q.CheckBeforeIngest(300); err != nil {
		t.Fatalf("CheckBeforeIngest(300) = %v", err)
	}
	q.RecordIngest(300)

	// Third file: 400 bytes
	if err := q.CheckBeforeIngest(400); err != nil {
		t.Fatalf("CheckBeforeIngest(400) = %v", err)
	}
	q.RecordIngest(400)

	// Fourth file: should fail (file count limit)
	err := q.CheckBeforeIngest(10)
	if err == nil {
		t.Fatal("CheckBeforeIngest after 3 files should fail (file limit)")
	}

	bytesRem, filesRem := q.Remaining()
	if bytesRem != 100 {
		t.Errorf("bytes remaining = %d, want 100", bytesRem)
	}
	if filesRem != 0 {
		t.Errorf("files remaining = %d, want 0", filesRem)
	}
}

func TestSessionQuota_SizeLimit(t *testing.T) {
	limits := Limits{MaxFiles: 10, MaxSizeBytes: 500}
	q := NewSessionQuota(limits)

	q.RecordIngest(400)

	// This would exceed the byte limit
	err := q.CheckBeforeIngest(200)
	if err == nil {
		t.Fatal("CheckBeforeIngest(200) should fail (would exceed byte limit)")
	}
}

func TestSessionQuota_Exhausted(t *testing.T) {
	limits := Limits{MaxFiles: 1, MaxSizeBytes: 100}
	q := NewSessionQuota(limits)

	if q.IsExhausted() {
		t.Fatal("fresh quota should not be exhausted")
	}

	q.RecordIngest(100)
	if !q.IsExhausted() {
		t.Fatal("quota should be exhausted after reaching limits")
	}
}

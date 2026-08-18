package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"squid-os/internal/version"
)

// IngestSourceKind identifies how content is provided to the IngestService.
type IngestSourceKind string

const (
	// IngestSourceKindFile reads content from a local file path.
	IngestSourceKindFile IngestSourceKind = "file"
	// IngestSourceKindURL downloads content from a remote URL.
	IngestSourceKindURL IngestSourceKind = "url"
	// IngestSourceKindStream reads content from an in-memory reader.
	IngestSourceKindStream IngestSourceKind = "stream"
)

// IngestSource describes the origin of content to be ingested.
// Exactly one of Path, URL, or Reader must be set, matching Kind.
type IngestSource struct {
	Kind   IngestSourceKind
	Path   string    // required when Kind == IngestSourceKindFile
	URL    string    // required when Kind == IngestSourceKindURL
	Reader io.Reader // required when Kind == IngestSourceKindStream
	Name   string    // optional display name override
}

// Validate checks that the source is well-formed for its kind.
func (s IngestSource) Validate() error {
	switch s.Kind {
	case IngestSourceKindFile:
		if s.Path == "" {
			return fmt.Errorf("file source requires Path")
		}
	case IngestSourceKindURL:
		if s.URL == "" {
			return fmt.Errorf("url source requires URL")
		}
	case IngestSourceKindStream:
		if s.Reader == nil {
			return fmt.Errorf("stream source requires Reader")
		}
	default:
		return fmt.Errorf("unknown ingest source kind: %q", s.Kind)
	}
	return nil
}

// Limits controls size and type restrictions for ingestion.
type Limits struct {
	MaxSizeBytes int64
	MaxFiles     int
	AllowedMIME  []string // empty means all allowed
}

// DefaultLimits is the standard ingestion policy.
var DefaultLimits = Limits{
	MaxSizeBytes: 10 << 20, // 10 MiB
	MaxFiles:     100,
}

// IngestService reads content from a source, detects MIME type, stores the
// file in the workspace, and registers the attachment with the session.
type IngestService struct {
	workspace Workspace
	limits    Limits
	// lookup returns the session's current attachment slice (for dedup).
	lookup func() []Attachment
	// register is called after each attachment is created.
	register func(Attachment)
}

// NewIngestService creates a service bound to a workspace.
// lookup returns the session's current attachments (for SHA256 dedup).
// register is called to add each new attachment to the session.
func NewIngestService(ws Workspace, limits Limits, lookup func() []Attachment, register func(Attachment)) *IngestService {
	return &IngestService{workspace: ws, limits: limits, lookup: lookup, register: register}
}

// Ingest reads content from the source, stores it in the workspace, and
// registers the attachment. The source kind determines how content is read:
//   - file:   reads from disk
//   - url:    downloads from network
//   - stream: reads from in-memory reader
//
// The display name is derived from the source kind: file-, url-, or paste-.
func (s *IngestService) Ingest(ctx context.Context, src IngestSource) (Attachment, error) {
	if err := src.Validate(); err != nil {
		return Attachment{}, err
	}

	// 1. Read content based on kind
	var content []byte
	var ext string

	switch src.Kind {
	case IngestSourceKindFile:
		var err error
		content, err = s.readFile(ctx, src)
		if err != nil {
			return Attachment{}, err
		}
		ext = strings.TrimPrefix(filepath.Ext(src.Path), ".")

	case IngestSourceKindURL:
		var err error
		content, ext, err = s.download(ctx, src)
		if err != nil {
			return Attachment{}, err
		}

	case IngestSourceKindStream:
		var err error
		content, err = s.readStream(ctx, src)
		if err != nil {
			return Attachment{}, err
		}
		ext = filepath.Ext(src.Name)
		ext = strings.TrimPrefix(ext, ".")
	}

	// 2. Derive display name from source kind
	displayName := s.deriveDisplayName(src)

	// 3. Determine the raw source reference (URL or file path)
	var sourceRef string
	switch src.Kind {
	case IngestSourceKindURL:
		sourceRef = src.URL
	case IngestSourceKindFile:
		sourceRef = src.Path
	}

	// 4. Store and register
	return s.storeContent(ctx, content, displayName, src.Kind, ext, sourceRef)
}

// readFile reads content from a local file.
func (s *IngestService) readFile(ctx context.Context, src IngestSource) ([]byte, error) {
	info, err := os.Stat(src.Path)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("cannot ingest a directory")
	}
	if info.Size() > s.limits.MaxSizeBytes {
		return nil, fmt.Errorf("file size %d exceeds limit %d", info.Size(), s.limits.MaxSizeBytes)
	}
	content, err := os.ReadFile(src.Path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return content, nil
}

// download fetches content from a remote URL.
func (s *IngestService) download(ctx context.Context, src IngestSource) ([]byte, string, error) {
	u, err := url.Parse(src.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, "", fmt.Errorf("invalid URL: %q", src.URL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "squid-os/"+version.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	content, err := ReadAtMost(resp.Body, s.limits.MaxSizeBytes)
	if err != nil {
		return nil, "", fmt.Errorf("download: %w", err)
	}

	ext := extFromMIME(resp.Header.Get("Content-Type"))
	if ext == "" {
		ext = extFromMIME(MIMEForPath(src.URL))
	}
	return content, ext, nil
}

// readStream reads content from an in-memory reader.
func (s *IngestService) readStream(ctx context.Context, src IngestSource) ([]byte, error) {
	content, err := ReadAtMost(src.Reader, s.limits.MaxSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	return content, nil
}

// deriveDisplayName generates a display name based on the source kind.
// The prefix matches the kind: file-, url-, paste-.
func (s *IngestService) deriveDisplayName(src IngestSource) string {
	if src.Name != "" {
		return src.Name
	}

	switch src.Kind {
	case IngestSourceKindFile:
		return filepath.Base(src.Path)
	case IngestSourceKindURL:
		return GuessFilenameFromURL(src.URL)
	case IngestSourceKindStream:
		return fmt.Sprintf("paste-%d.bin", time.Now().Unix())
	default:
		return fmt.Sprintf("ingest-%d.bin", time.Now().Unix())
	}
}

// storeContent writes the content to the workspace media directory and
// registers the attachment.
func (s *IngestService) storeContent(ctx context.Context, content []byte, displayName string, kind IngestSourceKind, extHint string, sourceRef string) (Attachment, error) {
	if err := IngestInContext(ctx); err != nil {
		return Attachment{}, err
	}

	// Detect MIME from content
	mimeType := DetectMIMEFromBytes(content, extHint)

	// Validate MIME against allowed list
	if len(s.limits.AllowedMIME) > 0 {
		allowed := false
		for _, m := range s.limits.AllowedMIME {
			if m == mimeType {
				allowed = true
				break
			}
		}
		if !allowed {
			return Attachment{}, fmt.Errorf("MIME type %q not in allowed list", mimeType)
		}
	}

	// Compute hash
	hash := sha256Hex(content)

	// Dedup: check if this content already exists in the session
	if existing, found := ResolveRefBySHA256(s.lookup(), hash); found {
		return existing, nil
	}

	// Generate safe filename: <prefix>-<hash>.<ext>
	fileExt := extFromMIME(mimeType)
	if fileExt == "" {
		fileExt = extFromMIME(MIMEForPath(displayName))
	}
	if fileExt == "" {
		fileExt = "bin"
	}
	prefix := prefixFromKind(kind)
	filename := fmt.Sprintf("%s-%s.%s", prefix, hash[:16], fileExt)

	// Write to media directory
	mediaDir := s.workspace.Dir()
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		return Attachment{}, fmt.Errorf("create media dir: %w", err)
	}

	finalPath := filepath.Join(mediaDir, filename)
	if _, err := os.Stat(finalPath); err == nil {
		// Collision: append hash suffix
		filename = fmt.Sprintf("%s-%s.%s", hash[:16], hash[16:32], fileExt)
		finalPath = filepath.Join(mediaDir, filename)
	}

	if err := os.WriteFile(finalPath, content, 0644); err != nil {
		return Attachment{}, fmt.Errorf("write file: %w", err)
	}

	// Create and register attachment
	attachment := NewAttachment(mediaDir, filename, sourceFromKind(kind), displayName)
	attachment.MIME = mimeType
	attachment.SHA256 = hash
	attachment.Size = int64(len(content))
	attachment.SourceRef = sourceRef

	// Register with the session
	if s.register != nil {
		s.register(attachment)
	}

	return attachment, nil
}

// prefixFromKind maps an ingest source kind to a filename prefix.
func prefixFromKind(kind IngestSourceKind) string {
	switch kind {
	case IngestSourceKindFile:
		return "file"
	case IngestSourceKindURL:
		return "url"
	case IngestSourceKindStream:
		return "paste"
	default:
		return "ingest"
	}
}

// sourceFromKind maps an ingest source kind to a source.
func sourceFromKind(kind IngestSourceKind) Source {
	switch kind {
	case IngestSourceKindFile:
		return SourceFile
	case IngestSourceKindURL:
		return SourceURL
	case IngestSourceKindStream:
		return SourceClipboard
	default:
		return SourceFile
	}
}

// sha256Hex returns the hex-encoded SHA256 hash of the content.
func sha256Hex(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// extFromMIME maps a MIME type to a file extension.
func extFromMIME(mimeType string) string {
	switch mimeType {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/bmp":
		return "bmp"
	case "image/svg+xml":
		return "svg"
	case "application/pdf":
		return "pdf"
	case "text/plain", "text/markdown":
		return "txt"
	case "text/html":
		return "html"
	case "text/css":
		return "css"
	case "text/javascript", "application/javascript":
		return "js"
	case "application/json":
		return "json"
	case "application/xml", "text/xml":
		return "xml"
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	default:
		return ""
	}
}

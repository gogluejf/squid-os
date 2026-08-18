package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kind classifies an attachment by its primary modality.
type Kind string

const (
	KindImage Kind = "image"
	KindPDF   Kind = "pdf"
	KindText  Kind = "text"
	KindAudio Kind = "audio"
	KindVideo Kind = "video"
	KindFile  Kind = "file" // generic document
)

// Source identifies how an attachment entered the session.
type Source string

const (
	SourceFile      Source = "file"
	SourceURL       Source = "url"
	SourceClipboard Source = "clipboard"
	SourceTool      Source = "tool"
	SourceMigrated  Source = "migrated" // converted from legacy ImagePath
)

// Attachment is the persisted metadata record for one session-local media file.
// FileName is a bare filename (e.g. "abc123.png"). The physical location is
// workspace.MediaDir()/media/<FileName> — the "media/" subdir is an
// implementation detail owned by the workspace, not the attachment.
type Attachment struct {
	ID           string `json:"id"`
	FileName     string `json:"file_name"` // bare filename, e.g. "abc123.png"
	DisplayName  string `json:"display_name"`
	MIME         string `json:"mime"`
	Kind         Kind   `json:"kind"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	Source       Source `json:"source"`
	DerivedFrom  string `json:"derived_from,omitempty"` // parent attachment ID if derived
	SourceRef    string `json:"source_ref,omitempty"`   // raw origin: URL for url source, abs path for file source
	CreatedAt    string `json:"created_at"`
}

// CanonicalRef returns the canonical @file reference text for this attachment.
// Uses the file name so the reference is human-readable: the source prefix
// and extension tell the user what the file is at a glance.
func (a Attachment) CanonicalRef() string {
	return "@file:" + a.FileName
}

// NewAttachment creates a populated Attachment record for a newly ingested file.
func NewAttachment(mediaDir, fileName string, source Source, displayHint string) Attachment {
	now := time.Now().UTC().Format(time.RFC3339)

	info, _ := os.Stat(filepath.Join(mediaDir, fileName))
	var size int64
	var hash string
	if info != nil {
		size = info.Size()
		hash = fileSHA256(filepath.Join(mediaDir, fileName))
	}

	mime := MIMEForPath(fileName)
	kind := KindForMIME(mime)
	displayName := fileName
	if displayHint != "" && displayHint != fileName {
		displayName = displayHint
	}

	return Attachment{
		ID:           uuid.New().String(),
		FileName:     fileName,
		DisplayName:  displayName,
		MIME:         mime,
		Kind:         kind,
		Size:         size,
		SHA256:       hash,
		Source:       source,
		CreatedAt:    now,
	}
}

// Validate checks that the attachment record is well-formed.
func (a Attachment) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("attachment ID is empty")
	}
	if a.FileName == "" {
		return fmt.Errorf("attachment FileName is empty")
	}
	// FileName must be a bare filename — no path separators.
	if strings.Contains(a.FileName, "/") || strings.Contains(a.FileName, "\\") {
		return fmt.Errorf("attachment FileName must be a bare filename: %q", a.FileName)
	}
	if filepath.Base(a.FileName) != a.FileName {
		return fmt.Errorf("attachment FileName must be a bare filename: %q", a.FileName)
	}
	if a.MIME == "" {
		return fmt.Errorf("attachment MIME is empty")
	}
	return nil
}

// fileSHA256 computes the SHA256 hex digest of a file.
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MarshalJSON implements custom JSON marshaling for Kind to ensure empty
// values serialize as empty strings, not omitted.
func (k Kind) MarshalJSON() ([]byte, error) {
	s := string(k)
	return json.Marshal(s)
}

// UnmarshalJSON implements custom JSON unmarshaling for Kind.
func (k *Kind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*k = Kind(s)
	return nil
}

// MarshalJSON implements custom JSON marshaling for Source.
func (s Source) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements custom JSON unmarshaling for Source.
func (s *Source) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*s = Source(str)
	return nil
}

// ResolveRef looks up an attachment by file name or by ID.
// File name is the primary lookup key (matches CanonicalRef); ID is a fallback
// for legacy or payload-level references.
func ResolveRef(attachments []Attachment, ref string) (Attachment, bool) {
	// Primary: match by file name
	for _, a := range attachments {
		if a.FileName == ref {
			return a, true
		}
	}
	// Fallback: match by ID (legacy / payload-level refs)
	for _, a := range attachments {
		if a.ID == ref {
			return a, true
		}
	}
	return Attachment{}, false
}

// ResolveRefBySHA256 looks up an attachment by its content hash.
func ResolveRefBySHA256(attachments []Attachment, hash string) (Attachment, bool) {
	for _, a := range attachments {
		if a.SHA256 == hash {
			return a, true
		}
	}
	return Attachment{}, false
}

// DeduplicateByHash returns the slice with duplicate entries (same SHA256)
// removed, keeping the first occurrence.
func DeduplicateByHash(attachments []Attachment) []Attachment {
	seen := make(map[string]bool)
	out := make([]Attachment, 0, len(attachments))
	for _, a := range attachments {
		if a.SHA256 != "" && seen[a.SHA256] {
			continue
		}
		if a.SHA256 != "" {
			seen[a.SHA256] = true
		}
		out = append(out, a)
	}
	return out
}

// IsMediaKind returns true for kinds that represent direct provider input (images, PDFs).
func IsMediaKind(k Kind) bool {
	switch k {
	case KindImage, KindPDF:
		return true
	default:
		return false
	}
}

// IngestInContext is a context-aware ingest guard — no-op for now but
// provides the hook for future cancellation during long downloads.
func IngestInContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MIMEForPath returns the MIME type hint for a file based on its extension.
// This is a lightweight hint — content-based detection takes precedence.
func MIMEForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg", ".svgz":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".text", ".md", ".markdown", ".go", ".py", ".js", ".ts", ".json", ".xml", ".yaml", ".yml":
		return "text/plain"
	case ".mp3", ".wav", ".ogg", ".flac":
		return "audio/mpeg"
	case ".mp4", ".webm", ".mov":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// KindForMIME classifies a MIME type into an attachment Kind.
func KindForMIME(mime string) Kind {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return KindImage
	case mime == "application/pdf":
		return KindPDF
	case strings.HasPrefix(mime, "text/"):
		return KindText
	case strings.HasPrefix(mime, "audio/"):
		return KindAudio
	case strings.HasPrefix(mime, "video/"):
		return KindVideo
	default:
		return KindFile
	}
}

// Workspace manages the physical location of a session's media files.
// It abstracts over persistent (saved session) and temporary (unsaved/incognito)
// storage so callers never need to know where media physically lives.
//
// The workspace is purely a storage-location concern. The attachment
// collection (metadata) lives on the session (Doc.Attachments). Resolution
// is done by the caller: filepath.Join(ws.Dir(), a.FileName).
type Workspace interface {
	// Dir returns the absolute path to the directory where media files
	// are stored. Files live at Dir()/<FileName>.
	Dir() string
}

// PersistentWorkspace is a Workspace backed by the session's media directory.
// Used for saved sessions where media lives alongside chat.json.
type PersistentWorkspace struct {
	mediaDir string
}

// NewPersistentWorkspace creates a workspace with files stored in
// sessionDir/media/.
func NewPersistentWorkspace(sessionDir string) Workspace {
	return &PersistentWorkspace{mediaDir: filepath.Join(sessionDir, "media")}
}

func (w *PersistentWorkspace) Dir() string {
	return w.mediaDir
}

// TempWorkspace is a Workspace backed by a temporary directory.
// Used for unsaved and incognito sessions. Files are not persisted until
// the session is explicitly saved.
type TempWorkspace struct {
	mediaDir string
}

// NewTempWorkspace creates a workspace with files stored in tempDir/media/.
// The directory is created if it doesn't exist.
func NewTempWorkspace(tempDir string) Workspace {
	mediaDir := filepath.Join(tempDir, "media")
	_ = os.MkdirAll(mediaDir, 0755)
	return &TempWorkspace{mediaDir: mediaDir}
}

func (w *TempWorkspace) Dir() string {
	return w.mediaDir
}

// IsTraversalSafe verifies that a file name is a bare filename with no
// path separators. This is a defense-in-depth check for all attachment names.
func IsTraversalSafe(fileName string) bool {
	return fileName != "" && fileName != "." && fileName != ".." &&
		!strings.Contains(fileName, "/") && !strings.Contains(fileName, "\\") &&
		filepath.Base(fileName) == fileName
}

// ValidateAttachmentName ensures the attachment's file name is safe.
func ValidateAttachmentName(fileName string) error {
	if !IsTraversalSafe(fileName) {
		return fmt.Errorf("unsafe attachment file name: %q", fileName)
	}
	return nil
}

package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"squid-os/internal/log"
	"squid-os/internal/media"
	"squid-os/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"golang.design/x/clipboard"
)

// DefaultLargeTextBytes is the default threshold (1 KiB) above which
// pasted text is stored as a text attachment rather than inserted directly.
const DefaultLargeTextBytes = 1024

// ClipboardPayload represents the contents read from the system clipboard.
type ClipboardPayload struct {
	Text  string   // plain text content
	Files []string // file paths on macOS (clipboard can hold file URLs)
	Image []byte   // image bytes if clipboard contains an image
	MIME  string   // MIME type of image content
}

// readClipboard reads the clipboard contents using a fallback chain that
// covers macOS, X11, Wayland, and WSL2.
//
// Priority:
//  1. atotto/clipboard (macOS pbpaste, X11 xclip/xsel, Windows)
//  2. wl-clipboard (Wayland)
//  3. xclip (fallback for WSL2 with X11 forwarding)
//
// Returns the text and an error if all methods fail.
func readClipboard() (string, error) {
	// golang.design/x/clipboard requires Init() on first use.
	// It supports X11, Wayland, macOS, Windows — no external tools needed.
	if err := clipboard.Init(); err != nil {

		return "", fmt.Errorf("clipboard init: %w", err)
	}
	data := clipboard.Read(clipboard.FmtText)

	if data == nil {
		return "", nil // empty clipboard is not an error
	}
	return string(data), nil
}

func logPaste(label, text string, err error) {
	log.LogPaste(label, text, err)
}

// readClipboardPayload reads clipboard contents and attempts to detect
// images and file lists in addition to plain text.
func readClipboardPayload() ClipboardPayload {
	text, _ := readClipboard()

	// Also try to read image data from clipboard.
	// FmtImage maps to the "image/png" X11 target only. Many apps (browsers,
	// Gimp, etc.) put images on the clipboard as JPEG, BMP, TIFF, etc.
	// We must enumerate available formats and read the first real image.
	var imageData []byte
	var imageMIME string
	if imgData := clipboard.Read(clipboard.FmtImage); len(imgData) > 4 && isImageMagic(imgData) {
		// Fast path: PNG target has valid data.
		imageData = imgData
		imageMIME = detectImageMIME(imgData)
		logPaste("clipboard.ReadImage", fmt.Sprintf("%d bytes %s", len(imgData), imageMIME), nil)
	} else {
		// FmtImage (image/png) is empty or garbage — try other image targets.
		for _, f := range clipboard.Formats() {
			mime := f.MIME()
			if mime == "" || len(mime) < 6 || mime[:6] != "image/" {
				continue
			}
			data := clipboard.Read(f)
			if len(data) > 4 && isImageMagic(data) {
				imageData = data
				imageMIME = detectImageMIME(data)
				logPaste("clipboard.ReadImage", fmt.Sprintf("%d bytes %s (target %q)", len(data), imageMIME, mime), nil)
				break
			}
		}
		if imageData == nil {
			logPaste("clipboard.ReadImage", "no valid image found in any target", nil)
		}
	}

	// Check for macOS-style file URLs in clipboard text
	var files []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "file://") {
			path := strings.TrimPrefix(line, "file://")
			if path != "" && fileExists(path) {
				files = append(files, path)
			}
		}
	}

	// If we have file URLs, strip them from the text
	if len(files) > 0 {
		var textLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "file://") {
				textLines = append(textLines, line)
			}
		}
		text = strings.Join(textLines, "\n")
	}

	return ClipboardPayload{
		Text:  text,
		Files: files,
		Image: imageData,
		MIME:  imageMIME,
	}
}

// fileExists checks if a file path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// isImageMagic checks whether the byte slice starts with a known image magic
// number. Used to reject truncated or garbage clipboard reads.
func isImageMagic(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch {
	case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e && data[3] == 0x47:
		return true // PNG
	case data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return true // JPEG
	case data[0] == 'G' && data[1] == 'I' && data[2] == 'F':
		return true // GIF
	case data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F':
		return true // WebP (RIFF container)
	case data[0] == 0x42 && data[1] == 0x4d:
		return true // BMP
	}
	return false
}

// detectImageMIME returns the MIME type from magic bytes.
func detectImageMIME(data []byte) string {
	if len(data) < 4 {
		return "image/png"
	}
	switch {
	case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e && data[3] == 0x47:
		return "image/png"
	case data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case data[0] == 'G' && data[1] == 'I' && data[2] == 'F':
		return "image/gif"
	case data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F':
		return "image/webp"
	case data[0] == 0x42 && data[1] == 0x4d:
		return "image/bmp"
	}
	return "image/png"
}

// extFromMIME returns a file extension (with dot) for an image MIME type.
func extFromMIME(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ".png"
	}
}

// pasteIngestResult groups the refs and notification from a paste operation.
type pasteIngestResult struct {
	refs        []string
	notifyLevel ui.NotificationLevel
	notifyMsg   string
}

// ingestPasteItem ingests a single clipboard item (image bytes, file path, or
// large text) through the IngestService and returns the attachment reference.
func ingestPasteItem(svc *media.IngestService, kind string, name string, content interface{}) (string, error) {
	var src media.IngestSource

	switch v := content.(type) {
	case []byte:
		// Image or text-as-bytes: ingest via stream
		src = media.IngestSource{
			Kind:   media.IngestSourceKindStream,
			Name:   name,
			Reader: bytes.NewReader(v),
		}
	case string:
		// File path: ingest directly
		absPath, err := filepath.Abs(v)
		if err != nil {
			return "", fmt.Errorf("invalid path: %w", err)
		}
		src = media.IngestSource{
			Kind: media.IngestSourceKindFile,
			Path: absPath,
			Name: name,
		}
	default:
		return "", fmt.Errorf("unsupported paste content type")
	}

	attach, err := svc.Ingest(context.Background(), src)
	if err != nil {
		return "", err
	}
	return attach.CanonicalRef(), nil
}

// handlePaste processes a paste operation: reads clipboard, checks for
// large text, and either inserts text directly or creates attachment(s).
func (m *Model) handlePaste() (Model, tea.Cmd) {
	payload := readClipboardPayload()
	workspace := m.session.EnsureWorkspace()
	svc := media.NewIngestService(workspace, media.DefaultLimits,
		func() []media.Attachment { return m.session.Doc.Attachments },
		func(a media.Attachment) { m.session.AddAttachment(a) },
	)

	// 1) Clipboard files (e.g. macOS file URLs)
	if len(payload.Files) > 0 {
		res := m.ingestMultiple(svc, payload.Files, func(fp string) (string, string) {
			return fp, filepath.Base(fp)
		})
		return m.finishPaste(res)
	}

	// 2) Clipboard image
	if len(payload.Image) > 0 {
		name := "pasted-image" + extFromMIME(payload.MIME)
		ref, err := ingestPasteItem(svc, "image", name, payload.Image)
		if err != nil {
			m.setNotification(ui.NotificationError, "Paste failed: "+err.Error())
			return *m, nil
		}
		res := pasteIngestResult{refs: []string{ref}}
		return m.finishPaste(res)
	}

	// 3) Text — large or normal
	text := payload.Text
	if text == "" {
		return *m, nil
	}

	threshold := m.settings.PasteConfig.LargeTextBytes
	if threshold <= 0 {
		threshold = DefaultLargeTextBytes
	}

	if len([]byte(text)) > threshold {
		// Large text: ingest as attachment via stream
		ref, err := ingestPasteItem(svc, "text", "pasted-text.txt", []byte(text))
		if err != nil {
			m.setNotification(ui.NotificationError, "Paste failed: "+err.Error())
			return *m, nil
		}
		sizeKB := len([]byte(text)) / 1024
		thresholdKB := threshold / 1024
		mediaDir := workspace.Dir()
		res := pasteIngestResult{
			refs:        []string{ref},
			notifyLevel: ui.NotificationInfo,
			notifyMsg:   fmt.Sprintf("pasted %d KiB text as attachment (threshold: %d KiB)  ·  %s", sizeKB, thresholdKB, mediaDir),
		}
		return m.finishPaste(res)
	}

	// Normal text: insert directly
	m.textarea.InsertString(text)
	m.autoSizeTextarea()
	return *m, nil
}

// ingestMultiple processes a slice of file paths through the IngestService.
func (m *Model) ingestMultiple(svc *media.IngestService, filePaths []string, toItem func(string) (path, name string)) pasteIngestResult {
	var refs []string
	for _, fp := range filePaths {
		path, name := toItem(fp)
		ref, err := ingestPasteItem(svc, "file", name, path)
		if err != nil {
			continue
		}
		refs = append(refs, ref)
	}

	if len(refs) == 0 {
		return pasteIngestResult{
			notifyLevel: ui.NotificationWarning,
			notifyMsg:   "clipboard files could not be ingested",
		}
	}

	mediaDir := m.session.Workspace.Dir()
	base := filepath.Base(filePaths[0])
	return pasteIngestResult{
		refs:        refs,
		notifyLevel: ui.NotificationInfo,
		notifyMsg:   fmt.Sprintf("pasted %s from clipboard  ·  %s", base, mediaDir),
	}
}

// finishPaste inserts refs into the textarea and shows a notification.
func (m *Model) finishPaste(res pasteIngestResult) (Model, tea.Cmd) {
	if len(res.refs) == 0 {
		if res.notifyMsg != "" {
			m.setNotification(res.notifyLevel, res.notifyMsg)
		}
		return *m, nil
	}

	insertText := strings.Join(res.refs, " ") + " "
	m.textarea.InsertString(insertText)
	m.autoSizeTextarea()

	if res.notifyMsg != "" {
		m.setNotification(res.notifyLevel, res.notifyMsg)
	}
	return *m, nil
}

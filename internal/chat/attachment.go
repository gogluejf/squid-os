package chat

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"squid-os/internal/media"

	goai_provider "github.com/zendev-sh/goai/provider"
)

// resolveAttachmentToPart converts a single media.Attachment into a GoAI Part
// based on its kind. Images become PartImage, documents become PartFile,
// and text attachments become bounded inline PartText with source markers.
func resolveAttachmentToPart(a media.Attachment, baseDir string) (goai_provider.Part, error) {
	absPath := filepath.Join(baseDir, a.FileName)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return goai_provider.Part{}, fmt.Errorf("read file: %w", err)
	}

	switch a.Kind {
	case media.KindImage:
		return resolveImagePart(a, data), nil
	case media.KindPDF, media.KindFile:
		return resolveFilePart(a, data), nil
	case media.KindText:
		return resolveTextPart(a, data), nil
	default:
		// Unknown kinds default to file part with metadata.
		return resolveFilePart(a, data), nil
	}
}

// resolveImagePart creates a PartImage with a data URI.
func resolveImagePart(a media.Attachment, data []byte) goai_provider.Part {
	encoded := base64.StdEncoding.EncodeToString(data)
	dataURI := fmt.Sprintf("data:%s;base64,%s", a.MIME, encoded)
	return goai_provider.Part{
		Type:      goai_provider.PartImage,
		URL:       dataURI,
		MediaType: a.MIME,
		Detail:    "auto",
	}
}

// resolveFilePart creates a PartFile with base64 data in the URL field
// and MIME/filename metadata. GoAI adapters use URL for file_data.
func resolveFilePart(a media.Attachment, data []byte) goai_provider.Part {
	encoded := base64.StdEncoding.EncodeToString(data)
	dataURI := fmt.Sprintf("data:%s;base64,%s", a.MIME, encoded)
	return goai_provider.Part{
		Type:      goai_provider.PartFile,
		URL:       dataURI,
		MediaType: a.MIME,
		Filename:  a.DisplayName,
	}
}

// resolveTextPart creates a bounded PartText with explicit source and
// truncation markers. Text content is truncated to a reasonable inline
// limit to prevent overwhelming the context window.
const maxInlineTextBytes = 32 * 1024 // 32 KiB

func resolveTextPart(a media.Attachment, data []byte) goai_provider.Part {
	text := string(data)
	truncated := false
	if len(data) > maxInlineTextBytes {
		text = string(data[:maxInlineTextBytes])
		truncated = true
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("\n--- inline: %s (%s) ---\n", a.DisplayName, a.MIME))
	builder.WriteString(text)
	if truncated {
		builder.WriteString(fmt.Sprintf("\n--- [truncated: %d bytes omitted] ---\n", len(data)-maxInlineTextBytes))
	}
	builder.WriteString("--- end inline ---\n")

	return goai_provider.Part{
		Type: goai_provider.PartText,
		Text: builder.String(),
	}
}

// EstimateAttachmentTokens returns a conservative token estimate for an
// attachment based on its kind and size. It does not require the GoAI Part
// to be built, so it can be called at message-creation time.
//
// Estimates are intentionally conservative and documented:
//   - Images: 1 token per 4 bytes (covers tile-based billing).
//   - PDFs/files: 1 token per 4 bytes (base64 overhead handled by provider).
//   - Text: standard string token approximation on the inline content.
//   - Unknown: 1 token per 4 bytes (same as images/files).
//
// These estimates are stored per-ref in AttachmentRef.Tokens and summed
// by the lifetime tally.
func EstimateAttachmentTokens(a media.Attachment) int {
	switch a.Kind {
	case media.KindImage, media.KindPDF, media.KindFile, media.KindAudio, media.KindVideo:
		// Conservative baseline: ~1 token per 4 bytes.
		// OpenAI charges ~256 tokens per 512×512 tile, which is roughly
		// proportional to file size for typical images.
		return int(a.Size) / 4
	case media.KindText:
		// Text attachments use standard approximation.
		// Bounded by maxInlineTextBytes when resolved to a Part.
		if a.Size > maxInlineTextBytes {
			return CountTokensApproxInt(int(maxInlineTextBytes))
		}
		return CountTokensApproxInt(int(a.Size))
	default:
		// Unknown kinds: use the same conservative estimate as images/files.
		return int(a.Size) / 4
	}
}

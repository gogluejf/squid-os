package media

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DetectMIME determines the MIME type of content by reading up to 512 bytes
// from the reader. If the reader implements io.Seeker, it resets the position
// after reading so the content can be consumed again.
//
// The extensionHint is used only as a fallback when content detection yields
// application/octet-stream. Content detection always takes precedence.
func DetectMIME(r io.Reader, extensionHint string) string {
	var buf bytes.Buffer
	tee := io.TeeReader(r, &buf)

	// Read up to 512 bytes for detection
	data := make([]byte, 512)
	n, err := io.ReadFull(tee, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		// If we can't even read a prefix, fall back to extension
		if extensionHint != "" {
			return mime.TypeByExtension(extensionHint)
		}
		return "application/octet-stream"
	}

	if n > 0 {
		detected := http.DetectContentType(data[:n])
		// http.DetectContentType returns application/octet-stream when
		// it can't determine a more specific type. In that case, use
		// the extension hint if available.
		if detected == "application/octet-stream" && extensionHint != "" {
			hinted := mime.TypeByExtension(extensionHint)
			if hinted != "" && hinted != "application/octet-stream" {
				return hinted
			}
		}
		if detected != "application/octet-stream" {
			return detected
		}
	}

	// Final fallback: extension hint
	if extensionHint != "" {
		hinted := mime.TypeByExtension(extensionHint)
		if hinted != "" && hinted != "application/octet-stream" {
			return hinted
		}
	}

	return "application/octet-stream"
}

// DetectMIMEFromBytes determines MIME type from a byte slice.
func DetectMIMEFromBytes(data []byte, extensionHint string) string {
	if len(data) == 0 {
		if extensionHint != "" {
			return mime.TypeByExtension(extensionHint)
		}
		return "application/octet-stream"
	}

	detected := http.DetectContentType(data)
	if detected == "application/octet-stream" && extensionHint != "" {
		hinted := mime.TypeByExtension(extensionHint)
		if hinted != "" && hinted != "application/octet-stream" {
			return hinted
		}
	}
	if detected != "application/octet-stream" {
		return detected
	}

	if extensionHint != "" {
		hinted := mime.TypeByExtension(extensionHint)
		if hinted != "" && hinted != "application/octet-stream" {
			return hinted
		}
	}

	return "application/octet-stream"
}

// PeekMIME reads at most 512 bytes from the reader to detect MIME type
// and returns a reader that yields the peeked bytes followed by the
// remaining content. This allows single-pass MIME detection without
// rewinding.
func PeekMIME(r io.Reader, extensionHint string) (string, io.Reader, error) {
	var peeked bytes.Buffer
	limiter := &io.LimitedReader{R: r, N: 512}
	n, err := peeked.ReadFrom(limiter)

	var detected string
	if n > 0 {
		detected = http.DetectContentType(peeked.Bytes())
		if detected == "application/octet-stream" && extensionHint != "" {
			hinted := mime.TypeByExtension(extensionHint)
			if hinted != "" && hinted != "application/octet-stream" {
				detected = hinted
			}
		}
	}

	if detected == "" {
		if extensionHint != "" {
			detected = mime.TypeByExtension(extensionHint)
		}
		if detected == "" || detected == "application/octet-stream" {
			detected = "application/octet-stream"
		}
	}

	// Combine peeked bytes with remaining content
	combined := io.MultiReader(&peeked, r)
	return detected, combined, err
}

// IsTextMIME returns true if the MIME type represents text content.
func IsTextMIME(mime string) bool {
	return strings.HasPrefix(mime, "text/") ||
		mime == "application/json" ||
		mime == "application/xml" ||
		mime == "application/javascript" ||
		mime == "application/typescript"
}

// IsImageMIME returns true if the MIME type represents an image.
func IsImageMIME(mime string) bool {
	return strings.HasPrefix(mime, "image/")
}

// IsPDFMIME returns true if the MIME type represents a PDF.
func IsPDFMIME(mime string) bool {
	return mime == "application/pdf"
}

// ReadAndDetect reads all content from the reader while detecting MIME type
// from the first 512 bytes. Returns the MIME type and full content.
func ReadAndDetect(r io.Reader, extensionHint string) (string, []byte, error) {
	var buf bytes.Buffer
	tee := io.TeeReader(r, &buf)

	// Read first 512 bytes for detection
	data := make([]byte, 512)
	n, _ := io.ReadFull(tee, data)
	var detected string
	if n > 0 {
		detected = http.DetectContentType(data[:n])
	}

	// Read rest of content
	if _, err := io.Copy(&buf, tee); err != nil {
		return "", nil, err
	}

	content := buf.Bytes()

	// Refine detection with full content if needed
	if detected == "application/octet-stream" && len(content) > 0 {
		// Re-check with full content for edge cases
		fullDetect := http.DetectContentType(content)
		if fullDetect != "application/octet-stream" {
			detected = fullDetect
		}
	}

	// Apply extension hint as final fallback
	if detected == "application/octet-stream" && extensionHint != "" {
		hinted := mime.TypeByExtension(extensionHint)
		if hinted != "" && hinted != "application/octet-stream" {
			detected = hinted
		}
	}

	return detected, content, nil
}

// SanitizeMIME ensures the MIME type is a safe, well-formed value.
// Strips parameters and normalizes to lowercase.
func SanitizeMIME(raw string) string {
	m, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	return strings.ToLower(m)
}

// ReadAtMost reads up to maxSize bytes from the reader and returns them.
// If the reader has more data than maxSize, it returns an error indicating
// the content was truncated.
func ReadAtMost(r io.Reader, maxSize int64) ([]byte, error) {
	lr := &io.LimitedReader{R: r, N: maxSize + 1}
	buf, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxSize {
		return nil, ErrSizeLimitExceeded{Limit: maxSize, Actual: int64(len(buf))}
	}
	return buf, nil
}

// ErrSizeLimitExceeded is returned when content exceeds the configured size limit.
type ErrSizeLimitExceeded struct {
	Limit  int64
	Actual int64
}

func (e ErrSizeLimitExceeded) Error() string {
	return "content size exceeds limit"
}

// isTextSnippet checks if the given bytes appear to be text content
// by looking for a high ratio of printable characters in the first
// portion of the data.
func isTextSnippet(data []byte) bool {
	const sampleSize = 256
	n := len(data)
	if n > sampleSize {
		n = sampleSize
	}

	var printable, total int
	scanner := bufio.NewScanner(bytes.NewReader(data[:n]))
	scanner.Split(bufio.ScanBytes)
	for scanner.Scan() {
		b := scanner.Bytes()[0]
		total++
		if b >= 0x20 && b <= 0x7e || b == '\t' || b == '\n' || b == '\r' {
			printable++
		} else if b >= 0x80 {
			// Could be UTF-8 multi-byte, count as printable
			printable++
		}
	}

	if total == 0 {
		return false
	}
	return float64(printable)/float64(total) > 0.7
}

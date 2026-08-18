# Paste Failure Notification + .bin Fix

## Problem
`handlePaste()` in `internal/app/paste.go` silently does nothing when:
- Clipboard is empty (no text, no image, no files)
- Clipboard has text but it's a URL (user expected image paste)
- Image read fails (no valid image in any X11 target)

User gets zero feedback. Feels broken.

Additionally, URL downloads that fail MIME detection save as `.bin` — sketchy.

## Plan

### 1. Empty clipboard → warning notification

In `handlePaste()`, after reading payload, if all fields are empty:

```go
if payload.Text == "" && len(payload.Image) == 0 && len(payload.Files) == 0 {
    m.setNotification(ui.NotificationWarning, "Clipboard is empty")
    return *m, nil
}
```

### 2. URL-as-text paste → info notification

If text is a single URL (starts with `http://` or `https://`, no newlines) and no image bytes were found:

```go
if len(payload.Image) == 0 && isSingleURL(payload.Text) {
    m.setNotification(ui.NotificationInfo,
        "Pasted URL as text — prefix with @ to download as attachment")
}
```

Helper:
```go
func isSingleURL(s string) bool {
    s = strings.TrimSpace(s)
    if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
        return false
    }
    return !strings.Contains(s, "\n")
}
```

The user then types `@` before the URL (or re-pastes with `@` prefix) and the
existing `bareURLPattern` / `fileURLPattern` in `session.go` handles the download.

### 3. Image read rejected → debug-only (no notification)

The `logPaste("clipboard.ReadImage", "no valid image found...")` already covers this.
Don't spam notifications for this — it's a race condition where the user pasted before
the browser finished putting image data on the clipboard. The user will just paste again.

---

## .bin Extension Fix

### Problem
When `IngestService.Ingest()` downloads a URL (`IngestSourceKindURL`):

1. `download()` gets `Content-Type` header → `extFromMIME()` → may return `""`
2. `storeContent()` calls `DetectMIMEFromBytes()` → Go's `http.DetectContentType`
   only sniffs first 512 bytes → WebP/RIFF sometimes returns `application/octet-stream`
3. `extFromMIME("application/octet-stream")` → `""`
4. Falls back to `extFromMIME(MIMEForPath(displayName))` → displayName is a hash, no ext → `""`
5. Final fallback: `fileExt = "bin"`

Result: `url-e873e19ce0eac12a.bin` — a PNG/WebP with no useful extension.

### Fix (in `internal/media/ingest.go`)

**A. Prefer Content-Type header over byte-sniffing for URLs**

In `storeContent()`, for URL sources, pass the `Content-Type` from the HTTP response
as the `extHint` instead of relying solely on `DetectMIMEFromBytes`:

```go
// In download(), return the content-type alongside content+ext
// Already done: ext := extFromMIME(resp.Header.Get("Content-Type"))
// But storeContent re-detects from bytes. Fix: trust the header for URL sources.
```

Concretely: add a `MIMEHint string` field to `IngestSource`. Set it from the
`Content-Type` header in `download()`. In `storeContent()`:

```go
mimeType := DetectMIMEFromBytes(content, extHint)
// For URL sources, if we have a MIME hint from the header and byte-detection
// returned octet-stream, trust the header:
if src.Kind == IngestSourceKindURL && src.MIMEHint != "" &&
    mimeType == "application/octet-stream" {
    mimeType = src.MIMEHint
}
```

**B. Add missing MIME → ext mappings in `extFromMIME`**

Check that these are present (add if missing):
- `image/webp` → `"webp"`
- `image/avif` → `"avif"`
- `image/heic` → `"heic"`

**C. Last-resort: use URL path extension before `.bin`**

In `storeContent()`, before the final `"bin"` fallback:

```go
if fileExt == "" && src.Kind == IngestSourceKindURL {
    if u, err := url.Parse(src.URL); err == nil {
        if ext := strings.TrimPrefix(filepath.Ext(u.Path), "."); ext != "" && len(ext) <= 5 {
            fileExt = ext
        }
    }
}
if fileExt == "" {
    fileExt = "bin"
}
```

This means `preview.redd.it/foo.png?auto=webp` → ext `"png"` from URL path,
even if MIME detection fails.

### Files to change
- `internal/media/ingest.go` — add `MIMEHint` field, fix `storeContent` logic, add URL-path ext fallback
- `internal/media/ingest_test.go` — test WebP download with octet-stream sniff

## Files to change (summary)
- `internal/app/paste.go` — add the two notification paths
- `internal/media/ingest.go` — MIMEHint, ext fallback chain
- `internal/media/ingest_test.go` — new tests

## Out of scope (separate plan)
- Auto-detect image URLs in paste and auto-ingest (bigger feature, needs user opt-in)

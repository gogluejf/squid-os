# Squid — Portable Multimodal Attachments

Executable specification. Derived from implementation, not aspirational.

## Supported Tiers

Attachments fall into three tiers based on provider delivery capability:

| Tier | Kinds | Description |
|------|-------|-------------|
| **Deliverable** | `image`, `pdf`, `text` | Sent directly to the model via GoAI parts. Images use `PartImage` (data URI). PDFs and generic files use `PartFile`. Text uses bounded inline `PartText`. |
| **Storable** | `audio`, `video` | Stored in the workspace, referenceable in messages, but not sent as direct GoAI input — no GoAI adapter serializes `PartAudio` or `PartVideo`. |
| **Unsupported** | provider-dependent | `pdf` and `file` are unsupported on `openai-compatible` dialects (Chat Completions adapter silently omits `PartFile`). |

**Initial direct-provider baseline:** images and verified PDFs.

Images are supported across all four dialects (OpenAI-compatible Chat Completions, OpenAI Codex Responses, Anthropic, Gemini). PDFs are supported on Anthropic, Gemini, and OpenAI Codex — but **not** on OpenAI-compatible Chat Completions.

### Dialect Support Matrix

| Dialect | Image | PDF | Text | Audio | Video |
|---------|-------|-----|------|-------|-------|
| `openai-compatible` (Chat Completions) | Supported | **Unsupported** | Supported | Storable | Storable |
| `openai-codex` (Responses API) | Supported | Supported | Supported | Storable | Storable |
| `anthropic` | Supported | Supported | Supported | Storable | Storable |
| `gemini` | Supported | Supported | Supported | Storable | Storable |

Providers using `openai-compatible` include: OpenAI (chat), Ollama, LiteLLM, vLLM, Azure, OpenRouter, Fireworks, xAI, Groq, DeepSeek, Together, DeepInfra, Requesty, Cohere, Mistral, Perplexity, Cerebras, NVIDIA, RunPod, FPTCloud, Cloudflare, llama.cpp.

Providers using `anthropic`: Anthropic, Bedrock, MiniMax.

Providers using `gemini`: Gemini, Vertex.

## Shared Attachment Contract

### Attachment Record

Every attachment is a `media.Attachment` struct persisted in the session document:

```
ID           string  // UUID — unique per session
RelativePath string  // always "media/<hash-prefix>.<ext>"
DisplayName  string  // original filename or hint
MIME         string  // content-detected MIME type
Kind         Kind    // image, pdf, text, audio, video, file
Size         int64   // bytes
SHA256       string  // hex digest of file content
Source       Source  // file, url, clipboard, tool, migrated
CreatedAt    string  // RFC3339 timestamp
```

All paths are session-relative under `media/` and traversal-safe (validated by `IsTraversalSafe` and `ValidateAttachmentPath`).

### Canonical Reference

Every attachment is referenced in message text as `@file:<UUID>`. The `CanonicalRef()` method returns this format. Message text is the source of truth — removing the reference before submission prevents delivery. The physical file remains in the session for future reuse.

### Ingestion Pipeline

All sources (file, URL, clipboard, tool) pass through `IngestService`:

1. Validate source (absolute path required for files, scheme check for URLs).
2. Read content with size limits (100 MB total session, 50 MB per download).
3. Detect MIME from content (`http.DetectContentType` on first 512 bytes), extension as fallback hint.
4. Compute SHA256 hash.
5. Deduplicate: if a matching hash exists in the registry, return the existing attachment.
6. Store atomically: write to `.tmp-<uuid>` then rename to `<hash-prefix>.<ext>`.
7. Create and register the `Attachment` record.

Partial or rejected ingestion leaves no orphaned final media file.

### URL Security

URL ingestion enforces:
- Scheme: `http` and `https` only.
- Timeout: 30 seconds default.
- Redirect limit: 5, each redirect re-validated for SSRF.
- Size limit: 50 MB per download.
- SSRF protection: DNS resolution blocks private IPs (RFC 1918), loopback, link-local, and unique-local addresses. All `AllowPrivateIP`, `AllowLoopback`, `AllowLinkLocal`, `AllowUniqueLocal` default to `false`.

### Limits

Configured via `media.Limits`:

| Parameter | Default |
|-----------|---------|
| MaxFiles | 100 |
| MaxBytes (total session) | 100 MB |
| MaxDownloadBytes | 50 MB |
| DownloadTimeout | 30s |
| MaxRedirects | 5 |

### GoAI Part Resolution

At context-build time, `ResolveMessageAttachments` converts each `@file:<ID>` reference into a `ResolvedAttachment` pairing the `media.Attachment` with its `goai_provider.Part`:

- **Images** → `PartImage` with `data:<mime>;base64,<encoded>` URL and `Detail: "auto"`.
- **PDFs / Files** → `PartFile` with base64 data URI, MIME type, and display name.
- **Text** → Bounded `PartText` (32 KiB inline limit) with `--- inline: <name> (<mime>) ---` header and `--- [truncated: N bytes omitted] ---` marker if truncated.
- **Unknown kinds** → Default to `PartFile`.

Missing or corrupt files produce an error in the returned errors slice — context construction continues with the remaining resolved parts.

### Token Accounting

`EstimateAttachmentTokens` provides conservative estimates per attachment:
- Images, PDFs, files, audio, video: 1 token per 4 bytes.
- Text: standard approximation (`CountTokensApproxInt`), bounded by 32 KiB inline limit.
- Unknown kinds: 1 token per 4 bytes (same as images).

Estimates contribute to context input totals and are stored in `AttachmentUsage` records, distinguishable from exact text counts.

## Search and Paste Behavior

### File Search Roots

Configured via `FileSearchConfig` in settings (`settings.json` → `file_search`):

| Parameter | Default |
|-----------|---------|
| Roots | Working directory only (empty array resolves to `.`) |
| MaxDepth | 3 levels from each root |
| MaxResults | 50 files |
| Ignore | Hidden directories + `.git`, `.hg`, `.svn`, `node_modules`, `vendor`, `.idea`, `.vscode`, `__pycache__`, `.pytest_cache`, `dist`, `build`, `target`, `.next`, `.nuxt` |

Roots accept a configurable string array. Relative paths resolve against the working directory. `~` expands to home directory. Hidden files (names starting with `.`) are excluded.

Candidates appear as `ReferenceCandidate` with kind (`file` or `url`), display name (relative path), and source (absolute path or URL).

Direct path entry: if the query looks like a path (contains `/` or `\`) and the file exists, it resolves immediately. Media URLs (`http`/`https` with host) are also accepted as direct candidates.

### Paste Behavior

Ctrl+V and Shift+Insert invoke `handlePaste`, which reads the clipboard through a fallback chain:

1. `atotto/clipboard` (macOS `pbpaste`, X11 `xclip`/`xsel`, Windows).
2. `wl-clipboard` (`wl-paste --no-newline`) if `WAYLAND_DISPLAY` is set.
3. `xclip -out -selection clipboard` as fallback (WSL2, X11 forwarding).

Clipboard text is processed as follows:
- **Empty clipboard**: no-op.
- **File URLs** (`file://` prefix): ingest each file as a clipboard attachment and insert `@file:<ID>` references.
- **Text below threshold**: inserted directly into the textarea — no change to ordinary paste behavior.
- **Text above threshold**: stored as a text attachment and `@file:<ID>` is inserted.

The large-text threshold is configurable via `PasteConfig.LargeTextBytes` in settings (default: **32 KiB** / 32768 bytes). Notifications identify the origin (clipboard, local file, URL) and the workspace media path.

## Capability and Retry Policy

### Media Decision Classification

`MediaPolicy.Decide()` classifies each attachment kind against resolved `ModelCapabilities`:

| Condition | Decision | Behavior |
|-----------|----------|----------|
| Capabilities nil or zero-valued | `attempt` | Unknown model — send optimistically. |
| Kind is `text` | `allow` | Always delivered. |
| Kind is `image` and `caps.InputModalities.Image` is true | `allow` | Sent as `PartImage`. |
| Kind is `image` and image capability absent | `omit` | Not sent; visible omission reason shown. |
| Kind is `pdf`/`file` and `caps.InputModalities.PDF` is true | `allow` | Sent as `PartFile`. |
| Kind is `pdf`/`file` and PDF capability absent | `omit` | Not sent; visible omission reason shown. |
| Unknown kind (audio, video) | `attempt` | Sent optimistically — likely rejected by provider. |

### Retry Policy

**One classified retry only.** When a provider returns an error that `MediaPolicy.IsMediaRejection()` classifies as a media-specific rejection:

1. The request is retried exactly once with all rejected attachments removed.
2. The user is notified that attachments were stripped for the retry.
3. The original message history is preserved — only the delivery changes.

**Non-media errors are never misclassified for retry.** The rejection classifier excludes authentication errors (`401`, `403`, `unauthorized`, `forbidden`), quota/rate-limit errors (`429`, `rate limit`, `quota`), server errors (`500`, `503`), network errors (`connection refused`, `dial tcp`, `context deadline`, `context canceled`), and safety/content-filter errors (`safety`, `content_filter`, `blocked`, `violates`).

Media rejection is detected via pattern matching on error messages: `image_input`, `image_url`, `does not support image`, `multimodal input`, `vision is not available`, `file_input`, `pdf is not supported`, `unsupported modality`, `base64 image`, `data:image`, `data_uri`, `input_image`, `image parts`, and GoAI `APIError` with status 400 containing media keywords.

## Temporary, Saved, Forked, and Incognito Lifecycle

### Workspace Types

Two workspace implementations abstract over storage location:

- **`PersistentWorkspace`** — backed by `<session-dir>/media/`. Used for saved sessions. Attachments ingest directly into the session media folder.
- **`TempWorkspace`** — backed by a temporary directory under the temp folder with prefix `squid-session-<uuid>/media/`. Used for unsaved sessions (autosave-off).

Both expose the same `Workspace` interface (`MediaDir()`, `Resolve()`, `Register()`). Ingestion always goes through `IngestService`, not directly on the workspace.

### Unsaved → Save Migration

When an unsaved session is explicitly saved for the first time:

1. `MigrateTempWorkspace` calls `PersistWorkspace` to move all media files from the temp directory to `<session-dir>/media/`.
2. Each file is moved atomically (copy to `.tmp` then rename).
3. If any move fails, all previously moved files are rolled back — the session document never references missing media.
4. The returned `MigrationResult` contains the new `PersistentWorkspace` and a list of moved relative paths.
5. After migration, all new attachments ingest directly into the session media folder — no more temp indirection.

**Autosave-off sessions** write only to the runtime temp directory until explicit save. Autosave and manual saves on a saved session only update `chat.json` — no migration occurs.

### Fork Behavior

`CopyWorkspace` copies all media files from the source session's `media/` directory to the destination session's `media/` directory. Each file is copied atomically (write temp + rename). Attachment references (`RelativePath`) remain valid because the directory layout is preserved. The forked session has its own independent copy of all media.

### Incognito Lifecycle

Incognito workspaces use isolated temporary directories under the temp folder with prefix `squid-incognito-<uuid>-<uuid>/`. They are removed on normal session exit via `RemoveWorkspace()`.

**Startup cleanup:** `CleanupStale()` scans the temp root for directories matching `squid-incognito-*` or `squid-session-*` prefixes whose modification time is older than the configured threshold. It removes at most `MaxEntries` directories per run. Only Squid-owned directories are targeted — arbitrary temp files are never deleted. Crash leftovers (stale temp directories) are cleaned up and never become visible as saved sessions.

## inspect_media Contract

### Tool Schema

```json
{
  "type": "object",
  "properties": {
    "path_or_url": {
      "type": "string",
      "description": "Path to a local file, a session attachment reference (@file:id), or an HTTP/HTTPS URL"
    },
    "query": {
      "type": "string",
      "description": "Natural-language query describing what to extract or analyze from the media"
    }
  },
  "required": ["path_or_url", "query"]
}
```

### Source Resolution

`inspect_media` accepts three source types:

1. **Session reference** (`@file:<id>`): Resolved through the workspace's `Resolve()` method. Requires a non-nil workspace and a valid `SessionDir`.
2. **HTTP/HTTPS URL**: Ingested through the normal `IngestService` pipeline (subject to all URL security and size limits). The resulting attachment ID is recorded in the result.
3. **Local file path**: Absolute paths are used directly. Relative paths are resolved against the session directory.

### Execution Flow

1. Resolve source to an absolute file path.
2. Read file content (limited to 50 MB for inspection).
3. Detect MIME type from content.
4. Validate kind: only `image`, `pdf`, `text`, and `file` kinds are inspectable. Audio, video, and other kinds return `ErrInspectionUnsupported`.
5. Build a GoAI multimodal message with the query as text and the media as a part.
6. Call the configured media-capable model (set via `settings.media_model`, format `"provider/model"`).
7. Consume the stream with `MaxSteps(1)` — single response only.
8. Return bounded text output (max 8 KiB from the model).

### Result Format

Text-only output appended with metadata:

```
<model response text>

[Inspected attachment: @file:<id>]
[inspection_input_tokens: N]
```

The tool never embeds binary or image content in `ToolOutput`.

### Authorization

`inspect_media` is classified as **destructive** only when the source is an HTTP/HTTPS URL (because it downloads external content). Local file reads and session references are non-destructive.

### Configuration

Media inspection requires `settings.media_model` to be set (e.g., `"openai/gpt-4o"`). When empty, the tool returns an error: `"media inspection is not available: no media model configured"`.

The model builder is injected via `media.SetModelBuilder()` to avoid circular dependencies between the `media` and `chat/provider` packages.

### Token Accounting

Input tokens are estimated conservatively:
- Images, PDFs, files: 1 token per 4 bytes.
- Text: `approxTokens()` on the inline content (bounded by 32 KiB).
- Query text: `approxTokens()` on the query string length.

The `InputTokens` field in `InspectResult` is appended to the tool result for downstream accounting.

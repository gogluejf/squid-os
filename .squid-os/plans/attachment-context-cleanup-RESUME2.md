# RESUME 2: Attachment Context Cleanup (Step 4 done, Step 5 next)

## What changed since RESUME 1

### Step 4 — DONE (build green, tests green)
- `Workspace` interface shrunk to **one method**: `Dir() string` (returns the media dir where files live)
- `Attachment.RelativePath` → `Attachment.FileName` (bare filename, no `media/` prefix)
- `CanonicalRef()` = `"@file:" + FileName`
- `IngestService` — removed `SetRegisterFunc` setter; constructor takes `lookup func() []Attachment` + `register func(Attachment)` directly
- `media.ResolveRef(attachments, ref)` — matches by `FileName` first, `ID` as fallback
- Deleted `ResolveRefByPath` (no longer needed)
- `IsTraversalSafe` simplified to bare-filename check (no `media/` prefix, no `filepath.Clean`)
- `ValidateAttachmentPath` → `ValidateAttachmentName` (just calls `IsTraversalSafe`)
- `migrate.go` — uses `src.Dir()` / `dstWs.Dir()` directly, no `media/` joins
- `session.go`:
  - `mediaBaseDir()` returns `ws.Dir()`
  - `GetAttachment` uses `filepath.Join(ws.Dir(), a.FileName)`
  - `AddAttachment` — removed workspace Register block
  - `InitWorkspace` — 1-arg constructors
  - `resolveAttachmentRefs` stores `File: a.FileName`
  - `attachmentByFile` matches by `FileName`
- `tool_exec.go` — inspect_media uses `media.ResolveRef(s.Doc.Attachments, attachID)`
- `attachment.go` (chat) — `resolveAttachmentToPart(a, baseDir string)` instead of `(a, workspace)`
- `ResolveMessageAttachments` / `ResolveMessageAttachmentsForRef` — take `baseDir string` instead of `workspace`
- `context.go` — all builder functions take `baseDir string` instead of `workspace media.Workspace`
- `token_tally.go` — `calculateContextTally` passes `s.mediaBaseDir()`
- `paste.go` — uses `workspace.Dir()` for notifications
- All `registry` params renamed to `attachments` in `media/attachment.go`
- All test files updated (chat, media, app, ui, run, config)

### Test status
- `go build ./...` ✅
- `internal/chat` ✅ (all fixed)
- `internal/media` ✅
- `internal/config` ✅
- `internal/ui` ✅
- `internal/run` ✅
- All other packages ✅
- `internal/app` — 8 pre-existing failures (completion `@skill:` vs `@skill/` format, NOT attachment-related)

### Capabilities research (done in-session, no code changes)
Explored `ModelCapabilities` / `ModalitySet` (vendor goai types) and per-provider
`Capabilities()` implementations. Key findings:
- Caps are **per-model** (method on `chatModel` which holds model ID), not per-provider
- `ModelCapabilitiesOf(m LanguageModel)` returns zero-value if model doesn't implement `CapableModel`
- OpenAI/Gemini/Anthropic/Bedrock all declare: Text+Image+PDF = true, Audio/Video = false
- `PartFile` serialization: Gemini ✅, Anthropic ✅, **OpenAI ❌ (silently dropped)**
- Audio/video: storable now, deliverable only when GoAI providers add support
- `MediaDecisionFor` already handles zero-value caps → `MediaAttempt` (optimistic)
- Reactive `stripMediaParts` retry is the safety net for unknown/wrong caps

---

## What remains

### Step 4 finish — Dead code deletion (quick)
Delete these (no production callers remain):
- `ResolveMessageAttachments` (chat/attachment.go) — build path reads `msg.Attachments` stored refs
- `ResolveMessageAttachmentsForRef` (chat/attachment.go)
- `(*Session).ResolveAttachments` (chat/session.go)
- `BuildAttachmentUsageList` (chat/attachment.go)
- The `match func(media.Attachment) bool` param pattern

**CAUTION:** `buildUserMessageParts` and `buildSyntheticInspectParts` in context.go
currently CALL `ResolveMessageAttachments` / `ResolveMessageAttachmentsForRef`.
Must rewrite them to iterate stored refs (`msg.Attachments` / `tc.Execution.Attachments`)
BEFORE deleting the resolve functions.

### Step 5 — Policy → caps
- Add `s.ModelCaps() goai_provider.ModelCapabilities`
  - Build model via provider, call `ModelCapabilitiesOf`
  - Cache keyed by (provider, model)
  - Invalidate on `SetInference` / `PushModelSwitch`
- At top of `s.BuildContext()`: `caps := s.ModelCaps()`
- Per-ref decision: `MediaDecisionFor(&caps, ref.Kind)`
  - `MediaAllow` / `MediaAttempt` → include part
  - `MediaOmit` → skip part, add visible reason text
- `MediaDecisionFor` currently has NO production caller — this wires it in

### Step 6 — Test cleanup
- Fix 2 WIP integration test files (currently in `/tmp/`):
  - `/tmp/attachment_integration_test.go` (was `internal/chat/`)
  - `/tmp/attachment_integration_test.go` (was `internal/app/`)
  - They use `MaxBytes` (now `MaxSizeBytes`), old signatures, ID-based refs
- Full `go test ./...` green
- Manual check: reload session `2026-08-15_03-21-23-ingest-notif`, footer ~1.4M

### Deferred (LAST)
- Old-session backfill on `LoadSession`: if message has `@file:` text refs but
  empty `Attachments`, re-resolve once and populate. Keep `FileName` (bare, no path).

### Unrelated (separate issue)
- 8 `internal/app` completion test failures (`@skill:` vs `@skill/` format)

---

## Key files (current state)
- `internal/media/workspace.go` — `Workspace{Dir()}`, `PersistentWorkspace`, `TempWorkspace`, `IsTraversalSafe`, `ValidateAttachmentName`
- `internal/media/attachment.go` — `Attachment{FileName}`, `CanonicalRef()`, `ResolveRef`, `ResolveRefBySHA256`, `DeduplicateByHash`
- `internal/media/ingest.go` — `IngestService{workspace, limits, lookup, register}`, `NewIngestService(ws, limits, lookup, register)`
- `internal/media/migrate.go` — `PersistWorkspace`, `MigrateTempWorkspace`, `CopyWorkspace` (all use `Dir()`)
- `internal/chat/session.go` — `Session`, `InitWorkspace`, `mediaBaseDir()`, `GetAttachment`, `AddAttachment`, `attachmentByFile`, `resolveAttachmentRefs`, `resolveFileReferences`, `BuildContext`
- `internal/chat/attachment.go` — `resolveAttachmentToPart(a, baseDir)`, `resolveImagePart/File/Text`, `EstimateAttachmentTokens`, `ResolveMessageAttachments` (TO DELETE), `BuildAttachmentUsageList` (TO DELETE)
- `internal/chat/context.go` — `BuildContext(messages, enabled, baseDir, attachments)`, `buildProviderMessages`, `buildUserMessageParts` (REWRITE to read stored refs), `buildSyntheticInspectParts` (REWRITE), `tallyAPIMessagesTokens`, `estimateMediaPartTokens`
- `internal/chat/engine.go` — `Engine.Stream`, `Engine.runStream` (reactive retry), `stripMediaParts`, `BuildAPIMessages`
- `internal/chat/media_policy.go` — `MediaDecision`, `MediaDecisionFor(caps, kind)`, `isMediaRejection`
- `internal/chat/tool_exec.go` — inspect_media ref storage via `media.ResolveRef`
- `internal/chat/token_tally.go` — `calculateLifetimeTally` (ToolAttachment bucket), `calculateContextTally`
- `internal/config/session.go` — `AttachmentRef{File, Tokens}`, `Message.Attachments`, `ToolCallEntry.Execution.Attachments`, `InputTokenTally.ToolAttachment`

## Immediate next action
1. Rewrite `buildUserMessageParts` + `buildSyntheticInspectParts` to read stored refs
2. Delete the resolve cascade (5 functions)
3. `go build ./...` + `go test ./internal/chat/` green
4. Then Step 5: `s.ModelCaps()` + wire into `BuildContext`

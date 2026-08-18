# Attachment Context Cleanup

## Problem

The attachment/context build path is a cascade of nil-wrapping functions.
`policy` and `caps` are threaded through ~8 functions but are **always nil in
production** (only tests pass real values). Resolution of `@file:` refs happens
at *build time* by scanning message text, which forces `workspace` +
`attachments` to be threaded everywhere. Lifetime tally has no tool-attachment
bucket, so context (1.4M) and lifetime (489k) diverge with no explanation.

## Goals

- One context builder: `s.BuildContext()` with **zero params**.
- One stream entry: `StartStream(ctx, s, endpoints)`.
- Kill `policy` / `caps` / `WithPolicy` cascade entirely.
- Store attachment refs on messages/tool-results so build time doesn't scan text.
- Capability decision (allow/omit) at build time from `s.ModelCaps()` (survives `/model` switch).
- Keep ingest-time limits (`media.Limits`) and temp→persistent migration untouched.

## Invariants (keep these safe)

1. Refs store **relative** paths (`media/xxx.png`) → migration stays bytes-only.
2. File **facts** (kind/mime/size/id) stay in `s.Doc.Attachments` registry; the
   ref on a message is minimal `{File, Tokens}`.
3. Capability decision is **build-time** (model can switch mid-session).
4. `s` is the single source of truth — no injected workspace/attachments/policy.

## Out of scope (future)

- Per-model size caps (`MaxImageBytes`, `MaxImages`).
- `media.Limits` / `SessionQuota` (already correct, write-side).

---

## Deletion order

### Step 1 — Dead code (zero non-test callers)
Delete:
- `buildUserMessageParts` (context.go)
- `buildProviderMessages` (context.go)
- `buildCompactedAPIMessagesWithAttachments` (context.go)
- `Engine.collectOmitted` (engine.go) — no-op
- `countAPIMessagesTokens` (context.go) — *verify no callers first*

Verify: `go build ./...` + `go test ./internal/chat/`

### Step 2 — Kill policy / WithPolicy cascade
Delete:
- `MediaPolicy` interface, `defaultMediaPolicy`, `NewMediaPolicy`
- `Engine.SetPolicy` + `e.policy` field + nil-branches
- `StartStreamWithContext`, `StartStreamWithPolicy` → `StartStream(ctx, s, endpoints)`
- `BuildContext` (free), `BuildContextWithPolicy` (free + method)
- `buildProviderMessagesWithPolicy`, `buildCompactedAPIMessagesWithAttachmentsAndPolicy`,
  `buildUserMessagePartsPolicy`, `buildSyntheticInspectParts` (drop policy/caps params)
- `media_policy_test.go` → retarget to `caps.MediaDecision` or delete

Keep: `isMediaRejection` (becomes free func) + reactive strip-and-retry.

Verify: no `policy`/`caps` param anywhere; `go build` + `go test` green.

### Step 3 — Data model: refs on messages
Add:
- `config.AttachmentRef{File string; Tokens int}`
- `Message.Attachments []AttachmentRef`
- `ToolCallEntry.Attachments []AttachmentRef`
- Populate at creation (Append / ingest / inspect_media)
- Backfill old sessions on `LoadSession` (re-resolve text refs once)
- lifetime gains `input.tool_attachment` bucket

Verify: new sessions store refs; old sessions backfill; tally includes tool attachments.

### Step 4 — Build reads stored refs
Delete:
- `ResolveMessageAttachments`, `ResolveMessageAttachmentsForRef`, the `match func`
- `resolveAttachmentToPart` (workspace-based) → `s.mediaBaseDir()` read
- `(*Session).ResolveAttachments`, `BuildAttachmentUsageList`

Add:
- `(*Session).mediaBaseDir() string` — `tempWorkspaceDir` or `SessionDir`
- `s.BuildContext()` zero-param, reads `msg.Attachments` + registry + `mediaBaseDir()`

Verify: build time no longer scans text or touches workspace registry.

### Step 5 — Policy → caps
- Move `MediaDecision` + `MediaAllow/Omit/Attempt` onto `ModelCapabilities`
- `(*Session).ModelCaps() ModelCapabilities` — cached per (provider,model), invalidated on `/model`
- Build-time omit = `caps.MediaDecision(kind)`
- `s.BuildContext()` now truly zero-param

Verify: model switch re-decides omissions; proactive omit works.

### Step 6 — Test cleanup + verification
- Fix WIP `attachment_integration_test.go` (`MaxBytes`, old ID refs)
- Delete redundant integration tests
- `go build ./...` + `go test ./...` green
- Reload `2026-08-15_03-21-23-ingest-notif`, footer consistent (~1.4M)

---

## Final signatures

### Survive
```go
func StartStream(ctx context.Context, s *Session, endpoints config.EndpointsConfig) <-chan StreamEvent
func RunLoop(ctx context.Context, s *Session, paths config.Paths, endpoints config.EndpointsConfig, checkpoint func() error) <-chan LoopEvent
func (s *Session) BuildContext() Context
func (s *Session) buildProviderMessages(plan CompactionPlan, compact bool, caps goai_provider.ModelCapabilities) []goai_provider.Message
func (s *Session) mediaBaseDir() string
func (s *Session) ModelCaps() goai_provider.ModelCapabilities
func (s *Session) AddAttachment(a media.Attachment)
func (s *Session) GetAttachment(id string) (media.Attachment, string, error)
func resolveImagePart(a media.Attachment, data []byte) goai_provider.Part
func resolveFilePart(a media.Attachment, data []byte) goai_provider.Part
func resolveTextPart(a media.Attachment, data []byte) goai_provider.Part
func EstimateAttachmentTokens(a media.Attachment) int
func tallyAPIMessagesTokens(msgs []goai_provider.Message) apiMessageTokenTally
func estimateMediaPartTokens(dataURI, mimeType string) int
func stripMediaParts(messages []goai_provider.Message) []goai_provider.Message
func isMediaRejection(err error) bool
```

### New
```go
type AttachmentRef struct { File string; Tokens int }
// Message.Attachments, ToolCallEntry.Attachments
func (c ModelCapabilities) MediaDecision(kind media.Kind) MediaDecision
// InputTokenTally.ToolAttachment int
```

### Dropped
See deletion order above (Steps 1–5).

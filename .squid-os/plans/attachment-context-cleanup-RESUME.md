# RESUME: Attachment Context Cleanup (Step 4 — IN PROGRESS)

## Context
You are continuing a multi-step cleanup of the attachment/context build path in
`~/src/squid-os`. Steps 1–3 are DONE and verified. Step 4 is mid-flight and the
build is currently BROKEN. Read this top-to-bottom, then continue.

The full plan is in `.squid-os/plans/attachment-context-cleanup.md`.

## Ground rules (user is opinionated — follow strictly)
- NO wrappers that just forward nil/params. If a function only delegates, delete it.
- NO `policy`/`caps`/`WithPolicy`/`SetPolicy` anywhere. (Already removed in Step 2.)
- `s.BuildContext()` must end with ZERO params — reads everything from `s`.
- The `Workspace` must shrink to the ORIGINAL contract: it is ONLY a storage-location
  concern (`MediaDir()`). NO registry, NO `Resolve`, NO `Ingest`, NO `Register` on it.
  The attachment registry lives on the session: `s.Doc.Attachments`.
- Resolution = `media.ResolveRef(s.Doc.Attachments, ref)` + `filepath.Join(ws.MediaDir(), a.RelativePath)`.
- Do NOT use git stash to test (user said it's too risky). To test without the WIP
  integration test files, `mv` them to /tmp and back. A clean backup of the repo
  (pre-Step-2) is at `~/src/squid-os.bck2` if a file was lost.
- Old-session backfill/migration is DEFERRED to the very end — do NOT do it now.
- After each sub-step: `go build ./...` must pass. Run chat tests with the two WIP
  integration test files moved out (they reference the old API and are already broken):
    mv internal/chat/attachment_integration_test.go /tmp/
    mv internal/app/attachment_integration_test.go /tmp/
    go test ./internal/chat/ -count=1   # expect exactly 10 pre-existing failures (ID-vs-basename drift)
    mv them back
  The 10 known pre-existing failures (NOT yours to fix): TestResolveMessageAttachments{WithRealWorkspace,MissingFile,DuplicateRefInText}, TestBuildAttachmentUsageList, TestAttachmentUsageDistinguishableFromTextTokens, TestSessionResolveAttachments{,Multiple}, TestCalculateTokenTally{AttachmentTokens,MultipleAttachmentTokens}, TestTokenTallyCompactionPreservesAttachmentTotals.

## What's DONE (Steps 1–3)
- Step 1: deleted dead wrappers (buildUserMessageParts, buildProviderMessages free,
  buildCompactedAPIMessagesWithAttachments{,AndPolicy}, Engine.collectOmitted,
  countAPIMessagesTokens, StreamResult, Engine.StreamWithContext). Engine.Stream is now
  the public 3-arg entry; Engine.runStream is the private worker w/ `retrying` guard.
- Step 2: deleted MediaPolicy interface, defaultMediaPolicy, NewMediaPolicy,
  Engine.SetPolicy + e.policy field, StartStreamWithContext, StartStreamWithPolicy,
  BuildContextWithPolicy (free+method), buildProviderMessagesWithPolicy,
  buildUserMessagePartsPolicy. Added free func `MediaDecisionFor(caps, kind)` and
  `isMediaRejection(err)` in internal/chat/media_policy.go. StartStream is now
  `StartStream(ctx, s, endpoints)`.
- Step 3: added `config.AttachmentRef{File, Tokens}`; `Message.Attachments []AttachmentRef`;
  `ToolCallEntry.Execution.Attachments []AttachmentRef`; `InputTokenTally.ToolAttachment int`.
  `Append` populates msg.Attachments via `s.resolveAttachmentRefs(text)`. inspect_media in
  tool_exec.go stores Execution.Attachments. calculateLifetimeTally sums them into
  input.ToolAttachment. Added `s.mediaBaseDir()` and `s.attachmentByFile(relPath)` to session.go.

## Step 4 — CURRENT STATE (build is BROKEN, finish this)
Goal: make the context builder read `msg.Attachments` (stored refs) instead of
scanning text + resolving through the workspace. Delete the resolve cascade.
Shrink Workspace to `MediaDir()` only.

### Already done in this step:
- Rewrote `internal/media/workspace.go`: `Workspace` interface now ONLY `MediaDir() string`.
  `PersistentWorkspace{SessionDir}` and `TempWorkspace{TempDir}` — NO `attachments` field,
  NO `Registry()`, NO `Resolve()`, NO `Ingest()`, NO `Register()`.
  `NewPersistentWorkspace(sessionDir string)` and `NewTempWorkspace(tempDir string)` —
  BOTH now take ONE arg (dropped the `registry []Attachment` param).
  Removed the now-unused `context` import.

### STILL BROKEN — fix these (from `go build ./...`):
1. `internal/media/migrate.go` — 3× `NewPersistentWorkspace(dest, nil)` → drop the `, nil`.
   Also `MigrateTempWorkspace` (~line 250-271) reads `w.attachments` from the source
   workspace to re-attach the registry to the new persistent workspace. That whole
   registry-re-attach block must go (workspace no longer holds a registry). The
   `MigrationResult.Workspace` is still returned but is now registry-less.
2. `internal/media/ingest.go:262` — `s.workspace.Registry()` (dedup by SHA256).
   IngestService needs the registry passed in some other way. IngestService is built in
   session.go `ToolContext` and `resolveFileReferences` via
   `media.NewIngestService(s.Workspace, media.DefaultLimits)`. You must thread the
   registry (`s.Doc.Attachments`) into IngestService so dedup still works — e.g.
   `NewIngestService(ws, limits, registry []Attachment)` and use it at line 262.
   ALSO `ingest.go:303` calls `reg.Register(attachment)` — DELETE that (redundant;
   the session's SetRegisterFunc callback already appends to s.Doc.Attachments).
3. `internal/chat/session.go` — `InitWorkspace()` calls
   `NewTempWorkspace(dir, s.Doc.Attachments)` / `NewPersistentWorkspace(dir, s.Doc.Attachments)`
   → drop the 2nd arg (3 call sites in InitWorkspace). Also `AddAttachment` has a
   redundant `reg.Register(a)` block (the `if reg, ok := s.Workspace.(interface{...})`)
   → DELETE it (registry is now only s.Doc.Attachments). `GetAttachment` uses
   `ws.Resolve(id)` → replace with `media.ResolveRef(s.Doc.Attachments, id)` +
   `filepath.Join(ws.MediaDir(), a.RelativePath)`.
4. `internal/chat/tool_exec.go` — inspect_media path uses `ws.Resolve(attachID)` →
   replace with `media.ResolveRef(s.Doc.Attachments, attachID)` (attachID here is the
   basename after stripping "@file:").
5. `internal/chat/attachment.go` — `resolveAttachmentToPart(a, workspace)` calls
   `workspace.Resolve(a.ID)` → replace with `filepath.Join(workspace.MediaDir(), a.RelativePath)`.
   Then `ResolveMessageAttachments` + `ResolveMessageAttachmentsForRef` + the `match func`
   param can be DELETED (build path will read msg.Attachments instead). See below.
6. `internal/chat/context.go` — `buildUserMessageParts` and `buildSyntheticInspectParts`
   currently call `ResolveMessageAttachments(...)`. Rewrite them to iterate the
   message's stored refs:
   - user msg: iterate `msg.Attachments`; for each ref look up
     `s.attachmentByFile(ref.File)` for kind/mime, read
     `filepath.Join(s.mediaBaseDir(), ref.File)`, build the part via
     resolveImagePart/resolveFilePart/resolveTextPart.
   - inspect synthetic: iterate `tc.Execution.Attachments` the same way.
   NOTE: these are currently free functions taking `(attachments, workspace)`.
   They will need access to the session (for mediaBaseDir + registry). Either make them
   methods on *Session, or pass `(registry []media.Attachment, baseDir string)`.
   The end goal (Step 4/5) is `s.BuildContext()` with zero params, so prefer making the
   builder a method on *Session that reads s.Doc.Messages / s.Doc.Attachments / s.mediaBaseDir().
7. `internal/chat/session.go` — `BuildContext` (the method) currently calls the free
   `BuildContext(messages, enabled, workspace, attachments)`. Collapse so the session
   method is the single builder. The free `BuildContext` in context.go can become a
   method or be inlined.

### Delete (resolve cascade) once the builder reads stored refs:
- `ResolveMessageAttachments`, `ResolveMessageAttachmentsForRef` (attachment.go)
- `(*Session).ResolveAttachments` (session.go) — reads msg.Attachments instead
- `BuildAttachmentUsageList` (attachment.go) — reads msg.Attachments instead
- the `match func(media.Attachment) bool` param pattern (no longer needed)
- `resolvedAttachmentToPart`'s workspace.Resolve usage (see #5)

### Keep:
- `resolveImagePart`, `resolveFilePart`, `resolveTextPart` (attachment.go) — pure part builders
- `EstimateAttachmentTokens` — used at creation time for ref.Tokens
- `media.ResolveRef` / `ResolveRefByPath` / `ResolveRefBySHA256` (attachment.go) — clean, take registry as param
- `stripMediaParts` + `isMediaRejection` + the reactive retry in engine.runStream

## Step 5 (NOT started) — policy → caps
- Add `s.ModelCaps() goai_provider.ModelCapabilities` — resolve from
  `s.CurrentInference()` (provider+model strings), cache per (provider,model),
  invalidate on model switch (SetInference / PushModelSwitch).
- At top of `s.BuildContext()`: `caps := s.ModelCaps()`. Per-ref build-time decision:
  `switch MediaDecisionFor(&caps, ref.Kind) { case MediaAllow, MediaAttempt: send; case MediaOmit: skip }`.
- `MediaDecisionFor` currently has NO production caller — Step 5 is what wires it in.
- NOTE: `ModelCapabilities` is a VENDOR type (goai) — you CANNOT add a method to it.
  That's why the decision is the free func `MediaDecisionFor(caps, kind)`, not `caps.Method()`.

## Step 6 (NOT started) — test cleanup
- Fix the two WIP integration test files (internal/chat + internal/app
  attachment_integration_test.go): they use `MaxBytes` (now `MaxSizeBytes` in
  media.Limits), old `NewMediaPolicy`/`BuildContextWithPolicy`/`ResolveMessageAttachments`
  signatures, and ID-based `@file:` refs (impl uses basename). Rewrite against the new API.
- Full `go build ./...` + `go test ./...` green.
- Manual check: reload session `2026-08-15_03-21-23-ingest-notif` (in
  ~/.config/squid-os/sessions/), footer should show ~1.4M consistently (was 24k on
  reload due to the LoadSession nil-workspace bug — already fixed via InitWorkspace in LoadSession).

## Old-session backfill (DEFERRED — do LAST, separately)
On LoadSession, if a message has `@file:` text refs but empty `Attachments`,
re-resolve once and populate. Keep paths RELATIVE (never absolute) so the
temp→persistent migration stays bytes-only. User said this "should not be complex".

## Key files
- internal/chat/context.go — the builder (BuildContext, buildProviderMessages,
  buildUserMessageParts, appendAssistantProviderMessages, buildSyntheticInspectParts,
  tallyAPIMessagesTokens, estimateMediaPartTokens)
- internal/chat/attachment.go — resolve cascade + part builders + EstimateAttachmentTokens
- internal/chat/session.go — Session, InitWorkspace, mediaBaseDir, attachmentByFile,
  AddAttachment, GetAttachment, BuildContext, resolveAttachmentRefs, resolveFileReferences
- internal/chat/tool_exec.go — inspect_media ref storage
- internal/chat/token_tally.go — calculateLifetimeTally (ToolAttachment bucket)
- internal/media/workspace.go — Workspace (now MediaDir only)
- internal/media/migrate.go — MigrateTempWorkspace/PersistWorkspace/CopyWorkspace (BROKEN)
- internal/media/ingest.go — IngestService (BROKEN: Registry + Register)
- internal/config/session.go — AttachmentRef, Message.Attachments,
  ToolCallEntry.Execution.Attachments, InputTokenTally.ToolAttachment

## Immediate next action
Run `go build ./...`, fix the 7 broken items listed under "STILL BROKEN" in order
(migrate.go → ingest.go → session.go → tool_exec.go → attachment.go → context.go),
get build green, then run the chat test suite (WIP files moved out) and confirm
still exactly 10 pre-existing failures. Then continue to the "Delete (resolve cascade)"
list and Step 5.

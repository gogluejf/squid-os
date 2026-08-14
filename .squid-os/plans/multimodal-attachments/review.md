# Multimodal Attachments — Agent Review Summary

> **⚠️ DO NOT implement any of these yet.** These are review findings for later consideration during or after implementation.

---

## Agent 1: Architecture & Design Review

**Overall:** Solid foundation. Workspace/Attachment/Ingestion triad is well-separated. Several P0 security gaps.

**Top findings:**

| Priority | Issue |
|----------|-------|
| **P0** | **SSRF protection missing** — `Source.URL` accepts any URL. Must block private IPs, metadata endpoints, `file://` scheme |
| **P0** | **Path traversal in `Resolve`** — `ref` of `../../../etc/shadow` could escape workspace. Must canonicalize and verify under `MediaDir()` |
| **P0** | **Clipboard sanitization absent** — Executables disguised as images, null bytes in filenames |
| **P1** | **`io.Reader` not retry-safe** — Exhausted on first read. Need `io.Seeker` or document single-use |
| **P1** | **Migration has no integrity verification** — `PersistWorkspace` should verify SHA256 post-copy |
| **P1** | **Workspace lacks identity** — Missing `ID()`, `SchemaVersion()`, `List()` methods |
| **P2** | **No deduplication logic** — `SHA256` field exists but nothing checks for duplicates before storing |
| **P2** | **CleanupPolicy is naive** — `MaxEntries` without eviction strategy (FIFO/LRU/size) is unpredictable |

**Interface suggestions:**
- `Attachment.DisplayName` → `Name` ("display" is a UI concern in a domain struct)
- Add `CreatedAt`, `LastAccessed` timestamps for cleanup
- Consider sealed interface for `Source` (`source.File`, `source.URL`, `source.Reader`) instead of discriminator tag

---

## Agent 2: Implementation Risk & Testing

**Risk summary:**

| Area | Rating | Why |
|------|--------|-----|
| Task Dependencies | **HIGH** | 7-step critical path. Hidden deps: 1.3↔1.4, 3.1→3.2, 4.1→1.2 |
| Test Coverage | **HIGH** | No race-condition tests, no partial-fork-recovery, no rejection-misclassification tests |
| Complexity | **HIGH** | Task 1.3 touches `ForkSessionTree` (200+ lines, 4 existing tests) — high regression risk. Task 3.2 retry requires streaming architecture change |
| Rollback | **HIGH** | 1.3 has no safe intermediate state. If fork breaks, users lose existing functionality |

**Key recommendations:**
- **Split 1.3 into two sub-tasks** with a checkpoint between persistence migration and fork copy
- **Add a feature flag** for migration behavior
- **Task 3.2 retry is underestimated** — OpenAI, Anthropic, and Gemini all use different error formats for media rejection. `IsMediaRejection` needs provider-specific parsing
- **10 of 35 acceptance criteria are vague** — e.g., "visible reason text" (where? what format?), "bounded text" (what limit?), "narrow terminals" (what column width?)
- **Missing tests:** concurrent ingestion + autosave race, partial fork failure, 401/403 misclassified as media rejection, post-compaction attachment accounting

---

## Agent 3: UX & Provider Compatibility

**UX risks (by severity):**

| Priority | Issue | Recommendation |
|----------|-------|----------------|
| **HIGH** | **Silent large-paste conversion** — User Ctrl+V's 50KB text, it vanishes into `@file/paste-12345.txt` with no feedback | Show persistent notification for 3+ seconds. Make threshold configurable and visible in help. Consider asking at 16KB: "Paste is large. Y to attach, N for text" |
| **HIGH** | **@ completion ambiguity** — Files mixed into @completion alongside skills/agents/tools | Add visual divider (`— files —`) or separate trigger (`@file` vs bare `@`) |
| **HIGH** | **Model switch is reactive, not proactive** — User switches to text-only model, images silently fail then retry | Pre-check attachments against new model capabilities. Notify: "3 images omitted — model doesn't support images" |
| **MEDIUM** | **Four chip types too busy** — Emoji + padding on narrow terminals consumes ~17% of a 60-col line | Reduce to two categories: file attachments (📄) and capability references (already have `@` prefix) |
| **MEDIUM** | **No URL download feedback** — 5-second downloads show nothing | Show spinner during download, result on completion |
| **MEDIUM** | **Provider quirks missing** | OpenAI Chat Completions doesn't support `PartFile`, Gemini requires async upload, Anthropic has separate file upload |

**Provider-specific gaps:**
- **OpenAI Chat Completions:** `PartFile` unsupported — only base64 images. The plan acknowledges this but needs explicit handling
- **Gemini:** Files API requires upload step + polling — files aren't instantly available
- **Bedrock:** Per-model granularity needed (Claude supports images, Llama doesn't)
- **Local models:** Most are text-only. The "attempt unknown modalities" approach is correct but retry-after-rejection is critical

---

## Bottom Line — Critical 5

| # | Issue | Severity | Action |
|---|-------|----------|--------|
| 1 | SSRF + Path Traversal | P0 / Blocker | Add URL validation and path canonicalization before any code ships |
| 2 | Task 1.3 split + feature flag | HIGH | Split migration into (a) temp→persist, (b) fork copy. Add feature flag |
| 3 | Silent large-paste conversion | HIGH UX | Show persistent notification. Make threshold visible in help |
| 4 | Provider policy table in 3.2 | MEDIUM | Hardcode known provider-model-media combos instead of runtime discovery |
| 5 | Race conditions untested | HIGH | Add concurrent ingestion + autosave test |

# TTFT & Inference Duration Measurement Bug (Buffered Providers)

## Problem

Some LLM providers deliver the entire response as a **single chunk** (buffered delivery) instead of streaming tokens incrementally. This breaks our per-message timing metrics: `time_to_first_token_ms` (TTFT) and `inference_duration_ms` become misleading.

### How it manifests

For a message where the provider buffers 4 seconds of generation then sends one chunk:

| Metric | Value | Reality |
|--------|-------|---------|
| `time_to_first_token_ms` | 4200ms | Model didn't "wait" 4s — it generated for 4s server-side |
| `tool_call_metrics.inference_duration_ms` | 18ms | Tool call wasn't generated in 18ms — it was generated over ~4s |
| `duration_ms` | 4218ms | Correct total wall-clock |
| `tok_per_sec` | absurdly high (e.g. 4000 tok/s) | 4000 tokens / 18ms = false speed |

### Affected stats (chat-analytics dashboard)

- **Total Waited** (sum of TTFT): inflated by buffered generation time
- **Total Inference** (sum of text+thinking+tool_call inference): deflated to near-zero
- **Avg Speed** (tok/s): wildly inaccurate for buffered messages
- **Avg TTFT**: inflated, doesn't represent actual "thinking" delay
- **Daily/Weekly token charts**: unaffected (token counts are correct)

### Why it happens

In `internal/chat/metrics.go`:

```go
func (m *StreamMetrics) AddToolCallChars(s string) {
    n := len(s)
    if m.toolCallChars == 0 && n > 0 {
        m.firstToolCallTokenAt = time.Now()  // ← set on FIRST chunk arrival
    }
    m.toolCallChars += n
    m.toolCallDoneAt = time.Now()            // ← updated on LAST chunk arrival
}
```

For streaming providers: many small chunks → `firstToolCallTokenAt` ≈ real first token, `toolCallDoneAt` ≈ real last token. ✓

For buffering providers: one big chunk → `firstToolCallTokenAt` ≈ T+4000ms, `toolCallDoneAt` ≈ T+4018ms. The 4s of generation is absorbed into TTFT. ✗

Same pattern applies to `AddTextChars` and `AddThinkingChars`.

### Detection heuristic (read-side, imperfect)

A message is likely buffered if:
- `output_tokens > 100` AND
- `inference_duration < 50ms` AND
- `time_to_first_token_ms > 500ms`

This catches most cases but isn't perfect (a fast model generating a short response could also match).

## What we investigated

1. **Use `duration_ms` as total processing** — It equals TTFT + inference, so it's the same sum. For streaming providers the split is meaningful; for buffering providers it just packages the same false data into one number. Doesn't solve the problem.

2. **Redistribute time when inference is suspiciously low** — If `inference_duration < 50ms` and `output_tokens > 100`, assume buffered and attribute all time to "generation" instead of "waiting". Heuristic, fragile, doesn't fix the root cause.

3. **Store wall-clock `elapsed_ms` at stream end** — `time.Since(Start)` at completion. Always correct regardless of provider behavior. But doesn't tell us the *split* between waiting and generation for buffered providers.

4. **Per-chunk timestamp logging** — Record arrival time of each chunk. Would allow reconstructing true generation timeline. High overhead, changes storage format significantly.

## Proposed avenues (for future implementation)

### Option A: Add `elapsed_ms` field (minimal, non-breaking)

Add one field to the message struct:

```go
// In config.Message or StreamMetrics finalization:
ElapsedMs int64 `json:"elapsed_ms,omitempty"`
```

Set at stream end: `elapsed_ms = time.Since(Start).Milliseconds()`

- For streaming providers: `elapsed_ms ≈ ttft + inference` (no change in behavior)
- For buffering providers: `elapsed_ms` is still correct, reveals that TTFT ≈ elapsed (all time was generation, not waiting)
- Analytics can use `elapsed_ms` as the honest "total processing" and flag messages where `ttft > 0.9 * elapsed` as "likely buffered"

**Pros:** One field, no breaking changes, always correct upper bound.
**Cons:** Still can't recover the true TTFT/generation split for buffered providers.

### Option B: Chunk-count metadata

Store `chunk_count` (number of delivery events received) alongside existing metrics:

```go
ChunkCount int `json:"chunk_count,omitempty"`
```

- Streaming provider: `chunk_count` = hundreds/thousands
- Buffering provider: `chunk_count` = 1 or 2

Analytics rule: if `chunk_count < 5` and `output_tokens > 100`, treat as buffered → show "Generation: Xs (buffered delivery)" instead of splitting into TTFT/inference.

**Pros:** Clean detection signal, no heuristics.
**Cons:** Slightly more state to track, still can't recover true split.

### Option C: Provider-aware timing (ideal, complex)

If the provider API exposes server-side timing headers (e.g. `x-inference-duration`, `x-time-to-first-token`), capture and store them. This gives the true server-side TTFT and generation time regardless of delivery chunking.

**Pros:** Fully accurate.
**Cons:** Not all providers expose this, adds provider-specific logic.

### Option D: Estimated per-token timing from single chunk

When a single chunk delivers N tokens over D ms (measured from chunk start to end), estimate:
- `estimated_ttft = 0` (unknown)
- `estimated_inference = D` (the chunk delivery time approximates generation time)

Only apply when `chunk_count == 1` and `N > threshold`.

**Pros:** Better than current for buffered case.
**Cons:** Still an estimate, chunk delivery time ≠ generation time (network transfer adds time).

## Recommendation

**Short-term (this session):** No code change. Dashboard cards are labeled accurately enough. The 6h43m "waited" vs 1h51m "inference" ratio is a known artifact of buffered providers.

**Medium-term (next squid-os iteration):** Implement **Option A + B** together:
- Add `elapsed_ms` (always-correct total)
- Add `chunk_count` (detection signal)
- Update analytics to show "Generation (buffered)" when `chunk_count < 5`

**Long-term:** Pursue **Option C** for providers that support it.

## Files involved

- `~/src/squid-os/internal/chat/metrics.go` — StreamMetrics, AddToolCallChars, AddTextChars, AddThinkingChars
- `~/src/squid-os/internal/chat/tool_exec.go` — Message finalization (lines 130-142)
- `~/src/squid-os/internal/chat/loop.go` — Same finalization for non-tool-call messages
- `~/src/squid-os/internal/config/session.go` — Message struct definition
- `~/.config/squid-os/skills/chat-analytics/scripts/server.py` — Analytics read side (get_daily_usage)

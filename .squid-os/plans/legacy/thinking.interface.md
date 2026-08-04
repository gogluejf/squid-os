# Plan: Clean Token/Timing Metrics Interface

## Context

Token counts and timing calculations are scattered across `stream.go`, `util.go`, and `render.go`. The goal is a clean split: `StreamMetrics` owns all timing and token counts; `streamState` owns text accumulation and exposes `AddTextChunk`/`AddThinkChunk` as the single entry points that feed metrics internally; rendering functions receive a plain snapshot struct.

---

## Ownership model

| Concern | Owner |
|---|---|
| Timing (first token, durations) | `StreamMetrics` |
| Token counts (text, thinking, total) | `StreamMetrics` (private char counts, public methods) |
| Text/thinking accumulation | `streamState` |
| Entry points for stream events | `streamState.AddTextChunk`, `streamState.AddThinkChunk` |
| Token methods exposed to callers | `m.stream.metrics.TextTokens()` etc. |

---

## Step 1 — New `StreamMetrics` type: `internal/app/metrics.go` (new file)

```go
type StreamMetrics struct {
    Start                time.Time
    firstThinkingTokenAt time.Time
    thinkingDoneAt       time.Time
    firstTextTokenAt     time.Time
    thinkingChars        int // private — updated by streamState
    textChars            int
}
```

**Public methods:**
- `MarkThinkingDone()` — records `thinkingDoneAt`
- `HasFirstToken() bool`
- `TextTokens() int` — `countTokensApprox` on `textChars`
- `ThinkingTokens() int` — `countTokensApprox` on `thinkingChars`
- `TotalTokens() int` — `countTokensApprox` on `thinkingChars + textChars`
- `Duration() time.Duration` — `time.Since(Start)`
- `ThinkingDuration() time.Duration` — `firstThinkingTokenAt` → `thinkingDoneAt` (or now if active)
- `TextDuration() time.Duration` — `firstTextTokenAt` → now
- `TimeToFirstToken() time.Duration` — earliest of firstThinkingTokenAt/firstTextTokenAt minus Start
- `TimeToFirstTextToken() time.Duration` — `firstTextTokenAt - Start`
- `AvgTokenPerSec() float64` — `TotalTokens()` / seconds since first token

**Private methods (called only by streamState):**
- `addTextChars(n int)` — `textChars += n`; records `firstTextTokenAt` on first call
- `addThinkChars(n int)` — `thinkingChars += n`; records `firstThinkingTokenAt` on first call

---

## Step 2 — Update `streamState` in `internal/app/stream.go`

Remove fields: `outputTokenCount`, `thinkingTokenCount`, `start`, `firstTokenTime`.  
Add: `metrics StreamMetrics`

Add chunk methods — the only public way to feed the stream:
```go
func (ss *streamState) AddTextChunk(text string) {
    ss.text += text
    ss.metrics.addTextChars(len(text))
}

func (ss *streamState) AddThinkChunk(think string) {
    ss.thinking += think
    ss.metrics.addThinkChars(len(think))
}
```

Update `reset()`: `ss.metrics = StreamMetrics{}`.  
Update `setStreamMode()`: `m.stream.metrics.Start = time.Now()`.

---

## Step 3 — Update `handleStreamEvent` in `internal/app/stream.go`

```go
if event.Text != "" {
    m.stream.AddTextChunk(event.Text)
}
if event.Thinking != "" {
    m.stream.AddThinkChunk(event.Thinking)
}
if m.stream.inThinking && !event.InThinking {
    m.stream.metrics.MarkThinkingDone()
}
m.stream.inThinking = event.InThinking
```

**On `event.Done`:**
```go
assistantMsg := config.Message{
    ...
    ThinkingTokens:         m.stream.metrics.ThinkingTokens(),
    OutputTokens:           m.stream.metrics.TextTokens(),
    TokensPerSecond:        m.stream.metrics.AvgTokenPerSec(),
    ResponseTimeMs:         m.stream.metrics.Duration().Milliseconds(),
    TimeToFirstTokenMs:     m.stream.metrics.TimeToFirstToken().Milliseconds(),
    ThinkingDurationMs:     m.stream.metrics.ThinkingDuration().Milliseconds(),
    TextTimeToFirstTokenMs: m.stream.metrics.TimeToFirstTextToken().Milliseconds(),
}
```

---

## Step 4 — Update `config.Message` in `internal/config/types.go`

Add new fields with `omitempty`:
```go
TimeToFirstTokenMs     int64 `json:"time_to_first_token_ms,omitempty"`
ThinkingDurationMs     int64 `json:"thinking_duration_ms,omitempty"`
TextTimeToFirstTokenMs int64 `json:"text_time_to_first_token_ms,omitempty"`
```
All existing fields unchanged.

---

## Step 5 — Fix `chatSession.totalTokens()` in `internal/app/chat_session.go`

```go
total += msg.InputTokens + msg.OutputTokens + msg.ThinkingTokens
```

---

## Step 6 — Delete `calcTokPerSec()` from `internal/app/util.go`

Replaced by `m.stream.metrics.AvgTokenPerSec()`.

---

## Step 7 — Add `buildFooterData()` to `internal/app/render.go`

```go
func (m Model) buildFooterData() ui.FooterData {
    return ui.FooterData{
        Model:       m.settings.Model,
        Provider:    m.settings.Provider,
        TotalTokens: m.session.totalTokens() + m.stream.metrics.TotalTokens(),
        Streaming:   m.stream.active,
        TokPerSec:   m.stream.metrics.AvgTokenPerSec(),
    }
}
```

Replace the inline block in `View()` with `m.buildFooterData()`.

---

## Step 8 — New `StreamingViewData` struct in `internal/ui/message.go`

```go
type StreamingViewData struct {
    RenderedMarkdown     string
    Partial              string
    ThinkingText         string
    InThinking           bool
    Width                int
    ThinkingExpanded     bool

    // Timing
    RequestStart         time.Time
    FirstThinkingTokenAt time.Time
    ThinkingDoneAt       time.Time
    FirstTextTokenAt     time.Time

    // Token counts
    ThinkingTokens       int
    TextTokens           int

    // Derived
    TokPerSec            float64
    Waiting              bool // no first token yet
}
```

Change signature: `func RenderStreamingMessage(data StreamingViewData) string`

---

## Step 9 — Update message rendering in `internal/ui/message.go`

### Streaming header (`renderStreamingHeader`)
- Always shows avg tok/s once `> 0`
- During waiting/thinking: right side = tok/s only (or empty)
- Once text phase starts (`!data.FirstTextTokenAt.IsZero()`): right side = `"X tok/s  1.2s  N tokens"` (text elapsed + text tokens)

### Streaming body (`RenderStreamingMessage`), top to bottom:
1. **Waiting** (`data.Waiting`): show `"waiting...  1.2s"` with live elapsed from `RequestStart`, in the body area (thinking label style, dark bg). No thinking block yet.
2. **Thinking block** (shown when `data.InThinking || data.ThinkingText != ""`): dark background, always before text. Label: `" thinking (N tokens)  1.2s"` — tokens in parens, duration beside. Duration is live from `FirstThinkingTokenAt` while active; frozen at `ThinkingDoneAt - FirstThinkingTokenAt` once done.
3. **Text content**: below the thinking block.

### Rendered message (`RenderMessage` + `renderHeader`) — mirrors text phase:
- Header right side: `"X tok/s  1.2s  N tokens"` using `TokensPerSecond`, `ResponseTimeMs` (text duration), `OutputTokens`
- Thinking block (if `ThinkingText != ""`): dark background, before text. Label: `" thinking (N tokens)  1.2s"` using saved `ThinkingTokens` and `ThinkingDurationMs`

---

## Step 10 — Update `updateViewportContent` in `internal/app/render.go`

```go
b.WriteString(ui.RenderStreamingMessage(ui.StreamingViewData{
    RenderedMarkdown:     m.stream.markdown,
    Partial:              partial,
    ThinkingText:         m.stream.thinking,
    InThinking:           m.stream.inThinking,
    Width:                m.width,
    ThinkingExpanded:     m.thinkingExpanded,
    RequestStart:         m.stream.metrics.Start,
    FirstThinkingTokenAt: m.stream.metrics.firstThinkingTokenAt,
    ThinkingDoneAt:       m.stream.metrics.thinkingDoneAt,
    FirstTextTokenAt:     m.stream.metrics.firstTextTokenAt,
    ThinkingTokens:       m.stream.metrics.ThinkingTokens(),
    TextTokens:           m.stream.metrics.TextTokens(),
    TokPerSec:            m.stream.metrics.AvgTokenPerSec(),
    Waiting:              !m.stream.metrics.HasFirstToken(),
}))
```

---

## Step 11 — Update `ui.FooterData` in `internal/ui/footer.go`

Remove `InThinking` and `Waiting` — footer no longer needs to know about phases. It just shows tok/s + total tokens + model.

---

## File Execution Order

1. `internal/app/metrics.go` — new file
2. `internal/config/types.go` — add Message fields
3. `internal/app/stream.go` — embed StreamMetrics, add AddTextChunk/AddThinkChunk, update reset/setStreamMode/handleStreamEvent
4. `internal/app/chat_session.go` — fix totalTokens()
5. `internal/app/util.go` — delete calcTokPerSec()
6. `internal/ui/footer.go` — simplify FooterData
7. `internal/ui/message.go` — StreamingViewData, updated RenderStreamingMessage + renderStreamingHeader
8. `internal/app/render.go` — buildFooterData(), update View() and updateViewportContent()

---

## Verification

- Build: `go build ./...`
- Existing `.chat.json` sessions load without error (new fields have `omitempty`)
- Stream with thinking: body shows waiting → thinking block with live time+tokens → text appears, thinking block freezes, header shows text tok/s + text time
- Stream without thinking: body shows waiting → text appears, header shows tok/s
- Footer always shows avg tok/s + total tokens
- Cancel mid-stream: clean reset, no stale metrics

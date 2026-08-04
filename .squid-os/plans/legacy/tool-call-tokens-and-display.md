# Plan: Tool Call Token Tracking & Visual Redesign

## Context

The codebase currently tracks tokens for **text** and **thinking** via `StreamMetrics` (character-based, `/4` approximation). Tool calls are accumulated in `streamState.pendingTools` but have **no token counting, no timing metrics, and minimal visual display**. The `ToolCallEntry` struct has no token/timing fields. The `Message` struct has no per-tool-call token/timing fields. `totalTokens()` in `chat_session.go` only sums `UserTokens + TextTokens`, ignoring tool call tokens.

---

## Phase 1: Data Model — Add token & timing fields to `ToolCallEntry`

**File: `internal/config/session.go`**

### 1a. Extend `ToolCallEntry`

Add per-tool-call metrics:

```go
type ToolCallEntry struct {
    ID        string `json:"id"`
    Type      string `json:"type"`
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
    Result    string `json:"result,omitempty"`
    Error     string `json:"error,omitempty"`

    // NEW fields:
    Tokens             int     `json:"tokens,omitempty"`               // total tokens (call + result)
    CallTokens         int     `json:"call_tokens,omitempty"`          // tokens in the call (arguments)
    ResultTokens       int     `json:"result_tokens,omitempty"`        // tokens in the result
    DurationMs         int64   `json:"duration_ms,omitempty"`          // time to get result
    TimeToFirstTokenMs int64  `json:"time_to_first_token_ms,omitempty"` // TTFB for tool
    TokPerSec          float64 `json:"tok_per_sec,omitempty"`          // tokens/sec for this tool
}
```

### 1b. No changes to `ToolResultEntry`

It already carries `Result` and `Error` strings; tokens will be computed from their length.

---

## Phase 2: Metrics — Track tool call tokens & timing during execution

**File: `internal/app/stream.go`**

### 2a. In `executeTools()` — add timing and token computation

For each tool call:
- Record `time.Now()` **before** `tool.Execute(args)`
- Record `time.Now()` **after** result arrives
- Compute:
  - `DurationMs = elapsed.Milliseconds()`
  - `CallTokens = countTokensApproxInt(len(tc.Function.Args))`
  - `ResultTokens = countTokensApproxInt(len(result))`
  - `Tokens = CallTokens + ResultTokens`
  - `TimeToFirstTokenMs = DurationMs` (tools are synchronous — TTFB ≈ total duration)
  - `TokPerSec = float64(Tokens) / elapsed.Seconds()` (guard division by zero)

### 2b. Update the `ToolCallEntry` in the assistant message

After tool execution, set the new fields on the existing `ToolCallEntry` (currently only `Result` and `Error` are set).

---

## Phase 3: Metrics — Include tool call tokens in `StreamMetrics` and `totalTokens`

**File: `internal/app/metrics.go`**

### 3a. Add `toolChars int` to `StreamMetrics`

Accumulate tool call + result character counts during streaming (before tool execution, we only have the call arguments).

### 3b. Add method `addToolChars(n int)`

Similar to `addTextChars` — records first token time on first call.

### 3c. Add method `ToolTokens() int`

Returns `countTokensApproxInt(m.toolChars)`.

### 3d. Update `TotalTokens()`

Include tool tokens:
```go
func (m StreamMetrics) TotalTokens() int {
    return countTokensApproxInt(m.thinkingChars + m.textChars + m.toolChars)
}
```

**File: `internal/app/stream.go`**

### 3e. In `handleStreamEvent()` — when `event.ToolCalls` arrive

For each new tool call, compute approximate token count from `tc.Function.Args` length and call `m.stream.metrics.addToolChars(len(tc.Function.Args))`.

**File: `internal/app/chat_session.go`**

### 3f. Update `totalTokens()`

```go
func (cs *chatSession) totalTokens() int {
    total := 0
    for _, msg := range cs.file.Messages {
        total += msg.UserTokens + msg.TextTokens
        // NEW: add tool call tokens
        for _, tc := range msg.ToolCalls {
            total += tc.Tokens
        }
    }
    return total
}
```

---

## Phase 4: Visual — Redesign `renderToolCallsInline` in `message.go`

**File: `internal/ui/message.go`**

### 4a. Colors

- **Tool call line**: stays **blue** (color `"110"`) — already via `ToolCallStyle`.
- **Checkmark `✓`**: change to **dark green** (color `"65"`).
- **Error `✗`**: stays red (`"196"`) — already correct.
- **Token/timing info** at end of line: **dark grey** (color `"243"`).

### 4b. Collapsed tool call line format (no result or long result)

```
 ↳ tool_name(args truncated to 50) ✓  call: X tok/s X tokens
```

- Blue for the tool call part, dark grey for the metrics suffix.
- If result is long and collapsed, append: `, ctrl+e to expand` in dark grey.
- The `call:` metrics come from `ToolCallEntry.CallTokens`, `ToolCallEntry.TokPerSec`.

### 4c. Collapsed with short result (≤30 chars)

```
 ↳ tool_name(args) ✓ <result>  call: X tok/s X tokens | result: X tokens
```

- For the inline result preview, replace `\n` with ` ` to avoid ugly two-line wrapping.

### 4d. Expanded tool call (ctrl+e)

```
 ↳ tool_name(FULL arguments, not truncated) ✓
   call: X tok/s X tokens | result: Y tokens | duration: Zms

   <result text in ToolCallResultStyle>
```

- **Key change**: In expanded mode, show the **full** `Arguments` string (no `truncate(tc.Arguments, 50)` — use `tc.Arguments` directly).
- One blank line between the expanded command line and the result.
- Show result tokens from `ToolCallEntry.ResultTokens`.
- Show duration from `ToolCallEntry.DurationMs`.

### 4e. Error tool call expanded

```
 ↳ tool_name(FULL args) ✗
   call: X tok/s X tokens | error

   <error text in ToolCallErrorStyle>
```

### 4f. New helper: `replaceNewlines(s string)`

Replaces `\n` with ` ` in the result text displayed **inline** (the short result preview in collapsed mode) so it doesn't wrap to two lines and look ugly. Only applies to the inline preview, not the expanded full result.

### 4g. New style in `styles.go`

```go
ToolCallMetricsStyle = lipgloss.NewStyle().
    Background(lipgloss.Color("233")).
    Foreground(lipgloss.Color("65")) // dark green
    Padding(0, 1)

ToolCallDimStyle = lipgloss.NewStyle().
    Background(lipgloss.Color("233")).
    Foreground(lipgloss.Color("243")) // dark grey for metrics
    Padding(0, 1)
```

---

## Phase 5: Visual — Skip header/body when message has no text but has tool calls

**File: `internal/ui/message.go`**

### 5a. In `RenderMessage()`

```go
hasText := msg.Text != ""
hasThinking := msg.ThinkingText != ""
hasTools := len(msg.ToolCalls) > 0

// If no text and no thinking but has tools: skip header, skip body — just render tool calls
if !hasText && !hasThinking && hasTools {
    b.WriteString(renderToolCallsInline(msg.ToolCalls, bubbleWidth, expanded))
    b.WriteString("\n")
    return b.String()
}
```

- No header, no body with `...`, just render tool calls directly.

### 5b. Remove the `...` fallback when tools are present

```go
body := msg.Text
if body == "" && msg.Role == "assistant" {
    if hasTools {
        // Don't show "..." body — tools will be rendered below
    } else {
        body = "..."
    }
}
```

---

## Phase 6: Streaming — Add tool call display during active streaming

**File: `internal/ui/message.go`** — `RenderStreamingMessage`

### 6a. Currently streaming doesn't render tool calls

When `stream.pendingTools` is populated during streaming, the viewport is updated via `updateViewportContent()` which calls `RenderStreamingMessage`. We need to render pending tool calls in the streaming view too.

### 6b. Add `PendingTools []config.ToolCallEntry` to `StreamingViewData`

Populated from `m.stream.pendingTools` in `updateViewportContent()`.

### 6c. In `RenderStreamingMessage()`

After the thinking and text blocks, render pending tools:

```go
if len(data.PendingTools) > 0 {
    b.WriteString(renderToolCallsInline(data.PendingTools, data.Width, data.Expanded))
}
```

### 6d. In `updateViewportContent()` in `render.go`

Build a `[]config.ToolCallEntry` from `m.stream.pendingTools` and pass it to `StreamingViewData`.

---

## Phase 7: Streaming — Add tool call token metrics to the streaming header

**File: `internal/ui/message.go`** — `StreamingViewData` and `renderStreamingHeader`

### 7a. Add fields to `StreamingViewData`

```go
ToolTokens    int
ToolTokPerSec float64
```

### 7b. In `renderStreamingHeader()`

Add tool tokens to the right-side metadata if `ToolTokens > 0`:

```go
if data.ToolTokens > 0 {
    right = append(right, fmt.Sprintf("tool: %d tokens", data.ToolTokens))
}
```

### 7c. In `updateViewportContent()` in `render.go`

Populate `ToolTokens` from `m.stream.metrics.ToolTokens()`.

---

## Summary of Files to Modify

| File | Changes |
|------|---------|
| `internal/config/session.go` | Add token/timing fields to `ToolCallEntry` |
| `internal/app/metrics.go` | Add `toolChars`, `ToolTokens()`, `addToolChars()`, update `TotalTokens()` |
| `internal/app/stream.go` | Track tool call chars on receive; compute timing/tokens in `executeTools()` |
| `internal/app/chat_session.go` | Update `totalTokens()` to include tool call tokens |
| `internal/ui/styles.go` | Add `ToolCallMetricsStyle` (dark green), `ToolCallDimStyle` (dark grey) |
| `internal/ui/message.go` | Redesign `renderToolCallsInline()` with new format/colors; add newline replacement for inline results; skip header/body when only tools present; update `RenderStreamingMessage()` for pending tools |
| `internal/app/render.go` | Populate new `StreamingViewData` fields (tools, tool tokens) |

## Estimated Complexity

- **Data model**: Low — straightforward struct additions.
- **Metrics tracking**: Medium — need to carefully track timing in `executeTools()` and character counts during streaming.
- **Visual redesign**: Medium — the `renderToolCallsInline()` function needs significant restructuring but the logic is self-contained.
- **Streaming integration**: Medium — requires threading pending tools and metrics through the streaming render path.

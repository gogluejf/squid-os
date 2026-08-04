# Tool Rendering Plan

## Flow Visual

```
User:    "fix the bug in main.go"

Assistant: Let me look at the file first.
           ↳ read_file(/home/goglue/src/squid-os/main.go) ✓ content read (2.4kb)
           ↳ bash(go build ./...) ✓ ./squid-os

Assistant: The bug has been fixed in main.go. I rebuilt the project successfully.
```

## Rules

1. **Assistant message with tool_calls**: render the assistant's text (if any, e.g. "Let me look at the file") followed by one line per tool call with the result appended inline.

2. **Tool call line format**: `↳ tool_name(args) ✓ short_result`
   - `short_result` is the first 80 chars of the tool output, or a success summary
   - If tool errored: `↳ tool_name(args) ✗ error message`
   - Multiple tool calls in the same assistant message each get their own line

3. **No separate "tool result" message block** — the result lives on the tool call line itself. This keeps the conversation tight and readable.

4. **During streaming** (before tools are executed): show `↳ tool_name(args)` without the result. Once tools execute, update the line to include `✓ result`.

5. **Truncated output**: if tool output exceeds 80 chars, show first 80 + `... (full output below)` then indent the full output in a dim style.

## Data Model

In `config.Message`:
- `ToolCalls []ToolCallEntry` — assistant messages that requested tools
- Each `ToolCallEntry` has `ID`, `Name`, `Arguments`, plus `Result string` and `Error string` to store the outcome inline

The app populates `Result`/`Error` after executing tools, so the rendered message can display them on the same line.

## Rendering Implementation

**`internal/ui/message.go`** — in `RenderMessage()`, after thinking block:

```go
if msg.ToolCalls != nil {
    for _, tc := range msg.ToolCalls {
        status := "✓"
        if tc.Error != "" {
            status = "✗"
        }
        line := fmt.Sprintf(" ↳ %s(%s) %s %s", tc.Name, tc.ArgsSummary(), status, tc.ResultSummary())
        b.WriteString(toolCallStyle.Render(line + "\n"))
        if tc.FullResult != "" {
            b.WriteString(toolCallResultStyle.Render("     "+tc.FullResult+"\n"))
        }
    }
}
```

**New style in `internal/ui/styles.go`**:
- `ToolCallStyle` — dim indented text for the arrow line
- `ToolCallResultStyle` — even dimmer, indented more, for overflow output

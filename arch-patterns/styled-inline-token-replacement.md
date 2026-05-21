# Styled Inline Token Replacement

## Intent
When injecting styled tokens (colored words) into plain text in a TUI, the ANSI reset after each token breaks the rest of the line's styling and punches transparent background holes — fix by splitting the line and re-styling every segment.

## Rule
Never mix raw text and styled fragments. After splitting at styled tokens, apply the context style to every segment — raw text gets the full content style, tokens get their color plus the context's background.

## Context
Applies when rendering plain text inside a lipgloss-styled box where the content has a specific background color (e.g., system/internal messages on a colored background), and you need to highlight inline keywords (tool names, labels) with a different foreground color.

## Example

```go
// BAD: ReplaceAllStringFunc leaves raw text unstyled and punches bg holes
line = re.ReplaceAllStringFunc(line, func(m string) string {
    return cyanStyle.Render(m) // no bg, rest of line is raw → broken
})

// GOOD: split and style every segment explicitly
for each match {
    b.WriteString(contentStyle.Render(textBeforeMatch))     // full style
    b.WriteString(cyanFg + contentBg.Render(match))          // color + bg
}
b.WriteString(contentStyle.Render(textAfterLastMatch))       // full style
```

## Anti-Pattern
Using `ReplaceAllStringFunc` (or any string replacement) to inject styled tokens into raw text — it leaves unstyled gaps between tokens and the token's style, lacking a background, punches a transparent hole in the surrounding box.

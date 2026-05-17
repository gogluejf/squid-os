# Lipgloss Style Concatenation Reset

## Intent
Every lipgloss `Render()` call emits ANSI reset codes, so unstyled text between styled segments loses all formatting including background.

## Rule
Apply a style to **every** segment in a concatenated string — labels, separators, and plain text alike. The style must respect the current background context to maintain visual continuity. Choose whichever style matches the segment's role (Label, Param, Dim, etc.) — the critical part is that it shares the same background as surrounding elements.

## Example
```go
// Bad: background drops on the comma
styled.WriteString(t.Style.Label.Render(name))
styled.WriteString(", ")  // background resets to terminal default

// Good: every piece styled with a background-aware style
styled.WriteString(t.Style.Label.Render(name))
styled.WriteString(t.Style.Param.Render(", "))  // same background, different fg
```

## Anti-Pattern
- Concatenating `styled + unstyled + styled` without styling the middle piece.
- Assuming styles carry over across string boundaries (they don't — ANSI resets always).
- Hardcoding a specific style (e.g., "always use Dim") — the rule is about background continuity, not the style name.

# Rich Domain Data

## Intent
Simplify consumers by providing ready-to-use objects instead of primitives that require assembly.

## Rule
When a provider exports data, it should return the final, rich object ready for use by the consumer. The consumer should not have to assemble the object from raw primitives (e.g., strings, ints) or stitch together multiple disparate vars from a shared namespace.

If a consumer needs 3-4 related style/color values, package them into a single struct builder function. The consumer calls one function and gets everything it needs.

## Example
```go
// Provider — single function returns everything a renderer needs
type StyleLabel struct {
    Label   lipgloss.Style
    Param   lipgloss.Style
    Dim     lipgloss.Style
    Content lipgloss.Style
    Error   lipgloss.Style
    Bg      string
    Fg      string
}

func SystemStyleLabel() StyleLabel { ... }

// Consumer — one call, no assembly
func render(msg) {
    s := style.SystemStyleLabel()
    s.Label.Render(msg.Name)
    s.Dim.Render(msg.Stats)
    s.Content.Render(msg.Body)
}
```

## Anti-Pattern
The provider scatters dozens of standalone vars (`SystemLabel`, `SystemParam`, `CanvasStatInline`, `TextMuted`) across a shared file, and every consumer must manually pick and assemble them — duplicating the same combination logic in each renderer.

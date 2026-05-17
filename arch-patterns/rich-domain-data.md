# Rich Domain Data

## Intent
Simplify consumers by providing ready-to-use objects instead of primitives that require assembly.

## Rule
When a provider exports data, it should return the final, rich object ready for use by the consumer. The consumer should not have to assemble the object from raw primitives (e.g., strings, ints).

## Example
```go
// Provider
type StyleLabel struct {
    Label lipgloss.Style // Pre-built object
    Param lipgloss.Style
}

// Consumer (Simple)
func Render(s StyleLabel) {
    s.Label.Render("Name")
}
```

## Anti-Pattern
The provider returns raw configuration strings (`Color: "110"`) and forces every consumer to reconstruct the complex object (`lipgloss.NewStyle().Color(...)`) manually.

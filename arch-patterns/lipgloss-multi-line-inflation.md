# Lipgloss Multi-Line Inflation

## Intent
Avoid rendering multi-line text directly to `lipgloss.Style.Render()` to prevent massive trailing whitespace inflation.

## Rule
Never pass multi-line strings to `Style.Render()`. Always split on `\n`, style each line individually, then rejoin.

## Context
Applies when passing multi-line content through lipgloss styles with width padding (`.Width()`, `.Padding()`, or `.Align()`). Common in terminal UI rendering tool results, file contents, or command output.

## Example
```go
// Per-line rendering — safe
func renderPerLine(s string, st lipgloss.Style) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return strings.Join(lines, "\n")
}
```

## Anti-Pattern
Passing multi-line strings directly to `Style.Render()` with any width/alignment set. Lipgloss's `alignTextHorizontal` finds the widest line and pads every other line (including short ones) to match, turning a single trailing `\n` into thousands of spaces when the block contains one disproportionately long line.

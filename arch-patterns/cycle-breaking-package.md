# Cycle-Breaking Package

## Intent
Resolve circular import dependencies between packages that require shared types.

## Rule
If Package A imports Package B, and Package B imports Package A (or transitively leads back to A), extract the shared types/interfaces into a third, independent Package C. Both A and B must import C.

## Example
```go
// package style (New independent package)
type StyleLabel struct { ... }

// package tools
import "style"
type Tool struct { Style style.StyleLabel }

// package ui
import "style"
func Render(s style.StyleLabel) { ... }
```

## Anti-Pattern
Defining a shared type in Package B and making Package A import Package B, while Package B needs a function or type from Package A.

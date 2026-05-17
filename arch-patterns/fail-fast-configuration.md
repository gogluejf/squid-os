# Fail-Fast Configuration

## Intent
Ensure critical configuration is present by rejecting incomplete setup immediately.

## Rule
For essential configuration fields, do not use silent fallback defaults. If a required field is missing or empty, the application should panic or return a hard error at initialization. Fallbacks hide bugs.

## Example
```go
// Provider
func GetStyle(t Tool) Style {
    // If Style is zero-valued, it crashes or panics later, exposing the definition error.
    return t.Style 
}
```

## Anti-Pattern
Checking if a value is empty and silently assigning a default value (`if color == "" { color = "default" }`). This masks the fact that the configuration is missing in the first place.

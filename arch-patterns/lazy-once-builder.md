# Lazy Once Builder

## Intent
Provide a clean, reusable API for lazy initialization of expensive objects using `sync.Once`.

## Rule
When a function returns an expensive-to-construct value that is identical across calls (e.g., compiled styles, pre-built templates), wrap the construction in `sync.Once` behind a simple getter function. Use a shared helper struct to avoid boilerplate.

## Example
```go
type cachedBuilder struct {
    once   sync.Once
    result T
}

func (c *cachedBuilder) Get(fn func() T) T {
    c.once.Do(func() { c.result = fn() })
    return c.result
}

var _myStyle cachedBuilder

func GetMyStyle() Style {
    return _myStyle.Get(func() Style {
        return BuildExpensiveStyle(...)
    })
}
```

## Anti-Pattern
Inlining `sync.Once` in every function (boilerplate), or building expensive objects eagerly at init when they might never be used.

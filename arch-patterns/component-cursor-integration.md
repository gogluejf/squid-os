# Component Cursor Integration

## Intent
Integrate `bubbles/cursor` into a custom component with correct blink lifecycle, character synchronization, and ANSI-safe rendering.

## Rule
Treat `cursor.Model` as a stateful widget, not a decorative glyph. Call `Focus()` on init, set the char with `SetChar()`, reset blink on input, and never wrap `cursor.View()` inside `lipgloss.Render()`.

## Context
When building a custom input component (question, prompt, inline editor) that needs a blinking cursor over placeholder text or trailing typed input, and the component renders inside a lipgloss-styled block.

## Example
```go
type MyComponent struct {
    input       string
    cur         cursor.Model
    initialized bool
}

func (c *MyComponent) Init(any) {
    if c.initialized {
        return // prevent re-init from orphaning blink loop
    }
    c.initialized = true
    c.cur = cursor.New()
    c.cur.Focus() // required: cursor starts unfocused/invisible
    c.cur.Style = lipgloss.NewStyle().
        Foreground(textColor).Background(bgColor)
    c.cur.TextStyle = lipgloss.NewStyle().
        Foreground(mutedColor).Background(bgColor)
    c.syncChar()
}

func (c *MyComponent) syncChar() {
    if c.input == "" {
        c.cur.SetChar(string([]rune(placeholder)[:1])) // cursor overlays first placeholder char
    } else {
        c.cur.SetChar(" ") // cursor trails after typed text
    }
}

func (c *MyComponent) resetBlink() tea.Cmd {
    c.syncChar()
    c.cur.Blink = false // show cursor solid after keystroke
    return c.cur.BlinkCmd()
}

func (c *MyComponent) Update(msg tea.Msg, ctx any) tea.Cmd {
    if _, ok := msg.(tea.KeyMsg); ok {
        return nil // keys handled by HandleKey
    }
    newCur, cmd := c.cur.Update(msg)
    c.cur = newCur
    return cmd
}

func (c *MyComponent) Render() string {
    if c.input != "" {
        // cursor trails after text
        return styledLabel() + styledText(c.input) + c.cur.View()
    }
    // cursor IS the first placeholder char, rest follows
    return styledLabel() + c.cur.View() + styledPlaceholder(placeholder[1:])
}
```

## Anti-Pattern
- **Wrapping `cursor.View()` in `lipgloss.Render()`** — lipgloss adds its own ANSI escapes that overwrite the cursor's Inline styling, making the cursor invisible or breaking its background fill.
- **Setting cursor char to `" "` when showing placeholder** — a space with no visible styling is invisible. The char should be the actual placeholder character so the cursor visually sits *on* the text.
- **Skipping `Focus()`** — cursor starts unfocused and produces no output. Without `Focus()`, the cursor is never visible.
- **Re-initializing the cursor on every render/layout pass** — creates a new cursor with a fresh blink context, orphaning the old blink ticker and causing erratic behavior. Guard with an `initialized` flag.
- **Not resetting blink after input** — without `Blink = false` + `BlinkCmd()`, the cursor stays in whatever blink phase it was in, making it look disconnected from user action.

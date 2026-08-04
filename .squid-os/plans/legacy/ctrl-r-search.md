# Ctrl+R Reverse Search Through Prompt History

## Overview

Add a reverse-search feature (like bash's Ctrl+R) to search through previously sent messages in the prompt history.

## User Flow

1. **Press Ctrl+R** in the textarea → enters search mode, shows a search overlay at the bottom, shows notification "search prompt history, esc to exit [1/N]"
2. **Type search string** → filters history entries in real-time, shows match count
3. **Press Ctrl+R again** → cycles to next matching entry (forward cycle through matches)
4. **Press Enter** → inserts the selected history entry into the textarea, exits search mode
5. **Press Escape** → exits search mode without inserting

## Architecture

```
┌─────────────────────────────────────┐
│  Viewport (chat messages)           │
├─────────────────────────────────────┤
│  Search Overlay: "R-search: foo"    │  ← New overlay (1 line)
├─────────────────────────────────────┤
│  Textarea (input)                   │
└─────────────────────────────────────┘
```

## Implementation Files

| File | Changes |
|------|---------|
| [`internal/app/keymap.go`](internal/app/keymap.go) | Add `HistorySearch` key binding (`ctrl+r`) |
| [`internal/app/modes.go`](internal/app/modes.go) | Add `ModeHistorySearch` constant |
| [`internal/app/app.go`](internal/app/app.go) | Add `historySearch` field to Model |
| [`internal/ui/history-search.go`](internal/ui/history-search.go) | **New file** - `HistorySearchOverlay` type with filter, match index, rendering |
| [`internal/app/input.go`](internal/app/input.go) | Handle `ctrl+r` in `handleChatKey`, history search mode key routing |
| [`internal/app/update.go`](internal/app/update.go) | Update `recalcLayout()` for search overlay height |
| [`internal/ui/footer.go`](internal/ui/footer.go) | Add `ctrl+r` search hint |
| [`internal/ui/help.go`](internal/ui/help.go) | Add search help text |

## Detailed Changes

### 1. [`keymap.go`](internal/app/keymap.go:5) - Add HistorySearch Binding

```go
type keyMap struct {
    Destroy        key.Binding
    UndoDestroy    key.Binding
    Send           key.Binding
    Cancel         key.Binding
    Help           key.Binding
    ExpandThinking key.Binding
    Save           key.Binding
    Load           key.Binding
    Model          key.Binding
    NewSession     key.Binding
    Incognito      key.Binding
    Quit           key.Binding
    Up             key.Binding
    Down           key.Binding
    Escape         key.Binding
    Tab            key.Binding
    ScrollUp       key.Binding
    ScrollDown     key.Binding
    PageUp         key.Binding
    PageDown       key.Binding
    HistorySearch  key.Binding  // NEW: ctrl+r for reverse search through prompt history
}

var keys = keyMap{
    // ... existing bindings ...
    HistorySearch: key.NewBinding(
        key.WithKeys("ctrl+r"),
        key.WithHelp("ctrl+r", "reverse search history"),
    ),
}
```

### 2. [`modes.go`](internal/app/modes.go:6) - Add History Search Mode

```go
const (
    ModeChat          Mode = iota // Default: textarea focused
    ModeStreaming                  // Inference active, input disabled
    ModeModelPicker               // Model selection
    ModeHelp                       // Help overlay
    ModeFilePicker                 // File path completion for /image, /system
    ModeSessionPicker              // Session list for /load
    ModeSavePrompt                 // Save session name input
    ModeHistorySearch              // NEW: Reverse search through prompt history
)
```

Add to `String()` method:
```go
case ModeHistorySearch:
    return "history-search"
```

### 3. [`ui/history-search.go`](internal/ui/history-search.go) - New HistorySearchOverlay Type

**New file** with the following structure:

```go
package ui

import (
    "fmt"
    "strings"
    "github.com/charmbracelet/lipgloss"
)

// HistorySearchOverlay handles the reverse-search overlay state and rendering
type HistorySearchOverlay struct {
    Filter   string
    MatchIdx int       // Index within filtered results
    Visible  bool
    Items    []string  // The items to search through (history entries)
}

func NewHistorySearchOverlay() HistorySearchOverlay
func (hs *HistorySearchOverlay) FilteredItems() []string  // Filter items by text (case-insensitive substring match)
func (hs *HistorySearchOverlay) SelectedText() string     // Return currently selected item
func (hs *HistorySearchOverlay) NextMatch()               // Cycle to next match (forward)
func (hs *HistorySearchOverlay) Reset()                   // Clear state
func (hs *HistorySearchOverlay) Render(width int) string  // Render the search line
func (hs *HistorySearchOverlay) RenderHeight() int        // Always 1
```

**Render output format:**
```
R-search: foo
```
Where `foo` is the filter text.

**Styling:** Use existing palette colors similar to command palette (paletteBg = lipgloss.Color("235")).

### 4. [`app.go`](internal/app/app.go:17) - Add History Search Field to Model

Add to Model struct:
```go
// History search overlay
historySearch ui.HistorySearchOverlay
```

Initialize in `New()`:
```go
historySearch: ui.NewHistorySearchOverlay(),
```

### 5. [`input.go`](internal/app/input.go) - Handle History Search Mode

#### Add mode routing in `handleKey()`:
```go
case ModeHistorySearch:
    return m.handleHistorySearchKey(msg)
```

#### Add Ctrl+R handler in `handleChatKey()`:
```go
case key.Matches(msg, keys.HistorySearch):
    if m.mode == ModeHistorySearch {
        // Ctrl+R while in history search mode → cycle to next match
        m.historySearch.NextMatch()
        return m, nil
    }
    // Enter history search mode
    m.mode = ModeHistorySearch
    m.historySearch = ui.NewHistorySearchOverlay()
    m.historySearch.Items = m.history.Entries
    m.historySearch.Visible = true
    m.recalcLayout()
    // Show notification with match count
    matches := len(m.historySearch.FilteredItems())
    if matches > 0 {
        (&m).setNotification(ui.NotificationInfo, fmt.Sprintf("search prompt history, esc to exit [%d/%d]", 1, matches))
    } else {
        (&m).setNotification(ui.NotificationInfo, "search prompt history, esc to exit [0/0]")
    }
    return m, nil
```

#### New `handleHistorySearchKey()` method:
```go
func (m Model) handleHistorySearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch {
    case key.Matches(msg, keys.Escape):
        m.mode = ModeChat
        m.historySearch.Reset()
        m.recalcLayout()
        return m, nil
    
    case key.Matches(msg, keys.HistorySearch):
        // Ctrl+R → cycle to next match
        m.historySearch.NextMatch()
        // Update notification with current match position
        matches := len(m.historySearch.FilteredItems())
        if matches > 0 {
            (&m).setNotification(ui.NotificationInfo, fmt.Sprintf("search prompt history, esc to exit [%d/%d]", m.historySearch.MatchIdx+1, matches))
        }
        return m, nil
    
    case key.Matches(msg, keys.Send):
        // Enter → confirm selection and insert text into textarea
        if item := m.historySearch.SelectedText(); item != "" {
            m.textarea.SetValue(item)
        }
        m.mode = ModeChat
        m.historySearch.Reset()
        m.recalcLayout()
        return m, nil
    
    default:
        // Handle character input for filter text
        if r, ok := msg.(tea.KeyRune); ok {
            m.historySearch.Filter += string(r)
            m.historySearch.MatchIdx = 0
            // Update notification with new match count
            matches := len(m.historySearch.FilteredItems())
            if matches > 0 {
                (&m).setNotification(ui.NotificationInfo, fmt.Sprintf("search prompt history, esc to exit [%d/%d]", 1, matches))
            } else {
                (&m).setNotification(ui.NotificationInfo, "search prompt history, esc to exit [0/0]")
            }
        }
        return m, nil
    }
}
```

### 6. [`update.go`](internal/app/update.go:70) - Update Layout

In `recalcLayout()`:
```go
const historySearchOverlayHeight = 1  // NEW constant

overlayHeight := 0
if m.cmdPalette.Visible {
    overlayHeight = m.cmdPalette.RenderHeight()
} else if m.mode == ModeHistorySearch {
    overlayHeight = historySearchOverlayHeight  // NEW
} else {
    // ... existing picker cases ...
}
```

### 7. [`render.go`](internal/app/render.go) - Render History Search Overlay

Add history search overlay rendering between viewport and input:
```go
if m.mode == ModeHistorySearch && m.historySearch.Visible {
    sections = append(sections, m.historySearch.Render(m.width))
}
```

### 8. [`footer.go`](internal/ui/footer.go:40) - Add Search Hint

Add to the footer line 1 left section:
```go
left += FooterDimStyle.Render("  ") +
    FooterKeyStyle.Render("ctrl+r") + FooterDimStyle.Render(" search")
```

### 9. [`help.go`](internal/ui/help.go) - Add Search Help Text

Add to the help overlay:
```
` + FooterKeyStyle.Render("ctrl+r") + `                Reverse search history
```

## Key Design Decisions

1. **Ctrl+R**: Using Ctrl+R as requested. Since Bubble Tea runs in raw mode, the terminal won't intercept it.

2. **Naming Convention**: Using `HistorySearch` throughout (keymap, mode, overlay, files) to distinguish from potential future search features.

3. **Search Scope**: Only searches through `m.history.Entries` (previously sent messages), not the current session messages.

4. **Single Line Overlay**: The search overlay is exactly 1 line (like the command palette header), showing `R-search: <filter>`.

5. **New Mode**: Requires `ModeHistorySearch` because:
   - Need to intercept all key input during search
   - The textarea should not receive normal input
   - Layout calculation needs to account for the overlay

6. **Cycle Behavior**: Ctrl+R cycles forward through matches. If there are 3 matches for "foo", pressing Ctrl+R cycles: match 0 → match 1 → match 2 → match 0 → ...

7. **Filtering**: Case-insensitive substring match (not prefix match like command palette).

8. **Notification**: Shows match count `[current/total]` and instructions "search prompt history, esc to exit"

9. **Text Insertion**: The found text appears in the textarea when Enter is pressed (via `m.textarea.SetValue(item)`).

## Notification Behavior

| Event | Notification Message |
|-------|---------------------|
| Enter search mode | "search prompt history, esc to exit [1/N]" |
| Cycle to next match | "search prompt history, esc to exit [X/N]" |
| Type filter character | "search prompt history, esc to exit [1/N]" |
| Exit (Enter/Esc) | Notification cleared |

## Testing Checklist

- [ ] Ctrl+R enters history search mode
- [ ] Typing filters history entries in real-time
- [ ] Ctrl+R cycles through matches
- [ ] Enter inserts selected text into textarea and exits search mode
- [ ] Escape exits search mode without changes
- [ ] Footer shows ctrl+r hint
- [ ] Help overlay shows ctrl+r binding
- [ ] Layout correctly accounts for search overlay height
- [ ] Works when textarea is focused
- [ ] Notification shows match count [X/N]
- [ ] Notification shows "search prompt history, esc to exit"
- [ ] No matches shows appropriate message
- [ ] Found text appears in textarea when confirmed

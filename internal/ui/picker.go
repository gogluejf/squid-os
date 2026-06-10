package ui

import (
	"strings"

	"squid-os/internal/style"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PickerDisplayMode controls how picker items are rendered.
type PickerDisplayMode int

const (
	ModeSingleCol  PickerDisplayMode = iota // just Label (file picker)
	ModeLabelMeta                           // Label + Meta (session: name + date)
	ModeLabelDesc                           // Label + Description (skill: name + description)
	ModeLabelValue                          // Label + Value (model: name + model ID)
)

// PickerMatchMode controls how filtering matches items.
type PickerMatchMode int

const (
	MatchSubstring PickerMatchMode = iota // substring match (default, used by data pickers)
	MatchPrefix                           // prefix match (used by command palette)
)

// PickerItem is a typed row in a Picker, carrying all fields needed for display and selection.
type PickerItem struct {
	Label       string // left column / primary text
	Meta        string // right column short (date, context length, etc.)
	Description string // extended description (truncated in display)
	Value       string // internal value used for matching and default selection
}

// Picker is a reusable, self-contained picker component with typed items,
// key handling, filtering, configurable display modes, and optional callbacks.
// Callbacks receive ctx (typically *Model) so callers can mutate live state.
// Set DefaultValue to pre-select an item; call Init(ctx) after construction to
// resolve the default and fire OnSelectionChange.
type Picker struct {
	Title             string
	Items             []PickerItem
	Filter            string
	Selected          int
	DefaultValue      string // declarative default: match by Value or Label
	DisplayMode       PickerDisplayMode
	MatchMode         PickerMatchMode
	OnSelectionChange func(int, PickerItem, any)    // optional: fires on navigation/filter
	OnConfirm         func(PickerItem, any) tea.Cmd // optional: fires on Enter
	OnCancel          func(any) tea.Cmd             // optional: fires on Esc
}

// FilteredItems returns items matching the current filter (case-insensitive on Label, Meta, and Value).
func (p *Picker) FilteredItems() []PickerItem {
	if p.Filter == "" {
		return p.Items
	}
	f := strings.ToLower(p.Filter)
	var result []PickerItem
	for _, item := range p.Items {
		labelLower := strings.ToLower(item.Label)
		metaLower := strings.ToLower(item.Meta)
		valueLower := strings.ToLower(item.Value)
		switch p.MatchMode {
		case MatchPrefix:
			if strings.HasPrefix(labelLower, f) || strings.HasPrefix(metaLower, f) || strings.HasPrefix(valueLower, f) {
				result = append(result, item)
			}
		default:
			if strings.Contains(labelLower, f) || strings.Contains(metaLower, f) || strings.Contains(valueLower, f) {
				result = append(result, item)
			}
		}
	}
	return result
}

// SelectedItem returns the currently selected PickerItem, or a zero value if out of range.
func (p *Picker) SelectedItem() PickerItem {
	items := p.FilteredItems()
	if p.Selected >= 0 && p.Selected < len(items) {
		return items[p.Selected]
	}
	return PickerItem{}
}

// PickerMaxItems is the maximum number of rows rendered.
const PickerMaxItems = 15

// RenderHeight returns the exact number of terminal lines that Render() will output.
func (p *Picker) RenderHeight() int {
	h := 1 // heading line
	if p.Filter != "" {
		h++ // filter hint line
	}
	items := p.FilteredItems()
	if len(items) == 0 {
		h++ // "No matches" line
	} else {
		count := len(items)
		if count > PickerMaxItems {
			count = PickerMaxItems
		}
		h += count
	}
	return h
}

// HandleKey processes a key message and returns a tea.Cmd if a callback fires.
// Navigation, filtering, Enter (OnConfirm), and Esc (OnCancel) are all handled.
// OnCancel/backspace-empty cancel fires the OnCancel callback; Enter fires OnConfirm.
// Both callbacks receive ctx for live state mutation.
func (p *Picker) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	s := msg.String()

	// Navigation
	if s == "up" {
		if p.Selected > 0 {
			p.Selected--
			if p.OnSelectionChange != nil {
				items := p.FilteredItems()
				if p.Selected < len(items) {
					p.OnSelectionChange(p.Selected, items[p.Selected], ctx)
				}
			}
		}
		return nil
	}

	if s == "down" || s == "tab" {
		items := p.FilteredItems()
		if p.Selected < len(items)-1 {
			p.Selected++
			if p.OnSelectionChange != nil && p.Selected < len(items) {
				p.OnSelectionChange(p.Selected, items[p.Selected], ctx)
			}
		}
		return nil
	}

	// Confirm
	if s == "enter" {
		if p.OnConfirm != nil {
			return p.OnConfirm(p.SelectedItem(), ctx)
		}
		return nil
	}

	// Cancel
	if s == "esc" {
		if p.OnCancel != nil {
			return p.OnCancel(ctx)
		}
		return nil
	}

	// Filter: single character
	if len(s) == 1 && isPrintable(s) {
		p.Filter += s
		p.Selected = 0
		if p.OnSelectionChange != nil {
			items := p.FilteredItems()
			if len(items) > 0 {
				p.OnSelectionChange(0, items[0], ctx)
			}
		}
		return nil
	}

	// Backspace
	if s == "backspace" {
		if len(p.Filter) > 0 {
			p.Filter = p.Filter[:len(p.Filter)-1]
			p.Selected = 0
			if p.OnSelectionChange != nil {
				items := p.FilteredItems()
				if len(items) > 0 {
					p.OnSelectionChange(0, items[0], ctx)
				}
			}
		} else {
			// Backspace with empty filter = cancel
			if p.OnCancel != nil {
				return p.OnCancel(ctx)
			}
		}
		return nil
	}

	return nil
}

// Init resolves the DefaultValue, sets Selected, and fires OnSelectionChange
// for the initial selection. Call after constructing the Picker and before
// the first key event. Idempotent — safe to call multiple times.
func (p *Picker) Init(ctx any) {
	if p.DefaultValue == "" {
		return
	}
	m := strings.ToLower(p.DefaultValue)
	for i, item := range p.Items {
		if strings.ToLower(item.Value) == m || strings.ToLower(item.Label) == m {
			p.Selected = i
			p.DefaultValue = "" // consume so it only fires once
			if p.OnSelectionChange != nil {
				items := p.FilteredItems()
				if p.Selected < len(items) {
					p.OnSelectionChange(p.Selected, items[p.Selected], ctx)
				}
			}
			return
		}
	}
	p.DefaultValue = "" // consumed even if not found
}

// Render renders the picker with the configured display mode.
func (p *Picker) Render(width int) string {
	items := p.FilteredItems()

	// Determine visible window around selection (max PickerMaxItems rows).
	var visible []PickerItem
	if len(items) > PickerMaxItems {
		start := p.Selected - 7
		if start < 0 {
			start = 0
		}
		end := start + PickerMaxItems
		if end > len(items) {
			end = len(items)
		}
		// Also map global indices to a local offset within the visible slice.
		visible = items[start:end]
	} else {
		visible = items
	}

	var b strings.Builder
	b.WriteString(style.HeadingStyle.Render("  "+p.Title) + "\n")

	if p.Filter != "" {
		b.WriteString(style.CommandDescStyle.Render("  filter: "+p.Filter) + "\n")
	}

	if len(items) == 0 {
		b.WriteString(style.CommandDescStyle.Render("  No matches"))
		return lipgloss.NewStyle().
			Background(lipgloss.Color(style.P.BgFooter)).
			Width(width).
			Render(strings.TrimRight(b.String(), "\n"))
	}

	// Calculate label column width from visible items.
	maxLabel := 0
	for _, item := range visible {
		if l := len(item.Label); l > maxLabel {
			maxLabel = l
		}
	}
	// Minimum label width of 8 to avoid crushing on very short labels.
	if maxLabel < 8 {
		maxLabel = 8
	}
	// Reserve at least 2 chars gap between columns.
	labelWidth := maxLabel + 2

	// Styles
	bgFooter := lipgloss.Color(style.P.BgFooter)
	bgSelected := lipgloss.Color(style.P.BgSelected)

	normalLabel := lipgloss.NewStyle().Background(bgFooter).Foreground(lipgloss.Color(style.P.TextMuted)).Width(labelWidth)
	normalRight := lipgloss.NewStyle().Background(bgFooter).Foreground(lipgloss.Color(style.P.TextMuted))
	normalRow := lipgloss.NewStyle().Background(bgFooter).Width(width)

	selLabel := lipgloss.NewStyle().Background(bgSelected).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true).Width(labelWidth)
	selRight := lipgloss.NewStyle().Background(bgSelected).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true)
	selRow := lipgloss.NewStyle().Background(bgSelected).Width(width)

	// Map from visible-index to global index for selection check.
	var globalStart int
	if len(items) > PickerMaxItems {
		globalStart = p.Selected - 7
		if globalStart < 0 {
			globalStart = 0
		}
	}

	for i, item := range visible {
		globalIdx := globalStart + i
		isSelected := globalIdx == p.Selected

		var rendered string

		switch p.DisplayMode {
		case ModeSingleCol:
			content := "  " + item.Label
			if isSelected {
				rendered = selRow.Render(content)
			} else {
				rendered = normalRow.Render(content)
			}

		default:
			leftStyle := normalLabel
			rightStyle := normalRight
			rowStyle := normalRow
			if isSelected {
				leftStyle = selLabel
				rightStyle = selRight
				rowStyle = selRow
			}

			switch p.DisplayMode {
			case ModeLabelMeta:
				right := item.Meta
				rendered = rowStyle.Render(leftStyle.Render("  "+padRight(item.Label, maxLabel)) + rightStyle.Render(right))

			case ModeLabelDesc:
				// Truncate description to remaining width.
				avail := width - maxLabel - 4 // "  " + label + gap
				if avail < 4 {
					avail = 4
				}
				desc := truncateRight(item.Description, avail)
				rendered = rowStyle.Render(leftStyle.Render("  "+padRight(item.Label, maxLabel)) + rightStyle.Render(desc))

			case ModeLabelValue:
				right := item.Meta
				if right == "" {
					right = item.Value
				}
				rendered = rowStyle.Render(leftStyle.Render("  "+padRight(item.Label, maxLabel)) + rightStyle.Render(right))

			default:
				// Fallback to single col.
				content := "  " + item.Label
				rendered = rowStyle.Render(content)
			}
		}

		b.WriteString(rendered + "\n")
	}

	return lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgFooter)).
		Width(width).
		Render(strings.TrimRight(b.String(), "\n"))
}

// padRight pads s to the given width with spaces, or truncates if longer.
func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

// truncateRight truncates s to maxLen by replacing the end with "…".
func truncateRight(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return s[:maxLen-1] + "…"
}

// isPrintable returns true if the string is a single printable ASCII character.
func isPrintable(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		(c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~') || c == ' '
}

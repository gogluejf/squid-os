package ui

import (
	"strings"

	"squid-os/internal/style"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CommandInfo describes a slash command for the palette
type CommandInfo struct {
	Name        string
	Description string
}

// AllCommands is the full command list
var AllCommands = []CommandInfo{
	{Name: "model", Description: "Select inference model"},
	{Name: "skill", Description: "Select active skill"},
	{Name: "thinking", Description: "Toggle thinking mode (on/off)"},
	{Name: "auth-mode", Description: "Cycle authorization mode (auto/ask-on-write/ask-for-all)"},
	{Name: "image", Description: "Attach image to next message"},
	{Name: "save", Description: "Save current session"},
	{Name: "load", Description: "Load a saved session"},
	{Name: "clear", Description: "Clear chat and start fresh"},
	{Name: "system", Description: "Load system prompt"},
	{Name: "exit", Description: "Exit squid-os"},
	{Name: "help", Description: "Show help"},
}

// CommandPalette holds the state for the slash command overlay
type CommandPalette struct {
	Filter   string
	Selected int
	Visible  bool
	Items    []CommandInfo
}

func NewCommandPalette() CommandPalette {
	return CommandPalette{
		Items: AllCommands,
	}
}

// FilteredItems returns commands matching the current filter
func (cp *CommandPalette) FilteredItems() []CommandInfo {
	if cp.Filter == "" {
		return cp.Items
	}
	f := strings.ToLower(cp.Filter)
	var result []CommandInfo
	for _, item := range cp.Items {
		if strings.HasPrefix(strings.ToLower(item.Name), f) {
			result = append(result, item)
		}
	}
	return result
}

// MoveUp moves selection up
func (cp *CommandPalette) MoveUp() {
	if cp.Selected > 0 {
		cp.Selected--
	}
}

// MoveDown moves selection down
func (cp *CommandPalette) MoveDown() {
	items := cp.FilteredItems()
	if cp.Selected < len(items)-1 {
		cp.Selected++
	}
}

// SelectedCommand returns the currently selected command name, or empty
func (cp *CommandPalette) SelectedCommand() string {
	items := cp.FilteredItems()
	if cp.Selected >= 0 && cp.Selected < len(items) {
		return items[cp.Selected].Name
	}
	return ""
}

// Reset clears the palette state
func (cp *CommandPalette) Reset() {
	cp.Filter = ""
	cp.Selected = 0
	cp.Visible = false
}

// maxCmdItems is the maximum number of command palette rows ever rendered.
const maxCmdItems = 10

// RenderHeight returns the exact number of terminal lines that Render() will output.
func (cp *CommandPalette) RenderHeight() int {
	n := len(cp.FilteredItems())
	if n == 0 {
		return 1 // "No matching commands" line
	}
	if n > maxCmdItems {
		n = maxCmdItems
	}
	return n
}

// palette background colours
var paletteBg = lipgloss.Color(style.P.BgFooter)
var paletteSelectedBg = lipgloss.Color(style.P.BgSelected)

// Render renders the command palette
func (cp *CommandPalette) Render(width int) string {
	items := cp.FilteredItems()
	if len(items) == 0 {
		return lipgloss.NewStyle().
			Background(paletteBg).
			Width(width).
			Render(lipgloss.NewStyle().Background(paletteBg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("  No matching commands"))
	}
	// Cap rendered items to match RenderHeight.
	if len(items) > maxCmdItems {
		items = items[:maxCmdItems]
	}

	// Fixed name column width: longest command "/thinking" = 11 chars + 2 gap = 13.
	const nameColWidth = 13

	// Inline styles that carry the background so ANSI resets don't punch holes.
	normalNameStyle := lipgloss.NewStyle().Background(paletteBg).Foreground(lipgloss.Color(style.P.TextMuted)).Width(nameColWidth)
	normalDescStyle := lipgloss.NewStyle().Background(paletteBg).Foreground(lipgloss.Color(style.P.TextMuted))
	normalRowStyle := lipgloss.NewStyle().Background(paletteBg).Width(width)

	selNameStyle := lipgloss.NewStyle().Background(paletteSelectedBg).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true).Width(nameColWidth)
	selDescStyle := lipgloss.NewStyle().Background(paletteSelectedBg).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true)
	selRowStyle := lipgloss.NewStyle().Background(paletteSelectedBg).Width(width)

	var rows []string
	for i, item := range items {
		name := "  /" + item.Name
		desc := item.Description
		if i == cp.Selected {
			rows = append(rows, selRowStyle.Render(selNameStyle.Render(name)+selDescStyle.Render(desc)))
		} else {
			rows = append(rows, normalRowStyle.Render(normalNameStyle.Render(name)+normalDescStyle.Render(desc)))
		}
	}

	return strings.Join(rows, "\n")
}

// PickerAction represents the result of a key interaction with the Picker.
type PickerAction int

const (
	ActionNone   PickerAction = iota // no action
	ActionSelect                     // user pressed Enter to confirm
	ActionCancel                     // user pressed Esc to cancel
)

// PickerDisplayMode controls how picker items are rendered.
type PickerDisplayMode int

const (
	ModeSingleCol  PickerDisplayMode = iota // just Label (file picker)
	ModeLabelMeta                           // Label + Meta (session: name + date)
	ModeLabelDesc                           // Label + Description (skill: name + description)
	ModeLabelValue                          // Label + Value (model: name + model ID)
)

// PickerItem is a typed row in a Picker, carrying all fields needed for display and selection.
type PickerItem struct {
	Label       string // left column / primary text
	Meta        string // right column short (date, context length, etc.)
	Description string // extended description (truncated in display)
	Value       string // internal value used for matching and default selection
}

// Picker is a reusable, self-contained picker component with typed items,
// key handling, filtering, configurable display modes, and selection callbacks.
type Picker struct {
	Title             string
	Items             []PickerItem
	Filter            string
	Selected          int
	DefaultMatch      string
	DisplayMode       PickerDisplayMode
	OnSelectionChange func(int, PickerItem)
}

// FilteredItems returns items matching the current filter (case-insensitive on Label and Meta).
func (p *Picker) FilteredItems() []PickerItem {
	if p.Filter == "" {
		return p.Items
	}
	f := strings.ToLower(p.Filter)
	var result []PickerItem
	for _, item := range p.Items {
		if strings.Contains(strings.ToLower(item.Label), f) ||
			strings.Contains(strings.ToLower(item.Meta), f) {
			result = append(result, item)
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

// HandleKey processes a key message and returns the resulting action.
// Handles navigation (Up/Down/Tab), filtering (single-char input, backspace),
// Enter for selection, and Esc for cancellation.
func (p *Picker) HandleKey(msg tea.KeyMsg) PickerAction {
	s := msg.String()

	// Navigation
	if s == "up" {
		if p.Selected > 0 {
			p.Selected--
			if p.OnSelectionChange != nil {
				items := p.FilteredItems()
				if p.Selected < len(items) {
					p.OnSelectionChange(p.Selected, items[p.Selected])
				}
			}
		}
		return ActionNone
	}

	if s == "down" || s == "tab" {
		items := p.FilteredItems()
		if p.Selected < len(items)-1 {
			p.Selected++
			if p.OnSelectionChange != nil && p.Selected < len(items) {
				p.OnSelectionChange(p.Selected, items[p.Selected])
			}
		}
		return ActionNone
	}

	// Confirm / Cancel
	if s == "enter" {
		return ActionSelect
	}
	if s == "esc" {
		return ActionCancel
	}

	// Filter: single character or backspace
	if len(s) == 1 && isPrintable(s) {
		p.Filter += s
		p.Selected = 0
		if p.OnSelectionChange != nil {
			items := p.FilteredItems()
			if len(items) > 0 {
				p.OnSelectionChange(0, items[0])
			}
		}
		return ActionNone
	}
	if s == "backspace" && len(p.Filter) > 0 {
		p.Filter = p.Filter[:len(p.Filter)-1]
		p.Selected = 0
		if p.OnSelectionChange != nil {
			items := p.FilteredItems()
			if len(items) > 0 {
				p.OnSelectionChange(0, items[0])
			}
		}
		return ActionNone
	}

	return ActionNone
}

// SetDefaultSelected scans items by Value or Label (case-insensitive) and sets Selected index.
func (p *Picker) SetDefaultSelected(match string) {
	if match == "" {
		return
	}
	m := strings.ToLower(match)
	for i, item := range p.Items {
		if strings.ToLower(item.Value) == m || strings.ToLower(item.Label) == m {
			p.Selected = i
			return
		}
	}
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

// SavePrompt for the /save command
type SavePrompt struct {
	Name    string
	Editing bool
}

func NewSavePrompt(lastName string) SavePrompt {
	return SavePrompt{Name: lastName, Editing: true}
}

func (sp *SavePrompt) Render(width int) string {
	var b strings.Builder
	b.WriteString(style.HeadingStyle.Render("  Save Session") + "\n")
	b.WriteString(style.CommandDescStyle.Render("  Name: "))
	b.WriteString(style.CommandStyle.Render(sp.Name + "_"))
	return b.String()
}

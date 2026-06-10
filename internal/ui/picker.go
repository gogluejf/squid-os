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
	ModeLabelMeta                           // Label + Meta[0] (session: name + date)
	ModeLabelDesc                           // Label + Description (skill: name + description)
	ModeLabelValue                          // Label + Meta[0] or Value (legacy)
	ModeMultiCol                            // Label + Meta[0] + Meta[1] + ... (fixed cols, last truncates)
)

// PickerMatchMode controls how filtering matches items.
type PickerMatchMode int

const (
	MatchSubstring PickerMatchMode = iota
	MatchPrefix
)

// PickerItem is a typed row in a Picker.
// Meta is an array of secondary display fields — each becomes a column in MultiCol.
type PickerItem struct {
	Label       string   // primary text (always shown)
	Meta        []string // secondary fields (each becomes a column)
	Description string   // extended description (used by ModeLabelDesc)
	Value       string   // internal value for matching and selection
}

// Picker is a reusable, self-contained picker component.
// Call Init(ctx) after construction to compute column widths and resolve defaults.
type Picker struct {
	Title             string
	Items             []PickerItem
	Filter            string
	Selected          int
	DefaultValue      string
	DisplayMode       PickerDisplayMode
	MatchMode         PickerMatchMode
	OnSelectionChange func(int, PickerItem, any)
	OnConfirm         func(PickerItem, any) tea.Cmd
	OnCancel          func(any) tea.Cmd

	// Cached column widths — computed once in Init() from ALL items, never changes.
	labelWidth     int
	metaWidths     []int
	widthsComputed bool
}

// FilteredItems returns items matching the current filter.
func (p *Picker) FilteredItems() []PickerItem {
	if p.Filter == "" {
		return p.Items
	}
	f := strings.ToLower(p.Filter)
	var result []PickerItem
	for _, item := range p.Items {
		if matchItem(item, f, p.MatchMode) {
			result = append(result, item)
		}
	}
	return result
}

func matchItem(item PickerItem, f string, mode PickerMatchMode) bool {
	check := func(s string) bool {
		lower := strings.ToLower(s)
		if mode == MatchPrefix {
			return strings.HasPrefix(lower, f)
		}
		return strings.Contains(lower, f)
	}
	if check(item.Label) || check(item.Value) || check(item.Description) {
		return true
	}
	for _, m := range item.Meta {
		if check(m) {
			return true
		}
	}
	return false
}

// SelectedItem returns the currently selected PickerItem, or a zero value if out of range.
func (p *Picker) SelectedItem() PickerItem {
	items := p.FilteredItems()
	if p.Selected >= 0 && p.Selected < len(items) {
		return items[p.Selected]
	}
	return PickerItem{}
}

const PickerMaxItems = 15

// RenderHeight returns the exact number of terminal lines that Render() will output.
func (p *Picker) RenderHeight() int {
	h := 1
	if p.Filter != "" {
		h++
	}
	items := p.FilteredItems()
	if len(items) == 0 {
		h++
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
func (p *Picker) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	s := msg.String()

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

	if s == "enter" {
		if p.OnConfirm != nil {
			return p.OnConfirm(p.SelectedItem(), ctx)
		}
		return nil
	}

	if s == "esc" {
		if p.OnCancel != nil {
			return p.OnCancel(ctx)
		}
		return nil
	}

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
			if p.OnCancel != nil {
				return p.OnCancel(ctx)
			}
		}
		return nil
	}

	return nil
}

// Init computes fixed column widths from ALL items and resolves DefaultValue.
// Column widths are computed ONCE here and never change during scroll/filter.
func (p *Picker) Init(ctx any) {
	if !p.widthsComputed {
		p.computeWidths()
		p.widthsComputed = true
	}

	if p.DefaultValue == "" {
		return
	}
	m := strings.ToLower(p.DefaultValue)
	for i, item := range p.Items {
		if strings.ToLower(item.Value) == m || strings.ToLower(item.Label) == m {
			p.Selected = i
			p.DefaultValue = ""
			if p.OnSelectionChange != nil {
				items := p.FilteredItems()
				if p.Selected < len(items) {
					p.OnSelectionChange(p.Selected, items[p.Selected], ctx)
				}
			}
			return
		}
	}
	p.DefaultValue = ""
}

// computeWidths scans ALL items and sets fixed column widths (max value + 4 padding).
func (p *Picker) computeWidths() {
	labelMax := 0
	maxMeta := 0
	metaMax := []int{}

	for _, item := range p.Items {
		if l := len(item.Label); l > labelMax {
			labelMax = l
		}
		mlen := len(item.Meta)
		if mlen > maxMeta {
			maxMeta = mlen
		}
		// Extend metaMax to accommodate this item's meta count
		for len(metaMax) < mlen {
			metaMax = append(metaMax, 0)
		}
		// Check all meta fields (not just when count increases)
		for j, m := range item.Meta {
			if l := len(m); l > metaMax[j] {
				metaMax[j] = l
			}
		}
	}

	p.labelWidth = labelMax + 4
	if p.labelWidth < 12 {
		p.labelWidth = 12
	}

	p.metaWidths = make([]int, maxMeta)
	for i := range p.metaWidths {
		p.metaWidths[i] = metaMax[i] + 4
		if p.metaWidths[i] < 4 {
			p.metaWidths[i] = 4
		}
	}
}

// Render renders the picker with the configured display mode.
func (p *Picker) Render(width int) string {
	items := p.FilteredItems()

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

	var globalStart int
	if len(items) > PickerMaxItems {
		globalStart = p.Selected - 7
		if globalStart < 0 {
			globalStart = 0
		}
	}

	for i, item := range visible {
		isSelected := (globalStart+i) == p.Selected
		b.WriteString(p.renderRow(item, isSelected, width) + "\n")
	}

	return lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgFooter)).
		Width(width).
		Render(strings.TrimRight(b.String(), "\n"))
}

func (p *Picker) renderRow(item PickerItem, sel bool, width int) string {
	switch p.DisplayMode {
	case ModeSingleCol:
		content := "  " + item.Label
		return p.applyRowStyle(content, sel, width)

	case ModeMultiCol:
		return p.renderMultiCol(item, sel, width)

	case ModeLabelMeta:
		right := ""
		if len(item.Meta) > 0 {
			right = item.Meta[0]
		}
		content := "  " + padRight(item.Label, p.labelWidth-2) + right
		return p.applyRowStyle(content, sel, width)

	case ModeLabelDesc:
		avail := width - p.labelWidth - 4
		if avail < 4 {
			avail = 4
		}
		content := "  " + padRight(item.Label, p.labelWidth-2) + truncateRight(item.Description, avail)
		return p.applyRowStyle(content, sel, width)

	case ModeLabelValue:
		right := ""
		if len(item.Meta) > 0 {
			right = item.Meta[0]
		} else {
			right = item.Value
		}
		content := "  " + padRight(item.Label, p.labelWidth-2) + right
		return p.applyRowStyle(content, sel, width)

	default:
		content := "  " + item.Label
		return p.applyRowStyle(content, sel, width)
	}
}

// applyRowStyle applies background/foreground/bold + width constraint to plain text.
func (p *Picker) applyRowStyle(text string, sel bool, width int) string {
	bg := lipgloss.Color(style.P.BgFooter)
	fg := lipgloss.Color(style.P.TextMuted)
	if sel {
		bg = lipgloss.Color(style.P.BgSelected)
		fg = lipgloss.Color(style.P.TextAccent)
	}
	s := lipgloss.NewStyle().Background(bg).Foreground(fg).Width(width)
	if sel {
		s = s.Bold(true)
	}
	return s.Render(text)
}

// renderMultiCol: Label (fixed) + Meta[0] (fixed) + Meta[1] (fixed) + ... + last Meta (remaining, truncates)
// Builds the row as plain text first, then applies a single lipgloss style at the end.
func (p *Picker) renderMultiCol(item PickerItem, sel bool, width int) string {
	parts := []string{item.Label}
	for _, m := range item.Meta {
		parts = append(parts, m)
	}

	// Total fixed width: label + all meta columns except the last
	fixed := p.labelWidth
	for i := 1; i < len(parts)-1; i++ {
		if i-1 < len(p.metaWidths) {
			fixed += p.metaWidths[i-1]
		} else {
			fixed += 2
		}
	}
	// lastWidth is the remaining space for the last column, minus the 2-char row indent
	lastWidth := width - fixed - 2
	if lastWidth < 2 {
		lastWidth = 2
	}

	// Build row as plain text
	var b strings.Builder
	b.WriteString("  ")

	for j, part := range parts {
		if j == 0 {
			b.WriteString(padRight(part, p.labelWidth))
		} else if j < len(parts)-1 {
			w := 2
			if j-1 < len(p.metaWidths) {
				w = p.metaWidths[j-1]
			}
			b.WriteString(padRight(part, w))
		} else {
			b.WriteString(truncateRight(part, lastWidth))
		}
	}

	// Apply single style at the end
	bg := lipgloss.Color(style.P.BgFooter)
	fg := lipgloss.Color(style.P.TextMuted)
	if sel {
		bg = lipgloss.Color(style.P.BgSelected)
		fg = lipgloss.Color(style.P.TextAccent)
	}
	s := lipgloss.NewStyle().Background(bg).Foreground(fg).Width(width)
	if sel {
		s = s.Bold(true)
	}
	return s.Render(b.String())
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncateRight(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return s[:maxLen-1] + "…"
}

func isPrintable(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		(c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~') || c == ' '
}

package component

import (
	"strings"

	"squid-os/internal/style"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PickerMatchMode controls how filtering matches items.
type PickerMatchMode int

const (
	MatchSubstring PickerMatchMode = iota
	MatchPrefix
)

// PickerItem is a row in a Picker.
// Label is the primary column (always rendered in TextPrimary).
// Meta holds optional secondary columns (rendered in TextMuted; the last one takes remaining width).
// If Meta is empty, the Label alone fills the row.
type PickerItem struct {
	Label string   // primary text (always shown, white)
	Meta  []string // optional secondary fields (each becomes a dimmed column)
	Value string   // internal value for matching and selection
}

// Picker is a reusable, self-contained picker component.
// Call Init(ctx) after construction to compute column widths and resolve defaults.
type Picker struct {
	Title             string
	Description       string // dim multi-line text shown under title (optional)
	Items             []PickerItem
	Filter            string
	Selected          int
	DefaultValue      string
	MatchMode         PickerMatchMode
	OnSelectionChange func(int, PickerItem, any)
	OnConfirm         func(PickerItem, any) tea.Cmd
	OnCancel          func(any) tea.Cmd

	// Cached column widths — computed once in Init() from ALL items, never changes.
	labelWidth     int
	metaWidths     []int
	widthsComputed bool
	initialized    bool
	// lastNotified tracks the previously reported item to avoid redundant callbacks.
	lastNotified PickerItem
}

// fireSelectionChange calls OnSelectionChange only if the selected item actually changed.
func (p *Picker) fireSelectionChange(idx int, item PickerItem, ctx any) {
	if p.OnSelectionChange == nil {
		return
	}
	if item.Value == p.lastNotified.Value && idx == p.Selected {
		return
	}
	p.lastNotified = item
	p.OnSelectionChange(idx, item, ctx)
}

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
	if check(item.Label) || check(item.Value) {
		return true
	}
	// Meta only matches in substring mode — prefix matching on descriptions is too noisy
	if mode != MatchPrefix {
		for _, m := range item.Meta {
			if strings.Contains(strings.ToLower(m), f) {
				return true
			}
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
// Always returns the same height regardless of filter state — "No matches" pads to PickerMaxItems.
// Description lines are included in the count.
func (p *Picker) RenderHeight() int {
	// Base: leading blank + title + separator + 15 slots + trailing blank = 19
	// Plus: blank before desc + desc lines (if any)
	descCount := 0
	if p.Description != "" {
		_, descCount = formatDescription(p.Description, 80, DescriptionMaxLines) // width doesn't affect line count
		if descCount > 0 {
			descCount += 1 // blank separator before description
		}
	}
	return 3 + PickerMaxItems + 1 + descCount
}

// HandleKey processes a key message and returns a tea.Cmd if a callback fires.
func (p *Picker) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	s := msg.String()

	if s == "up" {
		if p.Selected > 0 {
			p.Selected--
			if p.Selected < len(p.FilteredItems()) {
				p.fireSelectionChange(p.Selected, p.FilteredItems()[p.Selected], ctx)
			}
		}
		return nil
	}

	if s == "tab" {
		items := p.FilteredItems()
		if len(items) == 0 {
			return nil
		}
		if p.Filter != "" {
			// Shell-style completion: extend filter by finding common prefix
			// across all matched item labels.
			completion := completeFilter(items, p.Filter)
			if completion != p.Filter {
				p.Filter = completion
				p.Selected = 0
				items := p.FilteredItems()
				if len(items) > 0 {
					p.fireSelectionChange(0, items[0], ctx)
				}
			}
			return nil
		}
		// No filter: just cycle selection down.
		if p.Selected < len(items)-1 {
			p.Selected++
			if p.Selected < len(items) {
				p.fireSelectionChange(p.Selected, items[p.Selected], ctx)
			}
		}
		return nil
	}

	if s == "down" {
		items := p.FilteredItems()
		if p.Selected < len(items)-1 {
			p.Selected++
			if p.Selected < len(items) {
				p.fireSelectionChange(p.Selected, items[p.Selected], ctx)
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

	if s == "esc" || s == "ctrl+c" {
		if p.OnCancel != nil {
			return p.OnCancel(ctx)
		}
		return nil
	}

	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		// Paste event — insert all runes into filter
		p.Filter += string(msg.Runes)
		p.Selected = 0
		items := p.FilteredItems()
		if len(items) > 0 {
			p.fireSelectionChange(0, items[0], ctx)
		}
		return nil
	}

	if len(s) == 1 && isPrintable(s) {
		p.Filter += s
		p.Selected = 0
		items := p.FilteredItems()
		if len(items) > 0 {
			p.fireSelectionChange(0, items[0], ctx)
		}
		return nil
	}

	if s == "backspace" {
		if len(p.Filter) > 0 {
			p.Filter = p.Filter[:len(p.Filter)-1]
			p.Selected = 0
			items := p.FilteredItems()
			if len(items) > 0 {
				p.fireSelectionChange(0, items[0], ctx)
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
// Fires OnSelectionChange for the resolved selection (default match, or first item).
// Idempotent — guarded by an initialized flag to prevent re-entry loops.
func (p *Picker) Init(ctx any) {
	if p.initialized {
		return
	}
	p.initialized = true

	if !p.widthsComputed {
		p.computeWidths()
		p.widthsComputed = true
	}

	items := p.FilteredItems()
	if len(items) == 0 {
		return
	}

	if p.DefaultValue != "" {
		m := strings.ToLower(p.DefaultValue)
		for i, item := range p.Items {
			if strings.ToLower(item.Value) == m || strings.ToLower(item.Label) == m {
				p.Selected = i
				p.DefaultValue = ""
				items := p.FilteredItems()
				if p.Selected < len(items) {
					p.fireSelectionChange(p.Selected, items[p.Selected], ctx)
				}
				return
			}
		}
		p.DefaultValue = ""
	}

	// No default match (or no default): select the first item and fire callback
	p.Selected = 0
	p.fireSelectionChange(0, items[0], ctx)
}

// Update handles non-key messages for Picker (no-op, kept for interface compliance).
func (p *Picker) Update(tea.Msg, any) tea.Cmd {
	return nil
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
		for len(metaMax) < mlen {
			metaMax = append(metaMax, 0)
		}
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

// Render renders the picker.
func (p *Picker) Render(width int) string {
	items := p.FilteredItems()

	// Compute description lines
	var descLines []string
	var descCount int
	if p.Description != "" {
		descLines, descCount = formatDescription(p.Description, width, DescriptionMaxLines)
	}

	// Compute how many item slots we have left
	// Total budget = PickerMaxItems + 4 (matches RenderHeight base)
	// Fixed: leading blank(1) + title(1) + sep before items(1) + trailing blank(1) = 4
	// Description: blank(1) + descCount lines
	// Remaining for item slots = PickerMaxItems - descCount
	itemSlots := PickerMaxItems
	if descCount > 0 {
		itemSlots -= (1 + descCount) // blank + desc lines
	}
	if itemSlots < 1 {
		itemSlots = 1
	}

	var visible []PickerItem
	if len(items) > itemSlots {
		start := p.Selected - (itemSlots / 2)
		if start < 0 {
			start = 0
		}
		end := start + itemSlots
		if end > len(items) {
			end = len(items)
			start = end - itemSlots
			if start < 0 {
				start = 0
			}
		}
		visible = items[start:end]
	} else {
		visible = items
	}

	var lines []string

	// Leading blank line
	lines = append(lines, " ")

	// Title (+ inline filter if active)
	if p.Filter != "" {
		var b strings.Builder
		bg := lipgloss.Color(style.P.BgFooter)
		b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextHeading)).Bold(true).Render("   " + p.Title))
		b.WriteString(lipgloss.NewStyle().Background(bg).Render(" "))
		b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("filter: " + p.Filter))
		lines = append(lines, b.String())
	} else {
		lines = append(lines, lipgloss.NewStyle().Background(lipgloss.Color(style.P.BgFooter)).Render(style.HeadingStyle.Render("   "+p.Title)))
	}

	// Description (dim, multi-line) — separated from title by a blank line
	if len(descLines) > 0 {
		lines = append(lines, " ")
		lines = append(lines, strings.Split(renderDescriptionLines(descLines, width), "\n")...)
	}

	if len(items) == 0 {
		// Separator blank line
		lines = append(lines, " ")
		lines = append(lines, lipgloss.NewStyle().Background(lipgloss.Color(style.P.BgFooter)).Render(style.CommandDescStyle.Render("   No matches")))
		// Pad remaining slots with blank lines to reach itemSlots
		for i := 0; i < itemSlots-1; i++ {
			lines = append(lines, " ")
		}
		// Trailing blank line
		lines = append(lines, " ")
		return lipgloss.NewStyle().
			Background(lipgloss.Color(style.P.BgFooter)).
			Width(width).
			Render(strings.Join(lines, "\n"))
	}

	// Separator blank line after title/description
	lines = append(lines, " ")

	var globalStart int
	if len(items) > itemSlots {
		globalStart = p.Selected - (itemSlots / 2)
		if globalStart < 0 {
			globalStart = 0
		}
	}

	for i, item := range visible {
		isSelected := (globalStart + i) == p.Selected
		lines = append(lines, p.renderRow(item, isSelected, width))
	}

	// Pad with blank lines to always show itemSlots item rows
	for i := len(visible); i < itemSlots; i++ {
		lines = append(lines, " ")
	}

	// Trailing blank line
	lines = append(lines, " ")

	return lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgFooter)).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

// renderRow renders a single picker row.
// Label is always TextPrimary (white), Meta columns are TextMuted (dimmed).
// The last Meta column takes remaining width and truncates if needed.
// If no Meta columns exist, the Label fills the row.
// Every segment carries background to prevent ANSI reset gaps (lipgloss-style-concatenation-reset).
func (p *Picker) renderRow(item PickerItem, sel bool, width int) string {
	bg := lipgloss.Color(style.P.BgFooter)
	labelFg := lipgloss.Color(style.P.TextPrimary)
	metaFg := lipgloss.Color(style.P.TextMuted)
	if sel {
		bg = lipgloss.Color(style.P.BgSelected)
		labelFg = lipgloss.Color(style.P.TextAccent)
		metaFg = lipgloss.Color(style.P.TextAccent)
	}
	base := lipgloss.NewStyle().Background(bg)
	if sel {
		base = base.Bold(true)
	}

	// Leading indicator: "   " for unselected, " ▶ " for selected (all 3 chars)
	prefix := "   "
	if sel {
		prefix = " \u25B6 " // ▶ play arrow padded to 3 chars
	}
	prefixLen := len([]rune(prefix))

	// No meta columns: label fills the row
	if len(item.Meta) == 0 {
		// Outer width style ensures the background fills the entire line
		s := lipgloss.NewStyle().Background(bg).Width(width)
		if sel {
			s = s.Bold(true)
		}
		inner := base.Foreground(labelFg).Render(prefix + item.Label)
		return s.Render(inner)
	}

	// Multi-column: every segment styled independently with background
	var b strings.Builder
	b.WriteString(base.Foreground(labelFg).Render(prefix))

	b.WriteString(base.Foreground(labelFg).Render(padRight(item.Label, p.labelWidth)))

	// Meta columns: fixed width for intermediate, remaining width for last
	fixed := p.labelWidth
	for i := 1; i < len(item.Meta); i++ {
		if i-1 < len(p.metaWidths) {
			fixed += p.metaWidths[i-1]
		} else {
			fixed += 2
		}
	}
	lastWidth := width - fixed - prefixLen
	if lastWidth < 2 {
		lastWidth = 2
	}

	for j, m := range item.Meta {
		if j < len(item.Meta)-1 {
			w := 2
			if j < len(p.metaWidths) {
				w = p.metaWidths[j]
			}
			b.WriteString(base.Foreground(metaFg).Render(padRight(m, w)))
		} else {
			b.WriteString(base.Foreground(metaFg).Render(padRight(truncateRight(m, lastWidth), lastWidth)))
		}
	}

	// Trailing background fill so the entire line has consistent bg
	// (padRight/truncateRight already account for width, but add trailing spaces just in case)
	totalWritten := prefixLen + p.labelWidth
	for j := range item.Meta {
		if j < len(item.Meta)-1 {
			w := 2
			if j < len(p.metaWidths) {
				w = p.metaWidths[j]
			}
			totalWritten += w
		} else {
			totalWritten += lastWidth
		}
	}
	if totalWritten < width {
		b.WriteString(base.Render(strings.Repeat(" ", width-totalWritten)))
	}

	return b.String()
}

func padRight(s string, n int) string {
	runes := []rune(s)
	if len(runes) >= n {
		return string(runes[:n])
	}
	return string(runes) + strings.Repeat(" ", n-len(runes))
}

func truncateRight(s string, maxCells int) string {
	runes := []rune(s)
	if len(runes) <= maxCells {
		return s
	}
	if maxCells <= 2 {
		return ".."
	}
	return string(runes[:maxCells-2]) + ".."
}

func isPrintable(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		(c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~') || c == ' '
}

// completeFilter finds how far the filter can be extended by finding the
// common continuation across all matched items' labels. Returns the new filter
// (at least as long as the original), or the original filter unchanged if
// no extension is possible.
func completeFilter(items []PickerItem, filter string) string {
	if len(items) <= 1 {
		if len(items) == 1 {
			// Single match: complete to the portion of the label that contains/extends the filter
			labelLower := strings.ToLower(items[0].Label)
			idx := strings.Index(labelLower, strings.ToLower(filter))
			if idx >= 0 {
				return items[0].Label[idx:]
			}
			return filter
		}
		return filter
	}

	filterLower := strings.ToLower(filter)

	// For each item, find where the filter matches in the label, then take
	// the full remaining text from that point as the candidate string.
	var candidates []string
	for _, item := range items {
		labelLower := strings.ToLower(item.Label)
		idx := strings.Index(labelLower, filterLower)
		if idx < 0 {
			return filter
		}
		// Candidate: everything from the match start to end of label
		candidates = append(candidates, item.Label[idx:])
	}

	// Find the common prefix among all candidates.
	minLen := len(candidates[0])
	for _, c := range candidates {
		if l := len(c); l < minLen {
			minLen = l
		}
	}
	common := minLen
	for i := 0; i < minLen; i++ {
		c := candidates[0][i]
		for j := 1; j < len(candidates); j++ {
			if candidates[j][i] != c {
				common = i
				break
			}
		}
		if common < minLen {
			break
		}
	}
	// Only return a longer string than the current filter
	if common <= len(filter) {
		return filter
	}
	return candidates[0][:common]
}

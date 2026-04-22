package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HistorySearchOverlay handles the reverse-search overlay state and rendering
type HistorySearchOverlay struct {
	Filter   string
	MatchIdx int // Index within filtered results
	Visible  bool
	Items    []string // The items to search through (history entries)
}

func NewHistorySearchOverlay() HistorySearchOverlay {
	return HistorySearchOverlay{}
}

// FilteredItems returns history entries matching the current filter (case-insensitive substring match)
func (hs *HistorySearchOverlay) FilteredItems() []string {
	if hs.Filter == "" {
		return hs.Items
	}
	f := strings.ToLower(hs.Filter)
	var result []string
	for _, item := range hs.Items {
		if strings.Contains(strings.ToLower(item), f) {
			result = append(result, item)
		}
	}
	return result
}

// SelectedText returns the currently selected item text, or empty string if no matches
func (hs *HistorySearchOverlay) SelectedText() string {
	items := hs.FilteredItems()
	if len(items) == 0 {
		return ""
	}
	if hs.MatchIdx >= len(items) {
		hs.MatchIdx = len(items) - 1
	}
	return items[hs.MatchIdx]
}

// NextMatch cycles to the next match (forward through filtered results)
func (hs *HistorySearchOverlay) NextMatch() {
	items := hs.FilteredItems()
	if len(items) == 0 {
		return
	}
	hs.MatchIdx = (hs.MatchIdx + 1) % len(items)
}

// PrevMatch cycles to the previous match (backward through filtered results)
func (hs *HistorySearchOverlay) PrevMatch() {
	items := hs.FilteredItems()
	if len(items) == 0 {
		return
	}
	hs.MatchIdx = (hs.MatchIdx - 1 + len(items)) % len(items)
}

// Reset clears the history search state
func (hs *HistorySearchOverlay) Reset() {
	hs.Filter = ""
	hs.MatchIdx = 0
	hs.Visible = false
	hs.Items = nil
}

// RenderHeight returns the exact number of terminal lines that Render() will output.
func (hs *HistorySearchOverlay) RenderHeight() int {
	return 1
}

// historySearchBg is the background colour for the history search overlay
const historySearchBg = lipgloss.Color("235")

// Render renders the history search overlay line (notification-style: white text on dark background)
func (hs *HistorySearchOverlay) Render(width int) string {

	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(252)).Render(" search prompt history: ")
	const dimColor = "240"

	// Only show match info after at least one character is typed
	if hs.Filter == "" {
		// No filter typed yet - just show prompt, no background bar
		dimSuffix := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render("(esc to exit)")
		return prefix + dimSuffix

	}

	items := hs.FilteredItems()
	total := len(items)
	idx := hs.MatchIdx
	if total > 0 && idx >= total {
		idx = 0
	}

	// Build the suffix based on match count
	var suffix string
	switch total {
	case 0:
		suffix = " (no matches) (esc to exit)"
	case 1:
		suffix = " (esc to exit)"
	default:
		suffix = fmt.Sprintf(" (%d/%d) (ctrl+r for next, esc to exit)", idx+1, total)
	}

	// Style only the filter text portion with bold white on dark background
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true).Background(lipgloss.Color("238"))
	filterStyled := filterStyle.Render(hs.Filter)

	// Style the suffix as dim
	dimSuffix := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render(suffix)

	// Construct: prefix + styled_filter + dim_suffix
	return prefix + filterStyled + dimSuffix
}

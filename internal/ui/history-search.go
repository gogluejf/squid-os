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

// Render renders the history search overlay line
func (hs *HistorySearchOverlay) Render(width int) string {
	items := hs.FilteredItems()
	total := len(items)
	idx := hs.MatchIdx
	if total > 0 && idx >= total {
		idx = 0
	}

	var status string
	if total == 0 {
		status = lipgloss.NewStyle().Background(historySearchBg).Foreground(lipgloss.Color("244")).Render(" no matches")
	} else {
		status = lipgloss.NewStyle().Background(historySearchBg).Foreground(lipgloss.Color("110")).Bold(true).Render(fmt.Sprintf(" [%d/%d]", idx+1, total))
	}

	searchLabel := lipgloss.NewStyle().Background(historySearchBg).Foreground(lipgloss.Color("110")).Bold(true).Render("R-search")
	filterText := lipgloss.NewStyle().Background(historySearchBg).Foreground(lipgloss.Color("252")).Render(hs.Filter)

	row := searchLabel + ": " + filterText + status

	return lipgloss.NewStyle().Background(historySearchBg).Width(width).Render(row)
}

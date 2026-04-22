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

// Render renders the history search overlay line (notification-style: white text on dark background)
func (hs *HistorySearchOverlay) Render(width int) string {
	// Only show match info after at least one character is typed
	if hs.Filter == "" {
		// No filter typed yet - just show prompt
		status := "search prompt history: (esc to exit)"
		message := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(status)
		return lipgloss.NewStyle().Background(historySearchBg).Width(width).Render(message)
	}

	items := hs.FilteredItems()
	total := len(items)
	idx := hs.MatchIdx
	if total > 0 && idx >= total {
		idx = 0
	}

	// Build the message in notification style
	var status string
	switch total {
	case 0:
		status = fmt.Sprintf("search prompt history: %s (no matches) (esc to exit)", hs.Filter)
	case 1:
		status = fmt.Sprintf("search prompt history: %s (esc to exit)", hs.Filter)
	default:
		status = fmt.Sprintf("search prompt history: %s (%d/%d) (ctrl+r for next, esc to exit)", hs.Filter, idx+1, total)
	}

	// Render with white text on dark background (same as notification style)
	message := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(status)

	return lipgloss.NewStyle().Background(historySearchBg).Width(width).Render(message)
}

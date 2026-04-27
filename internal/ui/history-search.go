package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HistorySearchOverlay handles the reverse-search overlay state and rendering
type HistorySearchOverlay struct {
	filterStr string
	MatchIdx  int
	Visible   bool
	Items     []string
	filtered  []string
}

func NewHistorySearchOverlay(items []string) HistorySearchOverlay {
	return HistorySearchOverlay{
		Items:    items,
		filtered: items,
	}
}

// FilterText returns the current filter string.
func (hs *HistorySearchOverlay) FilterText() string {
	return hs.filterStr
}

// Filter applies the given filter text to Items and caches the results in filtered.
// Resets MatchIdx to 0.
func (hs *HistorySearchOverlay) Filter(filter string) {
	hs.filterStr = filter
	if filter == "" {
		hs.filtered = hs.Items
	} else {
		f := strings.ToLower(filter)
		seen := make(map[string]struct{}, len(hs.filtered))
		hs.filtered = hs.filtered[:0]
		for _, item := range hs.Items {
			if strings.Contains(strings.ToLower(item), f) {
				if _, ok := seen[item]; !ok {
					seen[item] = struct{}{}
					hs.filtered = append(hs.filtered, item)
				}
			}
		}
	}
	if len(hs.filtered) > 0 {
		hs.MatchIdx = len(hs.filtered) - 1
	} else {
		hs.MatchIdx = 0
	}
}

// FilteredItems returns the cached filtered results.
func (hs *HistorySearchOverlay) FilteredItems() []string {
	return hs.filtered
}

// SelectedText returns the currently selected item text, or empty string if no matches
func (hs *HistorySearchOverlay) SelectedText() string {
	if len(hs.filtered) == 0 {
		return ""
	}
	if hs.MatchIdx >= len(hs.filtered) {
		hs.MatchIdx = len(hs.filtered) - 1
	}
	return hs.filtered[hs.MatchIdx]
}

// NextMatch cycles to the next match (forward through filtered results)
func (hs *HistorySearchOverlay) NextMatch() {
	if len(hs.filtered) == 0 {
		return
	}
	hs.MatchIdx = (hs.MatchIdx + 1) % len(hs.filtered)
}

// PrevMatch cycles to the previous match (backward through filtered results)
func (hs *HistorySearchOverlay) PrevMatch() {
	if len(hs.filtered) == 0 {
		return
	}
	hs.MatchIdx = (hs.MatchIdx - 1 + len(hs.filtered)) % len(hs.filtered)
}

// Reset clears the history search state
func (hs *HistorySearchOverlay) Reset() {
	hs.filterStr = ""
	hs.MatchIdx = 0
	hs.Visible = false
	hs.Items = nil
	hs.filtered = nil
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
	if hs.filterStr == "" {
		// No filter typed yet - just show prompt, no background bar
		dimSuffix := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render("(esc to exit)")
		return prefix + dimSuffix

	}

	total := len(hs.filtered)
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
	filterStyled := filterStyle.Render(hs.filterStr)

	// Style the suffix as dim
	dimSuffix := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render(suffix)

	// Construct: prefix + styled_filter + dim_suffix
	return prefix + filterStyled + dimSuffix
}

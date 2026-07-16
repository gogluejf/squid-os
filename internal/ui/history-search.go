package ui

import (
	"fmt"
	"strings"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/style"
	"squid-os/internal/util"

	"github.com/charmbracelet/lipgloss"
)

// HistorySearchOverlay handles the reverse-search overlay state and rendering
type HistorySearchOverlay struct {
	filterStr string
	MatchIdx  int
	Visible   bool
	Items     []config.HistoryEntry
	filtered  []config.HistoryEntry
}

func NewHistorySearchOverlay(items []config.HistoryEntry) HistorySearchOverlay {
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
		seen := make(map[string]struct{}, len(hs.Items))
		filtered := make([]config.HistoryEntry, 0, len(hs.Items))
		// Iterate in reverse so we keep the LAST (most recent) occurrence of duplicates.
		for i := len(hs.Items) - 1; i >= 0; i-- {
			item := hs.Items[i]
			if strings.Contains(strings.ToLower(item.Text), f) {
				if _, ok := seen[item.Text]; !ok {
					seen[item.Text] = struct{}{}
					filtered = append(filtered, item)
				}
			}
		}
		hs.filtered = filtered
	}
	hs.MatchIdx = 0
}

// FilteredItems returns the cached filtered results.
func (hs *HistorySearchOverlay) FilteredItems() []config.HistoryEntry {
	return hs.filtered
}

// SelectedText returns the currently selected item text, or empty string if no matches.
func (hs *HistorySearchOverlay) SelectedText() string {
	entry, ok := hs.SelectedEntry()
	if !ok {
		return ""
	}
	return entry.Text
}

// SelectedEntry returns the currently selected item, or false if no matches.
func (hs *HistorySearchOverlay) SelectedEntry() (config.HistoryEntry, bool) {
	if len(hs.filtered) == 0 {
		return config.HistoryEntry{}, false
	}
	if hs.MatchIdx >= len(hs.filtered) {
		hs.MatchIdx = len(hs.filtered) - 1
	}
	return hs.filtered[hs.MatchIdx], true
}

// NextMatch cycles to the next match in display order (newer to older for reverse search).
func (hs *HistorySearchOverlay) NextMatch() {
	if len(hs.filtered) == 0 {
		return
	}
	hs.MatchIdx = (hs.MatchIdx + 1) % len(hs.filtered)
}

// PrevMatch cycles to the previous match in display order (older to newer for reverse search).
func (hs *HistorySearchOverlay) PrevMatch() {
	if len(hs.filtered) == 0 {
		return
	}
	hs.MatchIdx = (hs.MatchIdx - 1 + len(hs.filtered)) % len(hs.filtered)
}

// Reset clears the history search state.
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
var historySearchBg = lipgloss.Color(style.P.BgFooter)

// Render renders the history search overlay line (notification-style: white text on dark background)
func (hs *HistorySearchOverlay) Render(width int) string {

	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextPrimary)).Render(" search prompt history: ")
	const dimColor = "240"

	// Only show match info after at least one character is typed
	if hs.filterStr == "" {
		// No filter typed yet - just show prompt, no background bar
		dimSuffix := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Background(lipgloss.Color(style.P.BgApp)).Render("(esc to exit)")
		return style.StatusLineStyle.Width(width).Render(prefix + dimSuffix)

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
		suffix = fmt.Sprintf(" · %s · (esc to exit)", hs.selectedDateLabel())
	default:
		suffix = fmt.Sprintf(" (%d/%d) · %s · (ctrl+r for next, esc to exit)", idx+1, total, hs.selectedDateLabel())
	}

	// Style only the filter text portion with bold white on dark background
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextHeading)).Bold(true).Background(lipgloss.Color(style.P.BgUser))
	filterStyled := filterStyle.Render(hs.filterStr)

	// Style the suffix as dim
	dimSuffix := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Background(lipgloss.Color(style.P.BgApp)).Render(suffix)

	// Construct: prefix + styled_filter + dim_suffix
	return style.StatusLineStyle.Width(width).Render(prefix + filterStyled + dimSuffix)
}

func (hs *HistorySearchOverlay) selectedDateLabel() string {
	entry, ok := hs.SelectedEntry()
	if !ok || entry.CreatedAt.IsZero() {
		return "unknown date"
	}
	return util.FriendlyHistoryDate(entry.CreatedAt, time.Now())
}

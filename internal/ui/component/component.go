package component

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

// Component is the common interface for all overlay components
// (Picker, Prompt, Question). The app layer stores a single Component
// and dispatches through it.
type Component interface {
	Init(ctx any)
	Update(msg tea.Msg, ctx any) tea.Cmd
	HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd
	Render(width int) string
	RenderHeight() int
}

// -------------------------------------------------------
// Shared description formatting
// -------------------------------------------------------

const (
	// DescriptionMaxLines is the maximum number of description lines shown
	// before truncation.  The "+N more lines" trailer adds one more.
	DescriptionMaxLines = 5
	// DescriptionIndent is the leading spaces before each description line.
	DescriptionIndent = "   "
)

// formatDescription splits raw text on '\n', pads each line with the standard
// indent, truncates at maxLines and appends a "+N more lines" trailer.
// Returns the formatted lines and the total line count (including the trailer
// if truncation occurred, not including the separator blank line).
//
// Callers render each returned line individually (typically with BgFooter +
// TextMuted styling) and use the count to compute how many content lines
// remain for the rest of the component (options, items, input).
func formatDescription(text string, width int, maxLines int) (lines []string, count int) {
	if text == "" {
		return nil, 0
	}

	prefixW := lipgloss.Width(DescriptionIndent)
	avail := width - prefixW
	if avail < 4 {
		avail = 4
	}

	raw := strings.Split(text, "\n")

	if len(raw) <= maxLines {
		// Fits entirely — no truncation
		for _, line := range raw {
			// Truncate overly long individual lines
			if lipgloss.Width(line) > avail {
				runes := []rune(line)
				var result []rune
				totalW := 0
				for _, r := range runes {
					chW := lipgloss.Width(string(r))
					if totalW+chW > avail {
						break
					}
					totalW += chW
					result = append(result, r)
				}
				result = append(result, '.', '.', '.')
				line = string(result)
			}
			lines = append(lines, DescriptionIndent+line)
		}
		return lines, len(lines)
	}

	// Truncate: show maxLines, then a trailer line
	for i := 0; i < maxLines; i++ {
		line := raw[i]
		if lipgloss.Width(line) > avail {
			runes := []rune(line)
			var result []rune
			totalW := 0
			for _, r := range runes {
				chW := lipgloss.Width(string(r))
				if totalW+chW > avail {
					break
				}
				totalW += chW
				result = append(result, r)
			}
			result = append(result, '.', '.', '.')
			line = string(result)
		}
		lines = append(lines, DescriptionIndent+line)
	}
	remaining := len(raw) - maxLines
	lines = append(lines, DescriptionIndent+fmt.Sprintf("... (+%d more lines)", remaining))
	return lines, len(lines)
}

// renderDescriptionLines applies styling to the pre-formatted description lines
// and returns a single joined string.  The caller is responsible for adding
// the separator blank line(s) before/after.
func renderDescriptionLines(lines []string, width int) string {
	bg := lipgloss.Color(style.P.BgFooter)
	var styled []string
	for _, line := range lines {
		styled = append(styled, lipgloss.NewStyle().
			Background(bg).
			Foreground(lipgloss.Color(style.P.TextMuted)).
			Render(line))
	}
	return strings.Join(styled, "\n")
}

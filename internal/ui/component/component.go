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
// indent, wraps lines that exceed the available width, and truncates at maxLines
// with a "+N more lines" trailer if needed.
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

	// First pass: expand all raw lines into wrapped lines
	var wrapped []string
	for _, line := range raw {
		wrapped = append(wrapped, wrapLine(line, avail)...)
	}

	// Second pass: apply maxLines cap to the flattened list
	if len(wrapped) <= maxLines {
		for _, line := range wrapped {
			lines = append(lines, DescriptionIndent+line)
		}
		return lines, len(lines)
	}

	for i := 0; i < maxLines; i++ {
		lines = append(lines, DescriptionIndent+wrapped[i])
	}
	remaining := len(wrapped) - maxLines
	lines = append(lines, DescriptionIndent+fmt.Sprintf("... (+%d more lines)", remaining))
	return lines, len(lines)
}

// wrapLine splits a single line into multiple lines so that no output line
// exceeds maxWidth.  It breaks on spaces when possible, and falls back to
// hard-break at maxWidth when a token (e.g. a long URL) is longer than avail.
func wrapLine(line string, maxWidth int) []string {
	if lipgloss.Width(line) <= maxWidth {
		return []string{line}
	}

	var result []string
	words := strings.Fields(line)
	var current []string
	currentWidth := 0

	for _, word := range words {
		w := lipgloss.Width(word)
		added := w
		if len(current) > 0 {
			added++ // space
		}

		// If a single word is wider than maxWidth, hard-break it
		if w > maxWidth {
			// Flush current line first
			if len(current) > 0 {
				result = append(result, strings.Join(current, " "))
				current = nil
				currentWidth = 0
			}
			// Hard-break the word into chunks
			runes := []rune(word)
			var chunk []rune
			chunkWidth := 0
			for _, r := range runes {
				chW := lipgloss.Width(string(r))
				if chunkWidth+chW > maxWidth {
					result = append(result, string(chunk))
					chunk = nil
					chunkWidth = 0
				}
				chunk = append(chunk, r)
				chunkWidth += chW
			}
			if len(chunk) > 0 {
				result = append(result, string(chunk))
			}
			continue
		}

		if currentWidth+added <= maxWidth {
			current = append(current, word)
			currentWidth += added
		} else {
			result = append(result, strings.Join(current, " "))
			current = []string{word}
			currentWidth = w
		}
	}

	if len(current) > 0 {
		result = append(result, strings.Join(current, " "))
	}

	return result
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

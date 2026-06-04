package ui

import (
	"strings"

	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
)

// DrawCanvas renders a message box with optional title parts and body content.
//
//   - parts:  pre-styled title segments rendered as "↳ part0 · part1 · ..."
//   - content: body blocks joined with "\n\n".  Can be pre-styled or plain text.
//   - s: StyleLabel with all needed styles.
//   - topGap:  leading blank rows before the first line.
//   - width:   total rendered width (includes margins + padding).
//
// Trailing spacing: one blank row after content, then MarginBottom (bg-colored).
func DrawCanvas(parts []string, content []string, s style.StyleLabel, topGap int, width int, marginBottom int) string {

	partStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(s.Fg)).
		Background(lipgloss.Color(s.Bg))

	st := partStyle.
		Margin(0, style.BoxMargin, marginBottom, style.BoxMargin).
		MarginBackground(lipgloss.Color(style.P.BgApp)).
		Padding(0, 2).
		Width(width)

	var b strings.Builder
	for i := 0; i < topGap; i++ {
		b.WriteByte('\n')
	}

	if len(parts) > 0 {
		sep := partStyle.Render(" · ")
		arrow := partStyle.Render("↳ ")

		b.WriteString(arrow)
		b.WriteString(parts[0])
		for i := 1; i < len(parts); i++ {
			b.WriteString(sep)
			b.WriteString(parts[i])
		}
	}

	if len(content) > 0 {
		if len(parts) > 0 {
			b.WriteString("\n\n")
		} else if topGap < 1 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(content, "\n\n"))
	}

	b.WriteByte('\n')

	return st.Render(b.String())
}

// drawCanvasSpan is a convenience for full-canvas blocks (topGap=1, marginBottom=0).
func drawCanvasSpan(parts []string, content []string, s style.StyleLabel, width int) string {
	return DrawCanvas(parts, content, s, 1, width, 0)
}

// drawToolBox is a convenience for tool call blocks (topGap=2, marginBottom=1).
func drawToolBox(parts []string, content []string, s style.StyleLabel, boxWidth int) string {
	return DrawCanvas(parts, content, s, 2, boxWidth, 1)
}

// drawUserBox is a convenience for user message blocks (topGap=1, marginBottom=1).
func drawUserBox(parts []string, content []string, s style.StyleLabel, boxWidth int) string {
	return DrawCanvas(parts, content, s, 1, boxWidth, 1)
}

// availablePartWidth returns the remaining printable width for a single header
// line part, given the box width and the sibling parts that will render beside it.
// It accounts for: box padding (4), the "↳ " prefix (2), and " · " separators (3 each).
// Minimum returned value is 4 to avoid degenerate truncation.
func availablePartWidth(boxWidth int, otherParts []string) int {
	innerW := boxWidth - 4
	used := 2 // ↳
	for _, p := range otherParts {
		used += lipgloss.Width(p)
	}
	used += len(otherParts) * 3 // " · " between each adjacent pair
	avail := innerW - used
	if avail < 4 {
		avail = 4
	}
	return avail
}

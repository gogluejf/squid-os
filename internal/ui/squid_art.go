package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

// squidGrid is a 24x14 pixel art of a squid.
// 1 = filled pixel, 0 = empty/background.
var squidGrid = [24][14]int{
	{0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0},
	{0, 1, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0},
	{0, 1, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0},
	{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1},
	{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
	{1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1},
	{1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	{0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0},
	{0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0},
	{0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0},
	{0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 0, 0, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 0, 0, 1, 1},
	{1, 0, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1},
	{0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0},
}

const (
	squidRows  = 24
	squidCols  = 14
	squidBlock = "█"
	squidEmpty = " "
)

// bgStyle paints BgApp so empty pixels and padding fill the viewport uniformly.
var bgStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(style.P.BgApp))

// filledStyle is the styled block for a filled pixel (electric blue on BgApp).
var filledStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(style.P.SquidPixel)).
	Background(lipgloss.Color(style.P.BgApp)).
	Width(1)

// emptyStyle is the styled space for an empty pixel — carries BgApp so the
// whole viewport stays uniform when no messages are present.
var emptyStyle = bgStyle

// RenderSquidArt renders the squid pixel art horizontally centered,
// placed below existingContentRows in the viewport.
// Followed by two trailing spaces after the art.
func RenderSquidArt(width, viewportHeight, existingContentRows int) string {
	// Build each line, horizontally centered
	var lines []string
	for r := 0; r < squidRows; r++ {
		var row strings.Builder
		for c := 0; c < squidCols; c++ {
			if squidGrid[r][c] == 1 {
				row.WriteString(filledStyle.Render(squidBlock))
				row.WriteString(filledStyle.Render(squidBlock))
			} else {
				row.WriteString(emptyStyle.Render(squidEmpty))
				row.WriteString(emptyStyle.Render(squidEmpty))
			}
		}
		line := row.String()
		renderedWidth := lipgloss.Width(line)
		totalPadding := width - renderedWidth
		if totalPadding < 0 {
			totalPadding = 0
		}
		leftPad := totalPadding / 2
		rightPad := totalPadding - leftPad
		padded := bgStyle.Render(strings.Repeat(" ", leftPad)) + line + bgStyle.Render(strings.Repeat(" ", rightPad))
		lines = append(lines, padded)
	}

	// The available space below existing content
	availableRows := viewportHeight - existingContentRows
	if availableRows <= 0 {
		return ""
	}

	// Center vertically in the available space
	topPad := (availableRows - squidRows) / 2
	if topPad < 0 {
		topPad = 0
	}
	bottomPad := availableRows - topPad - squidRows
	if bottomPad < 0 {
		bottomPad = 0
	}

	// Pre-rendered full-width BgApp line for padding rows
	bgLine := bgStyle.Render(strings.Repeat(" ", width))

	var b strings.Builder
	for i := 0; i < topPad; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(bgLine)
	}
	if topPad > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(strings.Join(lines, "\n"))
	for i := 0; i < bottomPad; i++ {
		b.WriteByte('\n')
		b.WriteString(bgLine)
	}

	// Two trailing spaces with BgApp so the cursor area blends
	b.WriteString(bgStyle.Render("  "))

	return b.String()
}

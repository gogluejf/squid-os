package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterData holds dynamic footer information
type FooterData struct {
	Model            string
	Provider         string
	TotalTokens      int
	TotalInputTokens int
	TotalOutTokens   int
	TokPerSec        float64
	Streaming        bool
}

// RenderFooter renders the fixed 2-line footer bar, always exactly `width` chars wide.
// Line 1: command hints (left) + model label (right) — Provider · Model
// Line 2: tok/s · ↓output[↑input] · total, right-aligned
func RenderFooter(data FooterData, width int) string {
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Width(width)

	bgSpace := func(n int) string {
		if n <= 0 {
			return ""
		}
		return FooterDimStyle.Render(strings.Repeat(" ", n))
	}

	sep := FooterDimStyle.Render(" · ")

	// ── Line 1: command hints (left) + model label (right) ─────────────
	left1 := " " + FooterKeyStyle.Render("/") + FooterDimStyle.Render("cmd") +
		FooterDimStyle.Render("  ") +
		FooterKeyStyle.Render("ctrl+l") + FooterDimStyle.Render(" load") +
		FooterDimStyle.Render("  ") +
		FooterKeyStyle.Render("ctrl+h") + FooterDimStyle.Render(" help")
	modelLabel := FooterDimStyle.Render(fmt.Sprintf("%s · %s", data.Provider, data.Model))

	gap1 := width - lipgloss.Width(left1) - lipgloss.Width(modelLabel)
	if gap1 < 1 {
		gap1 = 1
	}
	line1 := lineStyle.Render(left1 + bgSpace(gap1) + modelLabel)

	// ── Line 2: tok/s · ↓out[↑in] · total, right-aligned ───────────────
	var parts []string
	if data.Streaming && data.TokPerSec > 0 {
		parts = append(parts, FooterValueStyle.Render(fmt.Sprintf("%.1f tok/s", data.TokPerSec)))
	}

	parts = append(parts, FooterValueStyle.Render(tokenChipBoth(data.TotalOutTokens, data.TotalInputTokens, nil, nil)+" ["+formatTokens(data.TotalTokens)+"]"))

	right2 := sep + strings.Join(parts, sep)
	prefix2 := width - lipgloss.Width(right2)
	if prefix2 < 0 {
		prefix2 = 0
	}
	line2 := lineStyle.Render(bgSpace(prefix2) + right2)

	return line1 + "\n" + line2
}

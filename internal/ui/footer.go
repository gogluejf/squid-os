package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterData holds dynamic footer information
type FooterData struct {
	Model       string
	Provider    string
	TotalTokens int
	TokPerSec   float64
	Streaming   bool
	InThinking  bool
}

// RenderFooter renders the fixed 2-line footer bar, always exactly `width` chars wide.
// Each line is rendered independently with an explicit full-width gray background so
// there are no uncoloured gaps anywhere on the bar.
func RenderFooter(data FooterData, width int) string {
	// lineStyle produces a full-width gray block — applied per line so the background
	// is guaranteed to cover the entire terminal width even around already-styled text.
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Width(width)

	// bgSpace returns n spaces that carry the footer background colour.
	// Using FooterDimStyle (which sets bg=235) instead of plain strings prevents
	// "holes" in the background where unstyled spaces would otherwise appear.
	bgSpace := func(n int) string {
		if n <= 0 {
			return ""
		}
		return FooterDimStyle.Render(strings.Repeat(" ", n))
	}

	// ── Line 1: command hints (left) + model info (right) ────────────────
	left := " " + FooterKeyStyle.Render("/") + FooterDimStyle.Render("cmd") +
		FooterDimStyle.Render("  ") +
		FooterKeyStyle.Render("ctrl+l") + FooterDimStyle.Render(" load") +
		FooterDimStyle.Render("  ") +
		FooterKeyStyle.Render("ctrl+h") + FooterDimStyle.Render(" help")
	var right1 string
	modelLabel := fmt.Sprintf("%s - %s", data.Provider, data.Model)
	if data.Streaming && data.TokPerSec > 0 {
		right1 = FooterValueStyle.Render(fmt.Sprintf("%.1f tok/s", data.TokPerSec)) + FooterDimStyle.Render("  "+modelLabel)
	} else {
		right1 = FooterDimStyle.Render(modelLabel)
	}

	gap1 := width - lipgloss.Width(left) - lipgloss.Width(right1)
	if gap1 < 1 {
		gap1 = 1
	}
	line1 := lineStyle.Render(left + bgSpace(gap1) + right1)

	// ── Line 2: token count, right-aligned ───────────────────────────────
	right2 := FooterDimStyle.Render(fmt.Sprintf("%d tokens", data.TotalTokens))
	prefix2 := width - lipgloss.Width(right2)
	if prefix2 < 0 {
		prefix2 = 0
	}
	line2 := lineStyle.Render(bgSpace(prefix2) + right2)

	return line1 + "\n" + line2
}

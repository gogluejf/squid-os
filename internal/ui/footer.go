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
	ContextWindow    int // model context window in tokens; 0 if unknown
}

// RenderFooter renders the fixed 2-line footer bar, always exactly `width` chars wide.
// Line 1: command hints (left) + model label (right) — Provider · Model
// Line 2: tok/s · ↓output[↑input] · [tok/total] · context bar %, right-aligned
func RenderFooter(data FooterData, width int) string {
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(P.BgFooter)).
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
	modelLabel := FooterValueStyle.Render(data.Model)

	gap1 := width - lipgloss.Width(left1) - lipgloss.Width(modelLabel)
	if gap1 < 1 {
		gap1 = 1
	}
	line1 := lineStyle.Render(left1 + bgSpace(gap1) + modelLabel)

	// ── Line 2: [thinking: on/off] (left) + tok/s · ↓out[↑in] · [tok/total] · context bar % (right) ──
	var parts []string
	if data.Streaming && data.TokPerSec > 0 {
		parts = append(parts, FooterValueStyle.Render(fmt.Sprintf("%.1f tok/s", data.TokPerSec)))
	}

	tokLabel := FooterValueStyle.Render(tokenChipBoth(data.TotalOutTokens, data.TotalInputTokens, nil, nil)) +
		FooterValueStyle.Render(" [") + FooterValueStyle.Render(formatTokens(data.TotalTokens))
	if data.ContextWindow > 0 {
		tokLabel += FooterDimStyle.Render("/" + formatTokens(data.ContextWindow))
	}
	tokLabel += FooterValueStyle.Render("]")
	parts = append(parts, tokLabel)

	// Context usage bar: 20-char bar showing token usage vs context window
	ctxBar := renderContextBar(data.TotalTokens, data.ContextWindow)
	if ctxBar != "" {
		parts = append(parts, ctxBar)
	}

	right2 := sep + strings.Join(parts, sep)
	left2 := ""

	midSpace := width - lipgloss.Width(left2) - lipgloss.Width(right2)
	if midSpace < 1 {
		midSpace = 1
	}
	line2 := lineStyle.Render(left2 + bgSpace(midSpace) + right2)

	return line1 + "\n" + line2
}

// renderContextBar renders a 20-char context usage bar followed by the percentage.
// If contextWindow is 0 (unknown), returns "".
//
// The bar is 20 space characters: used portion on bg "237" (darker),
// remaining portion on bg "233" (lighter). Percentage follows after 1 space.
func renderContextBar(totalTokens, contextWindow int) string {
	if contextWindow == 0 {
		return ""
	}

	// Cap usage at 100%
	usagePct := float64(totalTokens) / float64(contextWindow) * 100.0
	if usagePct > 100 {
		usagePct = 100
	}
	if totalTokens == 0 {
		usagePct = 0
	}

	// 20 chars = 100%, each char = 5%
	darkChars := int(usagePct / 5.0)
	if darkChars > 20 {
		darkChars = 20
	}
	if darkChars < 0 {
		darkChars = 0
	}
	lightChars := 20 - darkChars

	pctStr := fmt.Sprintf("%.1f%%", usagePct)

	darkStyle := lipgloss.NewStyle().Background(lipgloss.Color(P.CtxBarUsed))
	lightStyle := lipgloss.NewStyle().Background(lipgloss.Color(P.CtxBarEmpty))

	bar := darkStyle.Render(strings.Repeat(" ", darkChars)) +
		lightStyle.Render(strings.Repeat(" ", lightChars))

	return FooterValueStyle.Render(pctStr+" ") + bar
}

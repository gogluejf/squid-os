package ui

import (
	"fmt"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/git"
	"squid-os/internal/style"
	"squid-os/internal/util"

	"github.com/charmbracelet/lipgloss"
)

// FooterData holds dynamic footer information
type FooterData struct {
	Model             string
	Provider          string
	TotalTokens       int
	TotalInputTokens  int
	TotalOutTokens    int
	TokPerSec         float64
	SeqDurMs          int64 // live sequence duration shown during streaming
	Streaming         bool
	ThinkingOn        bool   // thinking mode on/off (always visible)
	AuthorizationMode string // "auto", "ask-on-write", "ask-for-all"
	ContextWindow     int    // model context window in tokens; 0 if unknown
	WorkingDir        string
	Skill             config.SessionSkill
}

// RenderFooter renders the fixed 2-line footer bar, always exactly `width` chars wide.
// Line 1: command hints (left) + model label (right) — Provider · Model
// Line 2: tok/s · ↓output[↑input] · [tok/total] · context bar %, right-aligned
func RenderFooter(data FooterData, width int) string {
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgFooter)).
		Width(width)

	bgSpace := func(n int) string {
		if n <= 0 {
			return ""
		}
		return style.FooterDimStyle.Render(strings.Repeat(" ", n))
	}

	sep := style.FooterDimStyle.Render(" · ")

	// Thinking indicator — always visible, white text on footer bg.
	var thinkLabel string
	if data.ThinkingOn {
		thinkLabel = style.FooterValueStyle.Render("[thinking: on]")
	} else {
		thinkLabel = style.FooterValueStyle.Render("[thinking: off]")
	}

	// Authorization mode indicator — only the mode name is colored, brackets stay default.
	authColor := data.getAuthColor()
	modeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgFooter)).
		Foreground(lipgloss.Color(authColor))
	bracketStyle := style.FooterValueStyle
	authLabel := bracketStyle.Render("[") + modeStyle.Render(data.AuthorizationMode) + bracketStyle.Render("]")

	// Skill indicator — always shown; value colored with skill color.
	var skillLabel string
	skillStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgFooter)).
		Foreground(lipgloss.Color(style.P.TextSkill))
	if data.Skill.Next != nil {
		if *data.Skill.Next != "" {
			skillLabel = style.FooterValueStyle.Render("[skill: ") + skillStyle.Render(*data.Skill.Next) + style.FooterValueStyle.Render("]")
		} else {
			skillLabel = style.FooterValueStyle.Render("[skill: none]")
		}
	} else if data.Skill.Current != "" {
		skillLabel = style.FooterValueStyle.Render("[skill: ") + skillStyle.Render(data.Skill.Current) + style.FooterValueStyle.Render("]")
	} else {
		skillLabel = style.FooterValueStyle.Render("[skill: none]")
	}

	// ── Line 1: status chips + command hints (left) + model label (right) ─────────────
	left1 := " " + thinkLabel + authLabel + skillLabel + style.FooterDimStyle.Render("  ") +
		style.FooterKeyStyle.Render("/") + style.FooterDimStyle.Render("cmd") +
		style.FooterDimStyle.Render("  ") +
		style.FooterKeyStyle.Render("ctrl+h") + style.FooterDimStyle.Render(" help")

	modelLabel := style.FooterValueStyle.Render(data.Model)

	gap1 := width - lipgloss.Width(left1) - lipgloss.Width(modelLabel)
	if gap1 < 1 {
		gap1 = 1
	}
	line1 := lineStyle.Render(left1 + bgSpace(gap1) + modelLabel)

	// ── Line 2: working dir (left) + tok/s · ↓out[↑in] · [tok/total] · context bar % (right) ──
	var parts []string

	if data.Streaming && data.SeqDurMs > 0 {
		parts = append(parts, style.FooterDimStyle.Render(formatDuration(data.SeqDurMs)))
	}

	if data.Streaming && data.TokPerSec > 0 {
		parts = append(parts, style.FooterValueStyle.Render(fmt.Sprintf("%.1f tok/s", data.TokPerSec)))
	}

	tokLabel := style.FooterValueStyle.Render(tokenChipBoth(data.TotalOutTokens, data.TotalInputTokens, nil, nil)) +
		style.FooterValueStyle.Render(" [") + style.FooterValueStyle.Render(formatTokens(data.TotalTokens))
	if data.ContextWindow > 0 {
		tokLabel += style.FooterDimStyle.Render("/" + formatTokens(data.ContextWindow))
	}
	tokLabel += style.FooterValueStyle.Render("]")
	parts = append(parts, tokLabel)

	ctxBar := renderContextBar(data.TotalTokens, data.ContextWindow)
	if ctxBar != "" {
		parts = append(parts, ctxBar)
	}

	right2 := sep + strings.Join(parts, sep)

	var curDirLabel string
	if data.WorkingDir != "" {
		curDirLabel = style.FooterValueStyle.Render(util.FriendlyPath(git.CachedShortStat(data.WorkingDir)))
	}
	left2 := style.FooterValueStyle.Render(" ") + curDirLabel

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

	darkStyle := lipgloss.NewStyle().Background(lipgloss.Color(style.P.CtxBarUsed))
	lightStyle := lipgloss.NewStyle().Background(lipgloss.Color(style.P.CtxBarEmpty))

	bar := darkStyle.Render(strings.Repeat(" ", darkChars)) +
		lightStyle.Render(strings.Repeat(" ", lightChars))

	return style.FooterValueStyle.Render(pctStr+" ") + bar
}

// getAuthColor returns the ANSI color code for the authorization mode.
// auto = red (dangerous, no guard), ask-on-write = yellow/warning, ask-for-all = green (safest).
func (data FooterData) getAuthColor() string {
	switch data.AuthorizationMode {
	case "ask-for-all":
		return style.P.TextError // green
	case "ask-on-write":
		return style.P.TextWarning // yellow/orange
	default: // auto
		return style.P.TextSuccess // red
	}
}

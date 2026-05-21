package ui

import (
	"regexp"
	"strings"
	"sync"

	"squid-os/internal/style"
	"squid-os/internal/tools"

	"github.com/charmbracelet/lipgloss"
)

// styleContentLine handles a single line: check for heading first, then tool names,
// and always wrap in contentStyle. Also colorizes "- key:" patterns with label color.
func styleContentLine(line string, labelStyle lipgloss.Style, contentStyle lipgloss.Style) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "## ") {
		return labelStyle.Render(line)
	}

	// Colorize "- key:" patterns — if found, wraps all segments (per styled-inline-token pattern)
	styled := styleKeyLabelsInLine(line, contentStyle)
	if styled != line {
		return styled // already fully styled, skip tool-name pass
	}

	// No key pattern: style tool names, wrap rest in contentStyle
	return styleToolNamesInLine(line, contentStyle)
}

// keyValRe matches "- key:" patterns (e.g. "- compounder:", "- sop-entities:", "- appleIIe-engine:").
var keyValRe = regexp.MustCompile(`(-\s*\b[a-zA-Z][a-zA-Z0-9.\-]*:)`)

// styleKeyLabelsInLine finds "- key:" patterns and colorizes each key with label color,
// wrapping all other segments in contentStyle (per styled-inline-token pattern).
// Returns the original line untouched if no pattern is found.
func styleKeyLabelsInLine(line string, contentStyle lipgloss.Style) string {
	matches := keyValRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line
	}

	var b strings.Builder
	lastEnd := 0
	bg := contentStyle.GetBackground()
	valueFg := lipgloss.Color(style.P.TextPrimary)

	for _, groups := range matches {
		matchStart := groups[0]
		matchEnd := groups[1]

		if matchStart > lastEnd {
			b.WriteString(contentStyle.Render(line[lastEnd:matchStart]))
		}

		st := lipgloss.NewStyle().Foreground(valueFg).Background(bg)
		b.WriteString(st.Render(line[matchStart:matchEnd]))
		lastEnd = matchEnd
	}

	if lastEnd < len(line) {
		b.WriteString(contentStyle.Render(line[lastEnd:]))
	}

	return b.String()
}

// toolNamesRe matches tool names as whole words. Built once lazily.
var (
	toolNamesReOnce sync.Once
	toolNamesRe     *regexp.Regexp
)

func getToolNamesRe() *regexp.Regexp {
	toolNamesReOnce.Do(func() {
		var parts []string
		for _, t := range tools.GetTools() {
			parts = append(parts, regexp.QuoteMeta(t.Name))
		}
		toolNamesRe = regexp.MustCompile(`\b(` + strings.Join(parts, "|") + `)\b`)
	})
	return toolNamesRe
}

// styleToolNamesInLine finds tool names in a raw text line and styles each
// occurrence with its tool's label foreground + wrapStyle's background,
// wrapping non-tool segments in wrapStyle.
func styleToolNamesInLine(line string, wrapStyle lipgloss.Style) string {
	re := getToolNamesRe()
	matches := re.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return wrapStyle.Render(line)
	}

	var b strings.Builder
	lastEnd := 0

	bg := wrapStyle.GetBackground()

	for _, groups := range matches {
		matchStart := groups[0]
		matchEnd := groups[1]
		matchText := line[matchStart:matchEnd]

		if matchStart > lastEnd {
			b.WriteString(wrapStyle.Render(line[lastEnd:matchStart]))
		}

		if t := tools.GetRegistry().Get(matchText); t != nil {
			fg := t.Style.Label.GetForeground()
			st := lipgloss.NewStyle().Foreground(fg).Background(bg)
			b.WriteString(st.Render(matchText))
		} else {
			b.WriteString(wrapStyle.Render(matchText))
		}

		lastEnd = matchEnd
	}

	if lastEnd < len(line) {
		b.WriteString(wrapStyle.Render(line[lastEnd:]))
	}

	return b.String()
}

// styleParamValue styles tool names in a param value, wrapping in paramStyle.
func styleParamValue(value string, paramStyle lipgloss.Style) string {
	return styleToolNamesInLine(value, paramStyle)
}

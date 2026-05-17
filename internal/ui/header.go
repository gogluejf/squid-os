package ui

import (
	"strings"

	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
)

// HeaderData holds header information
type HeaderData struct {
	Incognito bool
}

// RenderHeader renders the top header bar, including the incognito indicator when active.
func RenderHeader(data HeaderData, width int) string {
	if !data.Incognito {
		return style.TopHeaderStyle.Width(width).Render("squid-os v0.1")
	}
	headerStyle := style.IncognitoHeaderStyle.Width(width)
	title := "squid-os v0.1"
	label := "👻 incognito"
	titleWidth := lipgloss.Width(style.IncognitoHeaderStyle.Render(title))
	labelWidth := lipgloss.Width(style.IncognitoHeaderStyle.Render(label))
	gap := width - titleWidth - labelWidth
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Render(title + strings.Repeat(" ", gap) + label)
}

package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HeaderData holds header information
type HeaderData struct {
	Incognito bool
}

// RenderHeader renders the top header bar, including the incognito indicator when active.
func RenderHeader(data HeaderData, width int) string {
	if !data.Incognito {
		return TopHeaderStyle.Width(width).Render("rig-chat v0.1")
	}
	headerStyle := IncognitoHeaderStyle.Width(width)
	title := "rig-chat v0.1"
	label := "👻 incognito"
	titleWidth := lipgloss.Width(IncognitoHeaderStyle.Render(title))
	labelWidth := lipgloss.Width(IncognitoHeaderStyle.Render(label))
	gap := width - titleWidth - labelWidth
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Render(title + strings.Repeat(" ", gap) + label)
}

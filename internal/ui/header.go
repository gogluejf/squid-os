package ui

import (
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/style"
	"squid-os/internal/util"
	"squid-os/internal/version"

	"github.com/charmbracelet/lipgloss"
)

// HeaderData holds header information
type HeaderData struct {
	Incognito bool
	Session   config.SessionInfo
}

// RenderHeader renders the top header bar, including the incognito indicator and session name.
func RenderHeader(data HeaderData, width int) string {
	var bgStyle lipgloss.Style
	if data.Incognito {
		bgStyle = lipgloss.NewStyle().Background(lipgloss.Color(style.P.BgIncognito))
	} else {
		bgStyle = lipgloss.NewStyle().Background(lipgloss.Color(style.P.BgFooter))
	}

	title := bgStyle.Bold(true).Foreground(lipgloss.Color(style.P.TextSecondary)).Padding(0, 1).Render("squid-os " + version.Full())

	// Build right-side session label
	var right string
	if data.Incognito {
		right = bgStyle.Bold(true).Foreground(lipgloss.Color(style.P.TextPrimary)).Render("👻 incognito")
	} else {
		muted := bgStyle.Foreground(lipgloss.Color(style.P.TextMuted))
		primary := bgStyle.Bold(true).Foreground(lipgloss.Color(style.P.TextSecondary))
		if data.Session.Name != "" {
			name := primary.Render(data.Session.Name)
			if !data.Session.ModTime.IsZero() {
				ts := muted.Render(util.FriendlyModDate(data.Session.ModTime)) + muted.Render(" · ")
				right = ts + name
			} else {
				right = name
			}
		} else {
			right = muted.Render("not saved")
		}
	}

	gap := width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	space := bgStyle.Render(strings.Repeat(" ", gap))

	lineStyle := bgStyle.Width(width)
	return lineStyle.Render(title + space + right)
}

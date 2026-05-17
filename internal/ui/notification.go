package ui

import (
	"strings"

	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
)

type NotificationLevel int

const (
	NotificationInfo    NotificationLevel = iota
	NotificationWarning                   // reserved, yellow 214
	NotificationError                     // red 196
)

type Notification struct {
	Level   NotificationLevel
	Message string
}

func (n Notification) Empty() bool { return n.Message == "" }

// RenderStatusLine renders the fixed status row between the viewport and textarea.
// Notification is left-aligned; attachmentChip (pre-rendered, may be "") is right-aligned.
// Always returns a full-width string so the row is stable (no layout shift).
func RenderStatusLine(n Notification, attachmentChip string, width int) string {
	var left string
	switch {
	case n.Empty():
		// nothing
	case n.Level == NotificationError:
		left = style.ErrorStyle.Render("✗ " + n.Message)
	case n.Level == NotificationWarning:
		left = style.WarningStyle.Render("⚠ " + n.Message)
	default:
		left = style.InfoStyle.Render("· " + n.Message)
	}

	if left == "" && attachmentChip == "" {
		return style.StatusLineStyle.Render(strings.Repeat(" ", width))
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(attachmentChip)
	if gap < 1 {
		gap = 1
	}
	mid := style.StatusLineStyle.Render(strings.Repeat(" ", gap))
	return left + mid + attachmentChip
}

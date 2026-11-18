package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

type NotificationLevel int

const (
	NotificationInfo    NotificationLevel = iota
	NotificationWarning // reserved, yellow 214
	NotificationError   // red 196
)

type Notification struct {
	Level   NotificationLevel
	Message string
}

func (n Notification) Empty() bool { return n.Message == "" }

// QueueCounts holds pending queued-input counts for the status row.
type QueueCounts struct {
	Step int
	Turn int
}

// RenderQueueChip builds the right-hand queue counter chip:
// "3 next step · 2 after turn". Zero groups are hidden; narrow widths fall
// back to the abbreviated "S:3 T:2" form. Returns "" when both are zero.
func RenderQueueChip(step, turn int, width int) string {
	if step <= 0 && turn <= 0 {
		return ""
	}
	var parts []string
	if step > 0 {
		parts = append(parts, itoa(step)+" next step")
	}
	if turn > 0 {
		parts = append(parts, itoa(turn)+" after turn")
	}
	full := strings.Join(parts, " · ")
	if lipgloss.Width(full) <= width {
		return full
	}
	abbrev := ""
	if step > 0 {
		abbrev += "S:" + itoa(step)
	}
	if turn > 0 {
		if abbrev != "" {
			abbrev += " "
		}
		abbrev += "T:" + itoa(turn)
	}
	// Even the abbreviated form must never exceed the reserved width.
	if lipgloss.Width(abbrev) > width {
		runes := []rune(abbrev)
		if width > 0 {
			return string(runes[:width])
		}
		return ""
	}
	return abbrev
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// RenderStatusLine renders the fixed status row between the viewport and textarea.
// Notification is left-aligned; queueChip (pre-rendered, may be "") is right-aligned.
// The right side is always reserved so the footer below never shifts; the left
// notification truncates when both sides would exceed width.
// Always returns a full-width string so the row is stable (no layout shift).
func RenderStatusLine(n Notification, queueChip string, width int) string {
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

	rightW := lipgloss.Width(queueChip)
	gapMin := 1
	if queueChip != "" {
		gapMin = 2
	}
	maxLeft := width - rightW - gapMin
	if maxLeft < 0 {
		maxLeft = 0
	}
	if lipgloss.Width(left) > maxLeft {
		left = truncateStyled(left, maxLeft)
	}
	gap := width - lipgloss.Width(left) - rightW
	if gap < gapMin {
		gap = gapMin
	}
	mid := style.StatusLineStyle.Render(strings.Repeat(" ", gap))
	return left + mid + queueChip
}

// truncateStyled shortens a rendered (styled) string to at most max display
// columns, appending an ellipsis marker when content was cut. Styles render
// plain text here, so rune slicing of the visible text is safe.
func truncateStyled(s string, max int) string {
	if max <= 1 {
		return s[:max]
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

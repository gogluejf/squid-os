package util

import (
	"fmt"
	"strings"
	"time"
)

// Truncate shortens s to maxLen characters, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// FriendlyModDate returns a human-readable relative time string for a modified date.
func FriendlyModDate(t time.Time) string {
	ago := time.Since(t)
	switch {
	case ago < time.Minute:
		return "just now"
	case ago < time.Hour:
		m := int(ago.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", m)
	case ago < 24*time.Hour:
		h := int(ago.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case ago < 7*24*time.Hour:
		d := int(ago.Hours() / 24)
		if d == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", d)
	default:
		return t.Format("Jan 2")
	}
}

// StripNewlines replaces newlines with spaces for clean single-line display.
func StripNewlines(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

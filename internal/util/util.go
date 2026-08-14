package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TruncateChars shortens s to maxLen characters, appending "..." if truncated.
func TruncateChars(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// TruncateWords limits s to maxWords, cutting at word boundaries.
func TruncateWords(s string, maxWords int) string {
	if maxWords <= 0 {
		return ""
	}
	words := strings.Fields(s)
	if len(words) <= maxWords {
		return s
	}
	return strings.Join(words[:maxWords], " ")
}

// FriendlyModDate returns a human-readable relative time string for a modified date.
func FriendlyModDate(t time.Time) string {
	return FriendlyHistoryDate(t, time.Now())
}

// FriendlyHistoryDate returns a compact history timestamp label relative to now.
func FriendlyHistoryDate(t, now time.Time) string {
	ago := now.Sub(t)
	if ago < 0 {
		ago = 0
	}
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
	case ago < 30*24*time.Hour:
		w := int(ago.Hours() / (24 * 7))
		if w < 1 {
			w = 1
		}
		if w == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", w)
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	default:
		return t.Format("Jan 2, 2006")
	}
}

// StripNewlines replaces newlines with spaces for clean single-line display.
func StripNewlines(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

// ComputeChecksum returns the hex-encoded SHA256 hash of data.
func ComputeChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// EqualStringSlices reports whether two string slices have identical values in order.
func EqualStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SetsEqual reports whether two slices contain the same elements, ignoring order.
// E must be comparable.
func SetsEqual[S ~[]E, E comparable](a, b S) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[E]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

// FriendlyPath replaces the user's home directory prefix with "~".
// Idempotent — calling it on an already-shrunk path is a no-op.
func FriendlyPath(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// ExpandHome expands a leading ~/ to the user's home directory and cleans the path.
func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, _ := os.UserHomeDir()
		p = strings.Replace(p, "~", home, 1)
	}
	return filepath.Clean(p)
}

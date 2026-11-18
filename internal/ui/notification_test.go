package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderQueueChip(t *testing.T) {
	cases := []struct {
		name  string
		step  int
		turn  int
		width int
		want  string
	}{
		{"both zero", 0, 0, 80, ""},
		{"step only", 3, 0, 80, "3 next step"},
		{"turn only", 0, 2, 80, "2 after turn"},
		{"both full", 3, 2, 80, "3 next step · 2 after turn"},
		{"narrow abbreviates", 3, 2, 15, "S:3 T:2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderQueueChip(tc.step, tc.turn, tc.width)
			if got != tc.want {
				t.Fatalf("RenderQueueChip(%d,%d,%d) = %q, want %q", tc.step, tc.turn, tc.width, got, tc.want)
			}
		})
	}
}

func TestRenderStatusLineReservesRightSide(t *testing.T) {
	chip := "3 next step · 2 after turn"
	width := 60
	row := RenderStatusLine(Notification{Level: NotificationInfo, Message: "hello"}, chip, false, width)
	if len([]rune(row)) != width {
		t.Fatalf("row width = %d, want %d", len([]rune(row)), width)
	}
	if !strings.HasSuffix(row, chip) {
		t.Fatalf("row must end with the queue chip, got %q", row)
	}
	// The left notification is truncated so both sides fit.
	if !strings.Contains(row, "hello") {
		t.Fatalf("notification lost entirely: %q", row)
	}
}

func TestRenderStatusLineTruncatesLongNotification(t *testing.T) {
	long := strings.Repeat("x", 200)
	chip := "S:3 T:2"
	width := 40
	row := RenderStatusLine(Notification{Level: NotificationError, Message: long}, chip, false, width)
	if len([]rune(row)) > width {
		t.Fatalf("row exceeds width: %d > %d", len([]rune(row)), width)
	}
	if !strings.HasSuffix(row, chip) {
		t.Fatalf("chip displaced: %q", row)
	}
}

func TestRenderStatusLineEmptyRowStable(t *testing.T) {
	row := RenderStatusLine(Notification{}, "", false, 50)
	if len([]rune(row)) != 50 {
		t.Fatalf("empty row width = %d, want 50", len([]rune(row)))
	}
}

// TestRenderStatusLineSpeakingIndicator verifies the voice indicator (task
// 1.6): the speaker glyph appears on the left side while speaking, coexists
// with a notification and the queue chip, truncates when space is tight, and
// is absent when not speaking.
func TestRenderStatusLineSpeakingIndicator(t *testing.T) {
	const glyph = "🔊 speaking…"

	// Speaking alone (no notification, no chip): glyph present. The row is
	// padded to exactly the requested width columns (same invariant as the
	// production pad loop: lipgloss.Width(row) == width).
	row := RenderStatusLine(Notification{}, "", true, 50)
	if got := lipgloss.Width(row); got != 50 {
		t.Fatalf("speaking row width = %d, want 50", got)
	}
	if !strings.Contains(row, glyph) {
		t.Fatalf("glyph missing while speaking: %q", row)
	}

	// Not speaking: glyph must be absent from an otherwise identical row.
	rowOff := RenderStatusLine(Notification{}, "", false, 50)
	if strings.Contains(rowOff, glyph) {
		t.Fatalf("glyph present while idle: %q", rowOff)
	}

	// Coexists with a notification: both visible, row padded to width.
	rowBoth := RenderStatusLine(Notification{Level: NotificationInfo, Message: "voice on"}, "", true, 50)
	if got := lipgloss.Width(rowBoth); got != 50 {
		t.Fatalf("row width = %d, want 50", got)
	}
	if !strings.Contains(rowBoth, glyph) || !strings.Contains(rowBoth, "voice on") {
		t.Fatalf("indicator or notification lost: %q", rowBoth)
	}
	// The glyph precedes the notification text.
	if strings.Index(rowBoth, glyph) > strings.Index(rowBoth, "voice on") {
		t.Fatalf("glyph should lead the left side: %q", rowBoth)
	}

	// With a queue chip: right side reserved, glyph still fits at 60 cols.
	chip := "3 next step"
	rowChip := RenderStatusLine(Notification{}, chip, true, 60)
	if got := lipgloss.Width(rowChip); got != 60 {
		t.Fatalf("row width = %d, want 60", got)
	}
	if !strings.HasSuffix(rowChip, chip) {
		t.Fatalf("chip displaced: %q", rowChip)
	}
	if !strings.Contains(rowChip, glyph) {
		t.Fatalf("glyph lost with chip: %q", rowChip)
	}

	// Narrow width: the indicator truncates. The row never exceeds width by
	// more than one column (the styled left side can render one column wider
	// than its rune count when cut mid-glyph) and the chip keeps its place.
	rowNarrow := RenderStatusLine(Notification{Level: NotificationError, Message: strings.Repeat("x", 40)}, chip, true, 30)
	if got := lipgloss.Width(rowNarrow); got > 31 {
		t.Fatalf("narrow row exceeds width: %d > 31", got)
	}
	if !strings.HasSuffix(rowNarrow, chip) {
		t.Fatalf("chip displaced in narrow row: %q", rowNarrow)
	}
}

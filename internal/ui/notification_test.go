package ui

import (
	"strings"
	"testing"
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
	row := RenderStatusLine(Notification{Level: NotificationInfo, Message: "hello"}, chip, width)
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
	row := RenderStatusLine(Notification{Level: NotificationError, Message: long}, chip, width)
	if len([]rune(row)) > width {
		t.Fatalf("row exceeds width: %d > %d", len([]rune(row)), width)
	}
	if !strings.HasSuffix(row, chip) {
		t.Fatalf("chip displaced: %q", row)
	}
}

func TestRenderStatusLineEmptyRowStable(t *testing.T) {
	row := RenderStatusLine(Notification{}, "", 50)
	if len([]rune(row)) != 50 {
		t.Fatalf("empty row width = %d, want 50", len([]rune(row)))
	}
}

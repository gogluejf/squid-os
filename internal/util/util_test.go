package util

import (
	"testing"
	"time"
)

func TestFriendlyHistoryDate(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{"just now", now.Add(-30 * time.Second), "just now"},
		{"one minute", now.Add(-1 * time.Minute), "1 min ago"},
		{"minutes", now.Add(-12 * time.Minute), "12 min ago"},
		{"one hour", now.Add(-1 * time.Hour), "1 hour ago"},
		{"hours", now.Add(-4 * time.Hour), "4 hours ago"},
		{"yesterday", now.Add(-25 * time.Hour), "yesterday"},
		{"days", now.Add(-5 * 24 * time.Hour), "5 days ago"},
		{"one week", now.Add(-8 * 24 * time.Hour), "1 week ago"},
		{"weeks", now.Add(-21 * 24 * time.Hour), "3 weeks ago"},
		{"same year date", time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC), "Jan 15"},
		{"older year full date", time.Date(2025, 12, 15, 9, 0, 0, 0, time.UTC), "Dec 15, 2025"},
		{"future", now.Add(1 * time.Hour), "just now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FriendlyHistoryDate(tt.time, now); got != tt.want {
				t.Fatalf("FriendlyHistoryDate() = %q, want %q", got, tt.want)
			}
		})
	}
}

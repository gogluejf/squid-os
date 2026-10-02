package app

import "testing"

func TestDiffKilled(t *testing.T) {
	cases := []struct {
		name   string
		before string
		after  string
		want   string
	}{
		{"kill to end", "hello world", "hello ", "world"},
		{"kill to start", "hello world", "world", "hello "},
		{"kill middle word", "a b c d", "a c d", "b "},
		{"no change", "abc", "abc", ""},
		{"insertion", "ac", "abc", ""},
		{"empty after", "gone", "", "gone"},
		{"unicode", "héllo wörld", "héllo ", "wörld"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diffKilled(tc.before, tc.after); got != tc.want {
				t.Errorf("diffKilled(%q, %q) = %q, want %q", tc.before, tc.after, got, tc.want)
			}
		})
	}
}

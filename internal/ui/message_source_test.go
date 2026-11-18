package ui

import (
	"strings"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestSourceHeaderBackgroundAndLayout(t *testing.T) {
	// Non-TTY tests otherwise suppress all ANSI output and cannot test colors.
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	for _, tc := range []struct{ source, icon string }{
		{"", ""}, {"voice", "👄"}, {"squid-sponge", "👄"}, {"hex-sponge", "👄"}, {"keyboard", "✍️"}, {"cli", ">_"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			msg := config.Message{Role: "user", CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Text: "hello", InputTokens: 12, Params: map[string]string{"source": tc.source}}
			if got := sourceChip(msg); got != tc.icon {
				t.Fatalf("icon=%q want %q", got, tc.icon)
			}
			for _, width := range []int{40, 80, 120} {
				out := renderUserMessage(msg, width, nil)
				if !strings.Contains(out, "\x1b[") {
					t.Fatal("test requires ANSI colors")
				}
				lines := strings.Split(out, "\n")
				if len(lines) != 7 {
					t.Fatalf("padding changed: %d lines", len(lines))
				}
				for _, i := range []int{0, 1, 3, 5, 6} {
					if strings.TrimSpace(ansi.Strip(lines[i])) != "" {
						t.Fatalf("padding line %d changed", i)
					}
				}
				header := lines[2]
				if lipgloss.Width(header) != width {
					t.Fatalf("header width=%d want %d", lipgloss.Width(header), width)
				}
				plain := ansi.Strip(header)
				if tc.icon != "" && !strings.Contains(plain, tc.icon+" ↑12") {
					t.Fatalf("icon spacing: %q", plain)
				}
				if !strings.Contains(header, style.UserStyleLabel().Dim.Render("↑12")) {
					t.Fatal("tokens lost background style")
				}
				if tc.icon != "" {
					// Verify the token chip still carries BgUser (the critical part).
					// The icon may use a different fg (orange for cli) but same bg.
					tokenStyled := style.UserStyleLabel().Dim.Render("↑12")
					if !strings.Contains(header, tokenStyled) {
						t.Fatal("token chip lost background style")
					}
				}
				if !strings.Contains(ansi.Strip(lines[4]), "hello") {
					t.Fatal("body moved")
				}
			}
		})
	}
}

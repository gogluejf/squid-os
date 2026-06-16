package ui

import (
	"strings"

	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
)

// Column widths for the 3-column layout.
const (
	col1Width = 16 // key or /command
	col2Width = 12 // shortcut (empty for non-command rows)
)

// helpBg is the base background style shared by every segment in the help overlay.
var helpBg = lipgloss.NewStyle().Background(lipgloss.Color(style.P.BgCode))

// padRight pads s to n runes with spaces.
func padRight(s string, n int) string {
	runes := []rune(s)
	if len(runes) >= n {
		return string(runes[:n])
	}
	return string(runes) + strings.Repeat(" ", n-len(runes))
}

// styledCol applies a lipgloss style with the shared background layered on top.
func styledCol(s lipgloss.Style, text string) string {
	return s.Background(lipgloss.Color(style.P.BgCode)).Render(text)
}

// descStyle is plain white, non-bold — easy to read.
var descStyle = lipgloss.NewStyle().
	Background(lipgloss.Color(style.P.BgCode)).
	Foreground(lipgloss.Color(style.P.TextPrimary))

// keyRow: [ key ][ empty ][ description ]
func keyRow(k, v string) string {
	c1 := styledCol(style.FooterKeyStyle, padRight(" "+k+" ", col1Width))
	c2 := helpBg.Render(strings.Repeat(" ", col2Width))
	c3 := descStyle.Render(v)
	return c1 + c2 + c3
}

// cmdRow: [ /cmd ][ empty ][ description ]
func cmdRow(cmd, v string) string {
	c1 := styledCol(style.FooterKeyStyle, padRight(" "+cmd+" ", col1Width))
	c2 := helpBg.Render(strings.Repeat(" ", col2Width))
	c3 := descStyle.Render(v)
	return c1 + c2 + c3
}

// cmdRowWithShortcut: [ /cmd ][ shortcut ][ description ]
func cmdRowWithShortcut(cmd, shortcut, v string) string {
	c1 := styledCol(style.FooterKeyStyle, padRight(" "+cmd+" ", col1Width))
	c2 := styledCol(style.FooterKeyStyle, padRight(" "+shortcut+" ", col2Width))
	c3 := descStyle.Render(v)
	return c1 + c2 + c3
}

// section builds a titled section: title, separator, rows.
func section(title string, rows []string) string {
	var b strings.Builder
	b.WriteString(styledCol(style.HeadingStyle, title))
	b.WriteString("\n")
	b.WriteString(styledCol(lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextDim)), strings.Repeat("─", col1Width+col2Width+40)))
	for _, r := range rows {
		b.WriteString("\n")
		b.WriteString(r)
	}
	return b.String()
}

// RenderHelp renders the full-screen help overlay.
func RenderHelp(width, height int) string {
	var b strings.Builder

	b.WriteString(styledCol(style.HeadingStyle, "squid-os"))
	b.WriteString("\n\n")

	// ── Chat + Streaming (always available) ──
	b.WriteString(section("Chat & Streaming", []string{
		keyRow("ctrl+e", "Expand/collapse thinking & tool results"),
		keyRow("ctrl+h", "Toggle this help"),
		keyRow("alt+t", "Toggle thinking mode"),
		keyRow("ctrl+1", "Cycle skill"),
		keyRow("shift+tab", "Cycle authorization mode"),
		keyRow("/", "Open command palette"),
		keyRow("esc", "Close overlay / dismiss palette"),
	}))
	b.WriteString("\n\n")

	// ── Chat ──
	b.WriteString(section("Chat", []string{
		keyRow("tab", "Numberize / next item"),
		keyRow("enter", "Send message"),
		keyRow("left alt+enter", "New line"),
		keyRow("ctrl+c", "Clear input / quit app"),
		keyRow("ctrl+d", "Destroy last message pair"),
		keyRow("ctrl+u", "Undo last destroy"),
		keyRow("alt+i", "Toggle incognito mode"),
		keyRow("ctrl+r", "Reverse search history"),
		keyRow("shift+↑/↓", "Scroll chat (3 lines)"),
		keyRow("pgup/pgdn", "Scroll chat (full page)"),
		keyRow("scroll", "Mouse wheel scrolls chat"),
		keyRow("shift+drag", "Select and copy text"),
	}))
	b.WriteString("\n\n")

	// ── Streaming ──
	b.WriteString(section("Streaming", []string{
		keyRow("ctrl+c", "Abort current assistant turn"),
	}))
	b.WriteString("\n\n")

	// ── Commands ──
	b.WriteString(section("Commands (/)", []string{
		cmdRowWithShortcut("/model", "alt+m", "Select model"),
		cmdRowWithShortcut("/skill", "alt+s", "Activate skill (next turn)"),
		cmdRowWithShortcut("/thinking", "alt+t", "Toggle thinking mode"),
		cmdRowWithShortcut("/auth-mode", "shift+tab", "Cycle authorization mode"),
		cmdRowWithShortcut("/save", "ctrl+s", "Save current session"),
		cmdRowWithShortcut("/load", "ctrl+l", "Load a saved session"),
		cmdRowWithShortcut("/clear", "ctrl+n", "Clear chat and start fresh"),
		cmdRowWithShortcut("/help", "ctrl+h", "Show help"),
		cmdRow("/system", "Load system prompt"),
		cmdRow("/exit", "Exit squid-os"),
	}))
	b.WriteString("\n\n")

	// ── Footer ──
	b.WriteString(styledCol(style.FooterDimStyle, "Press ctrl+h or esc to close"))

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Padding(1, 2).
		Background(lipgloss.Color(style.P.BgCode)).
		Foreground(lipgloss.Color(style.P.TextPrimary)).
		Render(b.String())
}

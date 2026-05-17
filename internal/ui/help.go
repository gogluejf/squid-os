package ui

import (
	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
)

// RenderHelp renders the full-screen help overlay
func RenderHelp(width, height int) string {
	title := style.HeadingStyle.Render("  squid-os")

	shortcuts := `
	 Keyboard Shortcuts
	 ` + lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextDim)).Render("────────────────────────────────") + `
	 ` + style.FooterKeyStyle.Render("ctrl+d") + `           Destroy last message pair
	 ` + style.FooterKeyStyle.Render("ctrl+u") + `           Undo last destroy
	 ` + style.FooterKeyStyle.Render("enter") + `            Send message
	 ` + style.FooterKeyStyle.Render("left alt+enter") + `   New line
	 ` + style.FooterKeyStyle.Render("ctrl+c") + `           Cancel inference / exit
	 ` + style.FooterKeyStyle.Render("ctrl+h") + `           Toggle this help
	 ` + style.FooterKeyStyle.Render("ctrl+e") + `           Expand/collapse thinking & tool results
	 ` + style.FooterKeyStyle.Render("ctrl+s") + `           Save session (edit name)
	 ` + style.FooterKeyStyle.Render("ctrl+l") + `           Load session
	 ` + style.FooterKeyStyle.Render("left alt+m") + `       Select model
	 ` + style.FooterKeyStyle.Render("ctrl+n") + `           New session / clear chat
	 ` + style.FooterKeyStyle.Render("left alt+i") + `       Toggle incognito mode
	 ` + style.FooterKeyStyle.Render("up/down") + `          Browse prompt history
	 ` + style.FooterKeyStyle.Render("shift+↑/↓") + `        Scroll chat (3 lines)
	 ` + style.FooterKeyStyle.Render("pgup/pgdn") + `        Scroll chat (full page)
	 ` + style.FooterKeyStyle.Render("scroll") + `           Mouse wheel scrolls chat
	 ` + style.FooterKeyStyle.Render("shift+drag") + `       Select and copy text
	 ` + style.FooterKeyStyle.Render("/") + `                Open command palette
	 ` + style.FooterKeyStyle.Render("ctrl+r") + `            Reverse search prompt history
	 ` + style.FooterKeyStyle.Render("esc") + `              Close overlay / dismiss palette`

	commands := `
  Slash Commands
  ` + lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextDim)).Render("────────────────────────────────") + `
  ` + style.CommandStyle.Render("/model") + `           Select model
  ` + style.CommandStyle.Render("/thinking") + `        Toggle thinking mode
  ` + style.CommandStyle.Render("/image") + `           Attach image to next message
  ` + style.CommandStyle.Render("/save") + `            Save current session
  ` + style.CommandStyle.Render("/load") + `            Load a saved session
  ` + style.CommandStyle.Render("/clear") + `           Clear chat and start fresh
  ` + style.CommandStyle.Render("/system") + `          Load system prompt
  ` + style.CommandStyle.Render("/exit") + `            Exit squid-os
  ` + style.CommandStyle.Render("/help") + `            Show this help`

	footer := "\n  " + style.FooterDimStyle.Render("Press ctrl+h or esc to close")

	content := title + "\n" + shortcuts + "\n" + commands + "\n" + footer

	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Padding(1, 2).
		Background(lipgloss.Color(style.P.BgCode)).
		Foreground(lipgloss.Color(style.P.TextPrimary))

	return style.Render(content)
}

package ui

import "github.com/charmbracelet/lipgloss"

// RenderHelp renders the full-screen help overlay
func RenderHelp(width, height int) string {
	title := HeadingStyle.Render("  rig-chat")

	shortcuts := `
  Keyboard Shortcuts
  ` + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("────────────────────────────────") + `
  ` + FooterKeyStyle.Render("enter") + `            Send message
  ` + FooterKeyStyle.Render("left alt+enter") + `   New line
  ` + FooterKeyStyle.Render("ctrl+c") + `           Cancel inference / exit
  ` + FooterKeyStyle.Render("ctrl+h") + `           Toggle this help
  ` + FooterKeyStyle.Render("ctrl+e") + `           Expand/collapse thinking
  ` + FooterKeyStyle.Render("ctrl+s") + `           Save session (edit name)
  ` + FooterKeyStyle.Render("ctrl+l") + `           Load session
  ` + FooterKeyStyle.Render("left alt+m") + `       Select model
  ` + FooterKeyStyle.Render("ctrl+n") + `           New session / clear chat
  ` + FooterKeyStyle.Render("left alt+i") + `       Toggle incognito mode
  ` + FooterKeyStyle.Render("up/down") + `          Browse prompt history
  ` + FooterKeyStyle.Render("shift+↑/↓") + `        Scroll chat (3 lines)
  ` + FooterKeyStyle.Render("pgup/pgdn") + `        Scroll chat (full page)
  ` + FooterKeyStyle.Render("scroll") + `           Mouse wheel scrolls chat
  ` + FooterKeyStyle.Render("shift+drag") + `       Select and copy text
  ` + FooterKeyStyle.Render("/") + `                Open command palette
  ` + FooterKeyStyle.Render("esc") + `              Close overlay / dismiss palette`

	commands := `
  Slash Commands
  ` + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("────────────────────────────────") + `
  ` + CommandStyle.Render("/model") + `           Select model
  ` + CommandStyle.Render("/thinking") + `        Toggle thinking mode
  ` + CommandStyle.Render("/image") + `           Attach image to next message
  ` + CommandStyle.Render("/save") + `            Save current session
  ` + CommandStyle.Render("/load") + `            Load a saved session
  ` + CommandStyle.Render("/clear") + `           Clear chat and start fresh
  ` + CommandStyle.Render("/system") + `          Load system prompt
  ` + CommandStyle.Render("/exit") + `            Exit rig-chat
  ` + CommandStyle.Render("/help") + `            Show this help`

	footer := "\n  " + FooterDimStyle.Render("Press ctrl+h or esc to close")

	content := title + "\n" + shortcuts + "\n" + commands + "\n" + footer

	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Padding(1, 2).
		Background(lipgloss.Color("234")).
		Foreground(lipgloss.Color("252"))

	return style.Render(content)
}

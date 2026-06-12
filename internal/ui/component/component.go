package component

import tea "github.com/charmbracelet/bubbletea"

// Component is the common interface for all overlay components
// (Picker, Prompt, Question). The app layer stores a single Component
// and dispatches through it.
type Component interface {
	Init(ctx any)
	HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd
	Render(width int) string
	RenderHeight() int
}

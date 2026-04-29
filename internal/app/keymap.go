package app

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Destroy        key.Binding
	UndoDestroy    key.Binding
	Send           key.Binding
	Cancel         key.Binding
	Help           key.Binding
	Expand key.Binding
	Save           key.Binding
	Load           key.Binding
	Model          key.Binding
	NewSession     key.Binding
	Incognito      key.Binding
	Quit           key.Binding
	Up             key.Binding
	Down           key.Binding
	Left           key.Binding
	Right          key.Binding
	Escape         key.Binding
	Tab            key.Binding
	ScrollUp       key.Binding
	ScrollDown     key.Binding
	PageUp         key.Binding
	PageDown       key.Binding
	HistorySearch  key.Binding
}

var keys = keyMap{
	Destroy: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "destroy last pair"),
	),
	UndoDestroy: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "undo destroy"),
	),
	Send: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "send message"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "cancel / exit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+h"),
		key.WithHelp("ctrl+h", "toggle help"),
	),
	Expand: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "expand/collapse"),
	),
	Save: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "save session"),
	),
	Load: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "load session"),
	),
	Model: key.NewBinding(
		key.WithKeys("alt+m"),
		key.WithHelp("alt+m", "select model"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session / clear"),
	),
	Incognito: key.NewBinding(
		key.WithKeys("alt+i"),
		key.WithHelp("alt+i", "toggle incognito"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("up", "previous"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("down", "next"),
	),
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("left", "confirm"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("right", "confirm"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "complete"),
	),
	ScrollUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "scroll up"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "scroll down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdown", "page down"),
	),
	HistorySearch: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "reverse search history"),
	),
}

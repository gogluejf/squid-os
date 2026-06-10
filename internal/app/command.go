package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// openCommandPicker opens the slash command palette.
func (m *Model) openCommandPicker() {
	m.activePicker = ui.Picker{
		Title:       "Commands",
		Items:       m.allCommands,
		DisplayMode: ui.ModeLabelDesc,
		MatchMode:   ui.MatchPrefix,
		OnConfirm: func(item ui.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			m.textarea.SetValue("")
			mm, cmd := m.executeCommand(item.Value)
			*m = mm.(Model)
			return cmd
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.setChatMode()
		},
	}
	m.pickerContext = "command"
	m.mode = ModeCommandPicker
	m.recalcLayout()
}

// executeCommand runs a slash command selected from the command palette.
func (m Model) executeCommand(name string) (tea.Model, tea.Cmd) {

	switch name {
	case "exit":
		_ = config.SaveHistory(m.paths, m.history)
		return m, tea.Quit

	case "help":
		m.mode = ModeHelp
		return m, nil

	case "model":
		return m.openModelPicker()

	case "skill":
		return m.openSkillPicker()

	case "thinking":
		return m.toggleThinking()

	case "auth-mode":
		_, cmd := m.cycleAuthorization()
		return m, cmd

	case "save":
		return m.openSaveSessionPrompt()

	case "load":
		return m.openSessionPicker()

	case "clear":
		return m.clearSession()

	case "system":
		return m.openSystemPicker()
	}

	return m, m.setChatMode()
}

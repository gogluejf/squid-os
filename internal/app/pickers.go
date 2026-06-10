package app

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// handleActivePicker delegates all key handling to the Picker component.
// Callbacks (OnConfirm, OnCancel, OnSelectionChange) do the real work.
func (m Model) handleActivePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle viewport scroll keys first (these bypass the picker).
	switch {
	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return m, nil

	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return m, nil

	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return m, nil
	}

	cmd := m.activePicker.HandleKey(msg, &m)

	// For command picker, keep textarea in sync with the filter
	if m.pickerContext == "command" {
		m.textarea.SetValue("/" + m.activePicker.Filter)
	}

	// Filter changes may alter render height.
	(&m).recalcLayout()
	return m, cmd
}

// handleSavePromptKey handles key input while the save-name prompt overlay is active.
func (m Model) handleSavePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		return m, m.setChatMode()

	case key.Matches(msg, keys.Send):
		nm, _ := m.saveAs(m.savePrompt.Name, false)
		return nm, nm.setChatMode()

	default:
		s := msg.String()
		if s == "backspace" {
			if len(m.savePrompt.Name) > 0 {
				m.savePrompt.Name = m.savePrompt.Name[:len(m.savePrompt.Name)-1]
			}
		} else if len(s) == 1 {
			m.savePrompt.Name += s
		}
		return m, nil
	}
}

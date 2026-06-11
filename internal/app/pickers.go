package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleActivePicker delegates all key handling to the Picker component.
// Callbacks (OnConfirm, OnCancel, OnSelectionChange) do the real work.
func (m Model) handleActivePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle viewport scroll keys first (these bypass the picker).
	if m.handleViewportScroll(msg) {
		return m, nil
	}

	cmd := m.activePicker.HandleKey(msg, &m)

	// For command picker, keep textarea in sync with the filter
	if m.pickerContext == "command" && cmd == nil {
		m.textarea.SetValue("/" + m.activePicker.Filter)
	}

	// Filter changes may alter render height.
	(&m).recalcLayout()
	return m, cmd
}

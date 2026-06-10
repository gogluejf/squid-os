package app

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/ui"
)

// handleActivePicker handles key input for any active picker using the unified
// Picker component.  Delegates navigation/filtering to activePicker.HandleKey
// and dispatches Select/Cancel actions.
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

	action := m.activePicker.HandleKey(msg, &m)

	switch action {
	case ui.ActionCancel:
		if m.pickerContext == "session" && m.sessionSnapshot != nil {
			m.session = *m.sessionSnapshot
			m.sessionSnapshot = nil
			if m.session.file.Session.WorkingDir != "" {
				(&m).applyWorkingDir(m.session.file.Session.WorkingDir)
			}
			m.updateViewportContent()
		}
		return m, m.setChatMode()

	case ui.ActionSelect:
		return m.confirmActivePicker()
	}

	// For command picker, keep textarea in sync with the filter
	if m.pickerContext == "command" {
		m.textarea.SetValue("/" + m.activePicker.Filter)
	}

	// Filter changes may alter render height.
	(&m).recalcLayout()
	return m, nil
}

// confirmActivePicker dispatches confirmation to the owning domain's confirm function.
func (m Model) confirmActivePicker() (tea.Model, tea.Cmd) {
	selected := m.activePicker.SelectedItem()

	switch m.pickerContext {
	case "command":
		m.textarea.SetValue("")
		return m.executeCommand(selected.Value)

	case "model":
		m = m.confirmModelPicker(selected)

	case "skill":
		m = m.confirmSkillPicker(selected)

	case "session":
		m = m.confirmSessionPicker(selected)

	case "system":
		m = m.confirmSystemPicker(selected)
	}

	(&m).updateViewportContent()
	return m, m.setChatMode()
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

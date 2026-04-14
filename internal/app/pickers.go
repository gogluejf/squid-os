package app

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/config"
	"rig-chat/internal/ui"
)

// handlePickerKey handles key input while any picker overlay is visible
// (model, session, file, or system prompt).
func (m Model) handlePickerKey(msg tea.KeyMsg, pickerType string) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		if pickerType == "session" && m.sessionSnapshot != nil {
			snap := m.sessionSnapshot
			m.messages = snap.messages
			m.renderedMessages = nil
			m.session = snap.session
			m.totalTokens = snap.totalTokens
			m.settings = snap.settings
			m.sessionSnapshot = nil
			m.updateViewportContent()
		}
		m.mode = ModeChat
		m.textarea.Focus()
		(&m).recalcLayout()
		return m, nil

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

	case key.Matches(msg, keys.Up):
		switch pickerType {
		case "model":
			m.modelPicker.MoveUp()
		case "session":
			m.sessionPicker.MoveUp()
			m = m.previewSession(m.sessionPicker.SelectedItem())
		case "image", "system":
			m.filePicker.MoveUp()
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		switch pickerType {
		case "model":
			m.modelPicker.MoveDown()
		case "session":
			m.sessionPicker.MoveDown()
			m = m.previewSession(m.sessionPicker.SelectedItem())
		case "image", "system":
			m.filePicker.MoveDown()
		}
		return m, nil

	case key.Matches(msg, keys.Send):
		return m.confirmPicker(pickerType)

	case key.Matches(msg, keys.Tab):
		switch pickerType {
		case "model":
			m.modelPicker.MoveDown()
		case "session":
			m.sessionPicker.MoveDown()
			m = m.previewSession(m.sessionPicker.SelectedItem())
		case "image", "system":
			m.filePicker.MoveDown()
		}
		return m, nil

	default:
		// Type to filter
		s := msg.String()
		switch pickerType {
		case "model":
			if len(s) == 1 {
				m.modelPicker.Filter += s
				m.modelPicker.Selected = 0
			} else if s == "backspace" && len(m.modelPicker.Filter) > 0 {
				m.modelPicker.Filter = m.modelPicker.Filter[:len(m.modelPicker.Filter)-1]
				m.modelPicker.Selected = 0
			}
		case "session":
			if len(s) == 1 {
				m.sessionPicker.Filter += s
				m.sessionPicker.Selected = 0
			} else if s == "backspace" && len(m.sessionPicker.Filter) > 0 {
				m.sessionPicker.Filter = m.sessionPicker.Filter[:len(m.sessionPicker.Filter)-1]
				m.sessionPicker.Selected = 0
			}
		case "image", "system":
			if len(s) == 1 {
				m.filePicker.Filter += s
				m.filePicker.Selected = 0
			} else if s == "backspace" && len(m.filePicker.Filter) > 0 {
				m.filePicker.Filter = m.filePicker.Filter[:len(m.filePicker.Filter)-1]
				m.filePicker.Selected = 0
			}
		}
		(&m).recalcLayout()
		return m, nil
	}
}

// confirmPicker applies the selected picker item for the given picker type.
func (m Model) confirmPicker(pickerType string) (tea.Model, tea.Cmd) {
	switch pickerType {
	case "model":
		selected := m.modelPicker.SelectedItem()
		if selected != "" {
			for _, e := range m.modelEntries {
				if fmt.Sprintf("%-12s  %s", e.Provider, e.ID) == selected {
					m.settings.Model = e.ID
					m.settings.Provider = e.Provider
					break
				}
			}
			_ = config.SaveSettings(m.paths, m.settings)
		}

	case "session":
		// Session is already previewed; just persist the selection and clear snapshot
		selected := m.sessionPicker.SelectedItem()
		if selected != "" {
			m.settings.Model = m.session.Session.Model
			m.settings.Provider = m.session.Session.Provider
			m.settings.Thinking = m.session.Session.Thinking
			if m.incognito != true {
				m.settings.LastSessionName = selected
				_ = config.SaveSettings(m.paths, m.settings)
			}
		}
		m.sessionSnapshot = nil

	case "image":
		selected := m.filePicker.SelectedItem()
		if selected != "" {
			m.attachedImage = selected
			m.recalcLayout()
		}

	case "system":
		selected := m.filePicker.SelectedItem()
		if selected != "" {
			m.settings.SystemPromptFile = selected
			_ = config.SaveSettings(m.paths, m.settings)
		}
	}

	m.mode = ModeChat
	m.textarea.Focus()
	(&m).recalcLayout()
	return m, nil
}

// handleSavePromptKey handles key input while the save-name prompt overlay is active.
func (m Model) handleSavePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		m.mode = ModeChat
		m.textarea.Focus()
		(&m).recalcLayout()
		return m, nil

	case key.Matches(msg, keys.Send):
		nm, cmd := m.saveAs(m.savePrompt.Name)
		nm.mode = ModeChat
		nm.textarea.Focus()
		(&nm).recalcLayout()
		return nm, cmd

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

// executeCommand runs a slash command selected from the command palette.
func (m Model) executeCommand(name string) (tea.Model, tea.Cmd) {
	m.cmdPalette.Reset()
	m.textarea.SetValue("")

	switch name {
	case "exit":
		_ = config.SaveHistory(m.paths, m.history)
		return m, tea.Quit

	case "help":
		m.mode = ModeHelp
		return m, nil

	case "model":
		// Scan models asynchronously
		m.mode = ModeChat // temporarily back to chat while loading
		return m, m.scanModelsCmd()

	case "thinking":
		m.thinkingToggle = ui.NewThinkingToggle(m.settings.Thinking)
		m.mode = ModeChat
		// Simple toggle for now
		m.settings.Thinking = !m.settings.Thinking
		_ = config.SaveSettings(m.paths, m.settings)
		m.textarea.Focus()
		return m, nil

	case "image":
		// List image files — for now just let user type a path
		m.filePicker = ui.NewPickerList("Attach Image (type path)", []string{})
		m.filePickerFor = "image"
		m.mode = ModeFilePicker
		(&m).recalcLayout()
		return m, nil

	case "save":
		return m.startManualSave()

	case "load":
		return m.startLoad()

	case "clear":
		return m.clearSession()

	case "system":
		prompts := config.ListSystemPrompts(m.paths)
		m.filePicker = ui.NewPickerList("System Prompt", prompts)
		m.filePickerFor = "system"
		m.mode = ModeFilePicker
		(&m).recalcLayout()
		return m, nil
	}

	m.mode = ModeChat
	m.textarea.Focus()
	return m, nil
}

// keep time import used in executeCommand implicitly via startManualSave
var _ = time.Now

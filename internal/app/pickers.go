package app

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// handlePickerKey handles key input while any picker overlay is visible
// (model, session, file, or system prompt).
func (m Model) handlePickerKey(msg tea.KeyMsg, pickerType string) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		if pickerType == "session" && m.sessionSnapshot != nil {
			m.session = *m.sessionSnapshot
			m.sessionSnapshot = nil
			// Restore working dir from the restored session
			if m.session.file.Session.WorkingDir != "" {
				(&m).applyWorkingDir(m.session.file.Session.WorkingDir)
			}
			m.updateViewportContent()
		}
		return m, m.setChatMode()

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
			m = m.previewSession(m.sessionPickerSelectedRaw())
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
			m = m.previewSession(m.sessionPickerSelectedRaw())
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
			m = m.previewSession(m.sessionPickerSelectedRaw())
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
				m = m.previewSession(m.sessionPickerSelectedRaw())
			} else if s == "backspace" && len(m.sessionPicker.Filter) > 0 {
				m.sessionPicker.Filter = m.sessionPicker.Filter[:len(m.sessionPicker.Filter)-1]
				m.sessionPicker.Selected = 0
				m = m.previewSession(m.sessionPickerSelectedRaw())
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
				name := modelBasename(e.ID)
				label := fmt.Sprintf("%-12s  %s", e.Provider, name)
				if e.ContextLength > 0 {
					label += "  " + formatContextLength(e.ContextLength)
				}
				if label == selected {
					if m.settings.Model != e.ID {
						oldModel := modelBasename(m.settings.Model)
						(&m).session.pushModelSwitchMsg(oldModel, name)
					}
					(&m).session.updateConfigMsg(e.Provider, e.ID, m.settings.Thinking)
					m.settings.Model = e.ID
					m.settings.Provider = e.Provider
					m.settings.ContextWindow = e.ContextLength
					break
				}
			}
			(&m).session.invalidateRenderAll()
			_ = config.SaveSettings(m.paths, m.settings)
			(&m).setNotification(ui.NotificationInfo, "switched to model: "+modelBasename(m.settings.Model))
		}

	case "session":
		selected := m.sessionPickerSelectedRaw()
		if selected != "" && !m.incognito {
			m.settings.LastSessionName = selected
			_ = config.SaveSettings(m.paths, m.settings)
		}
		m.session.setFrom(m.session.file)
		m.sessionSnapshot = nil
		if selected != "" {
			(&m).setNotification(ui.NotificationInfo, "session loaded from "+config.SessionPath(m.paths, selected))
		}

	case "image":
		selected := m.filePicker.SelectedItem()
		if selected != "" {
			m.attachedImage = selected
			m.recalcLayout()
		}

	case "system":
		selected := m.filePicker.SelectedItem()
		if selected != "" {
			changed := false
			if m.settings.SystemPromptFile != "" && m.settings.SystemPromptFile != selected {
				(&m).session.updateSystemPromptMsg(m.settings.SystemPromptFile, selected, m.paths)
				changed = true
			} else {
				// First set — update sys0 directly
				for i := range m.session.file.Messages {
					if m.session.file.Messages[i].ID == "sys0" {
						newContent := config.LoadSystemPrompt(m.paths, selected)
						m.session.file.Messages[i].Text = newContent
						m.session.file.Messages[i].InputTokens = countTokensApprox(newContent)
						changed = true
						break
					}
				}
			}
			if changed {
				(&m).session.invalidateRenderAll()
			}
			m.settings.SystemPromptFile = selected
			_ = config.SaveSettings(m.paths, m.settings)
		}
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
		return m, m.scanModelsCmd()

	case "thinking":
		m.thinkingToggle = ui.NewThinkingToggle(m.settings.Thinking)
		m.settings.Thinking = !m.settings.Thinking
		_ = config.SaveSettings(m.paths, m.settings)
		(&m).session.updateConfigMsg(m.settings.Provider, m.settings.Model, m.settings.Thinking)
		(&m).session.invalidateRenderAll()
		(&m).updateViewportContent()
		if m.settings.Thinking {
			(&m).setNotification(ui.NotificationInfo, "thinking is now on")
		} else {
			(&m).setNotification(ui.NotificationInfo, "thinking is now off")
		}
		return m, m.setChatMode()

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

	return m, m.setChatMode()
}

// keep time import used in executeCommand implicitly via startManualSave
var _ = time.Now

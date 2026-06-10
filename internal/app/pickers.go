package app

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
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

	action := m.activePicker.HandleKey(msg)

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

	// Filter changes may alter render height.
	(&m).recalcLayout()
	return m, nil
}

// confirmActivePicker applies the selected picker item based on pickerContext.
func (m Model) confirmActivePicker() (tea.Model, tea.Cmd) {
	selected := m.activePicker.SelectedItem()

	switch m.pickerContext {
	case "model":
		m = m.confirmModelPicker(selected)

	case "skill":
		m = m.confirmSkillPicker(selected)

	case "session":
		m = m.confirmSessionPicker(selected)

	case "image":
		m = m.confirmImagePicker(selected)

	case "system":
		m = m.confirmSystemPicker(selected)
	}

	(&m).updateViewportContent()
	return m, m.setChatMode()
}

// confirmModelPicker applies a model selection using the PickerItem.Value as the model ID.
func (m Model) confirmModelPicker(item ui.PickerItem) Model {
	modelID := item.Value
	if modelID == "" {
		return m
	}
	entries := m.pickerPayload.([]chat.ModelEntry)
	var entry *chat.ModelEntry
	for i := range entries {
		if entries[i].ID == modelID {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return m
	}
	name := modelBasename(entry.ID)
	if m.settings.Model != entry.ID {
		oldModel := modelBasename(m.settings.Model)
		(&m).session.pushModelSwitchMsg(oldModel, name)
	}
	(&m).session.updateConfigMsg(entry.Provider, entry.ID, m.settings.Thinking)
	m.settings.Model = entry.ID
	m.settings.Provider = entry.Provider
	m.settings.ContextWindow = entry.ContextLength
	(&m).session.invalidateRenderAll()
	_ = config.SaveSettings(m.paths, m.settings)
	(&m).setNotification(ui.NotificationInfo, "switched to model: "+modelBasename(m.settings.Model))
	return m
}

// confirmSkillPicker applies a skill selection from PickerItem.Label.
func (m Model) confirmSkillPicker(item ui.PickerItem) Model {
	skillName := strings.TrimSpace(item.Label)
	if skillName == "(none)" {
		skillName = ""
	}
	current := m.session.file.Session.Skill.Current
	if m.session.file.Session.Skill.Next != nil {
		current = *m.session.file.Session.Skill.Next
	}
	if skillName != current {
		(&m).setSkill(skillName)
	}
	return m
}

// confirmSessionPicker loads the selected session by name from PickerItem.Value.
func (m Model) confirmSessionPicker(item ui.PickerItem) Model {
	selected := item.Value
	if selected != "" && !m.incognito {
		m.settings.LastSessionName = selected
		_ = config.SaveSettings(m.paths, m.settings)
	}
	m.session.setFrom(m.session.file)
	m.sessionSnapshot = nil
	if selected != "" {
		(&m).setNotification(ui.NotificationInfo, "session loaded from "+config.SessionPath(m.paths, selected))
	}
	return m
}

// confirmImagePicker sets the attached image path.
func (m Model) confirmImagePicker(item ui.PickerItem) Model {
	selected := item.Label
	if selected != "" {
		m.attachedImage = selected
		m.recalcLayout()
	}
	return m
}

// confirmSystemPicker loads a system prompt file.
func (m Model) confirmSystemPicker(item ui.PickerItem) Model {
	selected := item.Value
	if selected == "" {
		selected = item.Label
	}
	if selected != "" {
		changed := false
		if m.settings.SystemPromptFile != "" && m.settings.SystemPromptFile != selected {
			(&m).session.updateSystemPromptMsg(m.settings.SystemPromptFile, selected, m.paths)
			changed = true
		} else {
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
	return m
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
	m.cmdPickerVisible = false
	m.cmdPicker.Filter = ""
	m.cmdPicker.Selected = 0
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

	case "skill":
		return m.openSkillPicker()

	case "thinking":
		return m.toggleThinking()

	case "auth-mode":
		_, cmd := m.cycleAuthorization()
		return m, cmd

	case "image":
		// List image files — for now just let user type a path
		m.activePicker = ui.Picker{
			Title:       "Attach Image (type path)",
			Items:       []ui.PickerItem{},
			DisplayMode: ui.ModeSingleCol,
		}
		m.pickerContext = "image"
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
		items := make([]ui.PickerItem, len(prompts))
		for i, p := range prompts {
			items[i] = ui.PickerItem{
				Label: p,
				Value: p,
			}
		}
		m.activePicker = ui.Picker{
			Title:       "System Prompt",
			Items:       items,
			DisplayMode: ui.ModeSingleCol,
		}
		m.pickerContext = "system"
		m.mode = ModeFilePicker
		(&m).recalcLayout()
		return m, nil
	}

	return m, m.setChatMode()
}

// keep time import used in executeCommand implicitly via startManualSave
var _ = time.Now

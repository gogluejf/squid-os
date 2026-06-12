package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// openSystemPicker opens the system prompt file picker.
func (m Model) openSystemPicker() (Model, tea.Cmd) {
	prompts := config.ListSystemPrompts(m.paths)
	items := make([]ui.PickerItem, len(prompts))
	for i, p := range prompts {
		items[i] = ui.PickerItem{
			Label: p,
			Value: p,
		}
	}

	m.activePicker = ui.Picker{
		Title:        "System Prompt",
		Items:        items,
		DefaultValue: m.settings.SystemPromptFile,
		OnConfirm: func(item ui.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			*m = m.confirmSystemPicker(item)
			m.updateViewportContent()
			return m.setChatMode()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.setChatMode()
		},
	}
	m.pickerContext = "system"
	m.mode = ModeComponentPicker
	(&m).recalcLayout()
	return m, nil
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

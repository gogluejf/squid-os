package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/config"
	"rig-chat/internal/ui"
)

// startManualSave opens the save prompt so the user can confirm or edit the session name.
func (m Model) startManualSave() (Model, tea.Cmd) {
	if m.incognito {
		return m, nil // no saving in incognito
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	m.savePrompt = ui.NewSavePrompt(name)
	m.mode = ModeSavePrompt
	m.textarea.Blur()
	(&m).recalcLayout()
	return m, nil
}

// saveAs persists the current session under the given name and updates LastSessionName.
func (m Model) saveAs(name string) (Model, tea.Cmd) {
	if name == "" || m.incognito {
		return m, nil
	}
	m.session.file.Messages = m.session.extractMessages()
	m.session.file.TotalTokens = m.session.totalTokens
	err := config.SaveSession(m.paths, name, m.session.file)
	if err != nil {
		m.lastError = fmt.Sprintf("save: %v", err)
	} else {
		m.settings.LastSessionName = name
		_ = config.SaveSettings(m.paths, m.settings)
		m.lastError = ""
	}
	return m, nil
}

// autoSave persists silently after each assistant reply when AutoSave is enabled.
func (m Model) autoSave() (Model, tea.Cmd) {
	if !m.settings.AutoSave || m.incognito {
		return m, nil
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	return m.saveAs(name)
}

// clearSession resets all messages and session state to start fresh.
func (m Model) clearSession() (Model, tea.Cmd) {
	m.session.clear(m.settings.Provider, m.settings.Model, m.settings.Thinking, m.settings.SystemPromptFile)
	if !m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	m.lastError = ""
	m.updateViewportContent()
	m.mode = ModeChat
	m.textarea.Focus()
	return m, nil
}

// toggleIncognito switches incognito mode on/off and resets the chat either way.
func (m Model) toggleIncognito() (Model, tea.Cmd) {
	m.incognito = !m.incognito
	m.session.clear(m.settings.Provider, m.settings.Model, m.settings.Thinking, m.settings.SystemPromptFile)
	if !m.incognito {
		// Leaving incognito: also reset last session name so auto-save doesn't
		// accidentally write to the previous session.
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	m.lastError = ""
	m.updateViewportContent()
	m.mode = ModeChat
	m.textarea.Focus()
	return m, nil
}

// startLoad opens the session picker, snapshots current state so Esc can restore it,
// and immediately previews the first (or last-used) session.
func (m Model) startLoad() (Model, tea.Cmd) {
	sessions := config.ListSessions(m.paths)
	if len(sessions) == 0 {
		return m, nil
	}

	// Snapshot current state so Esc can restore it
	m.sessionSnapshot = &sessionSnapshot{
		session:  m.session,
		settings: m.settings,
	}

	picker := ui.NewPickerList("Load Session", sessions)

	// Pre-select LastSessionName if it exists in the list
	if m.settings.LastSessionName != "" {
		for i, s := range sessions {
			if s == m.settings.LastSessionName {
				picker.Selected = i
				break
			}
		}
	}

	m.sessionPicker = picker
	m.mode = ModeSessionPicker
	(&m).recalcLayout()

	// Preview the initially selected session immediately
	m = m.previewSession(m.sessionPicker.SelectedItem())
	return m, nil
}

// previewSession loads a session's messages into view without persisting anything.
func (m Model) previewSession(name string) Model {
	if name == "" {
		return m
	}
	sf, err := config.LoadSession(m.paths, name)
	if err != nil {
		return m
	}
	m.session.setFrom(sf)
	m.updateViewportContent()
	return m
}

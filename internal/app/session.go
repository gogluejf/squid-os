package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/ui"
	"squid-os/internal/util"
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
// Pass silent=true to skip setting a notification (e.g. for background auto-saves).
func (m Model) saveAs(name string, silent bool) (Model, tea.Cmd) {
	if name == "" || m.incognito {
		return m, nil
	}
	m.session.file.TotalTokens = m.session.totalTokens()
	err := config.SaveSession(m.paths, name, m.session.file)
	if err != nil {
		if !silent {
			(&m).setNotification(ui.NotificationError, "couldn't save session")
		}
	} else {
		m.settings.LastSessionName = name
		_ = config.SaveSettings(m.paths, m.settings)
		if !silent {
			(&m).setNotification(ui.NotificationInfo, "session saved to "+config.SessionPath(m.paths, name))
		}
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
	return m.saveAs(name, true)
}

// clearSession resets all messages and session state to start fresh.
func (m Model) clearSession() (Model, tea.Cmd) {
	m.session.clear(m.settings, m.paths, m.workingDir)
	if !m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	if m.settings.AutoSave {
		(&m).setNotification(ui.NotificationInfo, "new session started, will auto-save")
	} else {
		(&m).setNotification(ui.NotificationInfo, "new session started  ·  ctrl+s to save")
	}
	m.updateViewportContent()
	return m, m.setChatMode()
}

// toggleIncognito switches incognito mode on/off and resets the chat either way.
func (m Model) toggleIncognito() (Model, tea.Cmd) {
	m.incognito = !m.incognito
	m.session.clear(m.settings, m.paths, m.workingDir)
	if !m.incognito {
		// Leaving incognito: also reset last session name so auto-save doesn't
		// accidentally write to the previous session.
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	// Disable logging in incognito, re-enable if debug is on
	log.SetEnabled(m.settings.DebugEnabled && !m.incognito)
	if m.incognito {
		(&m).setNotification(ui.NotificationInfo, "incognito is on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "incognito is off")
	}
	m.updateViewportContent()
	return m, m.setChatMode()
}

// startLoad opens the session picker, snapshots current state so Esc can restore it,
// and immediately previews the first (or last-used) session.
func (m Model) startLoad() (Model, tea.Cmd) {
	sessions := config.ListSessions(m.paths)
	if len(sessions) == 0 {
		return m, nil
	}

	// Snapshot current state so Esc can restore it
	snap := m.session
	m.sessionSnapshot = &snap

	// Build 2-column display strings: "name           modified"
	// Name column width = 30 (padded), date in friendly relative format.
	items := make([]string, len(sessions))
	for i, s := range sessions {
		dateLabel := util.FriendlyModDate(s.ModTime)
		items[i] = fmt.Sprintf("%-30s  %s", s.Name, dateLabel)
	}

	picker := ui.NewPickerList("Load Session", items)

	// Pre-select LastSessionName if it exists in the list
	if m.settings.LastSessionName != "" {
		for i, s := range sessions {
			if s.Name == m.settings.LastSessionName {
				picker.Selected = i
				break
			}
		}
	}

	m.sessionPicker = picker
	m.sessionPickerRaw = sessions // keep raw names for selection
	m.mode = ModeSessionPicker
	(&m).recalcLayout()

	// Preview the initially selected session immediately
	m = m.previewSession(m.sessionPickerSelectedRaw())
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
	m.session.setFrom(sf, false)
	// Restore working dir from the session
	if sf.Session.WorkingDir != "" {
		(&m).applyWorkingDir(sf.Session.WorkingDir)
	}
	m.updateViewportContent()
	return m
}

// sessionPickerSelectedRaw returns the raw session name for the currently
// selected picker item, using the parallel sessionPickerRaw slice.
func (m Model) sessionPickerSelectedRaw() string {
	filtered := m.sessionPicker.FilteredItems()
	if m.sessionPicker.Selected < 0 || m.sessionPicker.Selected >= len(filtered) {
		return ""
	}
	// The picker's filtered items correspond positionally to filtered raw items.
	rawFiltered := filterSessionList(m.sessionPickerRaw, m.sessionPicker.Filter)
	if m.sessionPicker.Selected >= len(rawFiltered) {
		return ""
	}
	return rawFiltered[m.sessionPicker.Selected].Name
}

// filterSessionList filters the raw session list by the same text filter used by the picker.
func filterSessionList(items []config.SessionInfo, filter string) []config.SessionInfo {
	if filter == "" {
		return items
	}
	f := filter
	var result []config.SessionInfo
	for _, item := range items {
		if matchesFilter(item.Name, f) || matchesFilter(util.FriendlyModDate(item.ModTime), f) {
			result = append(result, item)
		}
	}
	return result
}

// matchesFilter is a case-insensitive substring match used for session picker filtering.
func matchesFilter(s, f string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(f))
}

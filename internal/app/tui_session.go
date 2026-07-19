package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
	"squid-os/internal/util"
)

// openSaveSessionPrompt opens a prompt so the user can confirm or edit the session name.
func (m Model) openSaveSessionPrompt() (Model, tea.Cmd) {
	if m.incognito {
		return m, nil
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	prompt := &component.Prompt{
		Title: "Save Session",
		Label: "Name:",
		Value: name,
		OnConfirm: func(value string, ctx any) tea.Cmd {
			m := ctx.(*Model)
			nm, cmd := m.saveAs(value, false)
			*m = nm
			if cmd != nil {
				return tea.Batch(m.setChatMode(), cmd)
			}
			return m.setChatMode()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.setChatMode()
		},
	}
	(&m).setComponent(prompt)
	return m, nil
}

// clearSession resets all messages and session state to start fresh.
func (m Model) clearSession() (Model, tea.Cmd) {
	m.session = NewUISessionFromSettings(m.settings, m.paths, m.workingDir)
	if !m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	if m.settings.AutoSave {
		(&m).setNotification(ui.NotificationInfo, "new session started, will auto-save")
	} else {
		(&m).setNotification(ui.NotificationInfo, "new session started  ·  ctrl+s to save")
	}
	return m, m.setChatMode()
}

// toggleIncognito switches incognito mode on/off and resets the chat either way.
func (m Model) toggleIncognito() (Model, tea.Cmd) {
	m.incognito = !m.incognito
	m.session = NewUISessionFromSettings(m.settings, m.paths, m.workingDir)
	if !m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	log.SetEnabled(m.settings.DebugEnabled && !m.incognito)
	if m.incognito {
		(&m).setNotification(ui.NotificationInfo, "incognito is on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "incognito is off")
	}
	return m, m.setChatMode()
}

// openSessionPicker opens the session picker, snapshots current state so Esc can restore it,
// and immediately previews the first (or last-used) session.
func (m Model) openSessionPicker() (Model, tea.Cmd) {
	sessions := config.ListSessions(m.paths)
	if len(sessions) == 0 {
		return m, nil
	}

	m.sessionSnapshot = m.session

	items := make([]component.PickerItem, len(sessions))
	for i, s := range sessions {
		items[i] = component.PickerItem{
			Label: s.Name,
			Meta:  []string{util.FriendlyModDate(s.ModTime)},
			Value: s.Name,
		}
	}

	picker := component.Picker{
		Title:        "Load Session",
		Items:        items,
		DefaultValue: m.settings.LastSessionName,
		OnSelectionChange: func(idx int, item component.PickerItem, ctx any) {
			m := ctx.(*Model)
			name := item.Value
			if name == "" {
				return
			}
			sf, err := config.LoadSessionDoc(m.paths, name)
			if err != nil {
				return
			}
			m.session = NewUISessionFromDoc(sf)
			if sf.Session.WorkingDir != "" {
				m.applyWorkingDir(sf.Session.WorkingDir)
			}
			m.updateViewportContent()
		},
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			selected := item.Value
			if selected == "" {
				return m.setChatMode()
			}
			if !m.incognito {
				m.settings.LastSessionName = selected
				_ = config.SaveSettings(m.paths, m.settings)
			}
			sf, err := config.LoadSessionDoc(m.paths, selected)
			if err != nil {
				return m.setChatMode()
			}
			m.session = NewUISessionFromDoc(sf)
			if sf.Session.WorkingDir != "" {
				m.applyWorkingDir(sf.Session.WorkingDir)
			}
			m.sessionSnapshot = nil
			m.setNotification(ui.NotificationInfo, "session loaded from "+config.SessionPath(m.paths, selected))
			if msgIdx, ok := m.session.lastPendingToolMsgIdx(); ok {
				return tea.Batch(m.setChatMode(), func() tea.Msg { return pendingToolResumeMsg{msgIdx: msgIdx} })
			}
			return m.setChatMode()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			if m.sessionSnapshot != nil {
				m.session = m.sessionSnapshot
				m.sessionSnapshot = nil
				if m.session.Doc.Session.WorkingDir != "" {
					m.applyWorkingDir(m.session.Doc.Session.WorkingDir)
				}
			}
			return m.setChatMode()
		},
	}

	(&m).setComponent(&picker)
	return m, nil
}

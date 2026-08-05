package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
	"squid-os/internal/util"
)

func (m *Model) loadUISession(doc config.SessionDoc, name string) bool {
	resolved, err := runtimeconfig.Resolve(runtimeconfig.Inputs{Settings: m.settings, Paths: m.paths, ExistingSession: &doc, SessionName: name, Target: runtimeconfig.TargetInteractive})
	if err != nil {
		return false
	}
	runtimeconfig.ApplyToExistingSession(&doc, resolved.Config)
	m.session = NewUISessionFromDoc(doc, name, m.paths, resolved.Catalog)
	return true
}

// openSaveSessionPrompt opens a prompt so the user can confirm or edit the session name.
func (m Model) openSaveSessionPrompt() (Model, tea.Cmd) {
	if m.incognito {
		(&m).setNotification(ui.NotificationInfo, "incognito is on, session won't be saved")
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
	sessionConfig, err := runtimeconfig.Resolve(runtimeconfig.Inputs{
		Settings: m.settings,
		Paths:    m.paths,
		Target:   runtimeconfig.TargetInteractive,
		CLI:      runtimeconfig.Overrides{WorkingDir: m.session.Doc.Config.WorkingDir},
	})
	if err != nil {
		(&m).setNotification(ui.NotificationError, err.Error())
		return m, nil
	}
	m.session = NewUISession(sessionConfig.Config, m.paths, sessionConfig.Catalog)
	m.settings.LastSessionName = ""
	_ = config.SaveSettings(m.paths, m.settings)
	if m.settings.AutoSave {
		(&m).setNotification(ui.NotificationInfo, "new session started, will auto-save")
	} else {
		(&m).setNotification(ui.NotificationInfo, "new session started  ·  ctrl+s to save")
	}
	return m, m.setChatMode()
}

// toggleIncognito switches incognito mode on/off.
// On: keep current session alive, stop saving/history/debug.
// Off: reload session from disk if it was saved (discarding incognito changes).
func (m Model) toggleIncognito() (Model, tea.Cmd) {
	m.incognito = !m.incognito
	if m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
		(&m).setNotification(ui.NotificationInfo, "incognito is on")
	} else {
		// Resume — reload from disk if the session was previously saved
		if m.session.Info.Name != "" {
			if sf, err := config.LoadSessionDoc(m.paths, m.session.Info.Name); err == nil {
				m.loadUISession(sf, m.session.Info.Name)
				m.settings.LastSessionName = m.session.Info.Name
				_ = config.SaveSettings(m.paths, m.settings)
			}
		}
		(&m).setNotification(ui.NotificationInfo, "incognito is off")
	}
	log.SetEnabled(m.settings.DebugEnabled && !m.incognito)
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
			m.loadUISession(sf, name)
			m.updateViewportContent()
		},
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			selected := item.Value
			if selected == "" {
				return m.setChatMode()
			}
			m.settings.LastSessionName = selected
			_ = config.SaveSettings(m.paths, m.settings)
			sf, err := config.LoadSessionDoc(m.paths, selected)
			if err != nil {
				return m.setChatMode()
			}
			m.loadUISession(sf, selected)
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
			}
			return m.setChatMode()
		},
	}

	(&m).setComponent(&picker)
	return m, nil
}

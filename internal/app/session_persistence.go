package app

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// saveAs persists the current session under the given name and updates LastSessionName.
// Pass silent=true to skip setting a notification (e.g. for background auto-saves).
func (m Model) saveAs(name string, silent bool) (Model, tea.Cmd) {
	if name == "" || m.incognito {
		return m, nil
	}
	m.session.Doc.Config.Autosave.Name = name

	m.session.RefreshTokenTally()
	err := config.SaveSessionDoc(m.paths, name, m.session.Doc, m.session.Doc.TokenTally)
	if err != nil {
		if !silent {
			(&m).setNotification(ui.NotificationError, "couldn't save session")
		}
	} else {
		if m.settings.LastSessionName != name {
			m.settings.LastSessionName = name
			_ = config.SaveSettings(m.paths, m.settings)
		}
		m.session.Info.Name = name
		m.session.Info.ModTime = time.Now()
		if !silent {
			(&m).setNotification(ui.NotificationInfo, "session saved to "+config.SessionPath(m.paths, name))
		}
	}
	return m, nil
}

// autoSave persists silently after each assistant reply when session autosave is enabled.
// Only saves if there is at least one user message, OR the session file already
// exists on disk (to keep it in sync after destroys).
func (m Model) autoSave() (Model, tea.Cmd) {
	if !m.session.Doc.Config.Autosave.Enabled || m.incognito {
		return m, nil
	}
	name := m.session.Doc.Config.Autosave.Name
	if !m.session.HasUserMessage() {
		path := config.SessionPath(m.paths, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return m, nil
		}
	}
	return m.saveAs(name, true)
}

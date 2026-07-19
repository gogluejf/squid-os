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
	m.session.Doc.TotalTokens = m.session.TotalTokens()
	err := config.SaveSessionDoc(m.paths, name, m.session.Doc)
	if err != nil {
		if !silent {
			(&m).setNotification(ui.NotificationError, "couldn't save session")
		}
	} else {
		if m.settings.LastSessionName != name {
			m.settings.LastSessionName = name
			_ = config.SaveSettings(m.paths, m.settings)
		}
		if !silent {
			(&m).setNotification(ui.NotificationInfo, "session saved to "+config.SessionPath(m.paths, name))
		}
	}
	return m, nil
}

// autoSave persists silently after each assistant reply when AutoSave is enabled.
// Only saves if there is at least one user message, OR the session file already
// exists on disk (to keep it in sync after destroys).
func (m Model) autoSave() (Model, tea.Cmd) {
	if !m.settings.AutoSave || m.incognito {
		return m, nil
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	if !m.session.HasUserMessage() {
		path := config.SessionPath(m.paths, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return m, nil
		}
	}
	return m.saveAs(name, true)
}

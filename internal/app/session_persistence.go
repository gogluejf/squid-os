package app

import (
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// saveTo persists the current session to an explicit resolved destination.
// Saving to the current directory writes in place. A different destination
// forks only when the current session file already exists; otherwise it is the
// first save of the in-memory session.
func (m Model) saveTo(destinationDir string, silent bool) (Model, tea.Cmd) {
	if destinationDir == "" || m.incognito {
		return m, nil
	}

	name := filepath.Base(destinationDir)
	m.session.Doc.Config.Autosave.Name = name
	m.session.RefreshTokenTally()

	if destinationDir == m.session.SessionDir {
		if err := m.session.Save(); err != nil {
			if !silent {
				(&m).setNotification(ui.NotificationError, "couldn't save session: "+err.Error())
			}
			return m, nil
		}
	} else {
		sourceFile := config.SessionFilePath(m.session.SessionDir)
		_, sourceErr := os.Stat(sourceFile)
		switch {
		case sourceErr == nil:
			if _, err := config.ForkSessionTree(m.session.SessionDir, destinationDir); err != nil {
				if !silent {
					(&m).setNotification(ui.NotificationError, "couldn't fork session: "+err.Error())
				}
				return m, nil
			}
			forkedDoc, err := config.LoadSessionDoc(destinationDir)
			if err != nil {
				if !silent {
					(&m).setNotification(ui.NotificationError, "couldn't load forked session: "+err.Error())
				}
				return m, nil
			}
			m.session.Doc = forkedDoc
			m.session.SessionDir = destinationDir
			m.session.Doc.Config.Autosave.Name = name
			if err := m.session.Save(); err != nil {
				if !silent {
					(&m).setNotification(ui.NotificationError, "couldn't update forked session: "+err.Error())
				}
				return m, nil
			}
		case os.IsNotExist(sourceErr):
			m.session.SessionDir = destinationDir
			if err := m.session.Save(); err != nil {
				if !silent {
					(&m).setNotification(ui.NotificationError, "couldn't save session: "+err.Error())
				}
				return m, nil
			}
		default:
			if !silent {
				(&m).setNotification(ui.NotificationError, "couldn't inspect session: "+sourceErr.Error())
			}
			return m, nil
		}
	}

	if m.settings.LastSessionName != name {
		m.settings.LastSessionName = name
		_ = config.SaveSettings(m.paths, m.settings)
	}
	m.session.Info.Name = name
	m.session.Info.ModTime = time.Now()
	if !silent {
		(&m).setNotification(ui.NotificationInfo, "session saved to "+config.SessionFilePath(m.session.SessionDir))
	}
	return m, nil
}

// autoSave persists the current session in place after each assistant reply.
// It never resolves a root name, so the same path works for root and child sessions.
func (m Model) autoSave() (Model, tea.Cmd) {
	if !m.session.Doc.Config.Autosave.Enabled || m.incognito {
		return m, nil
	}
	if !m.session.HasUserMessage() {
		if _, err := os.Stat(config.SessionFilePath(m.session.SessionDir)); os.IsNotExist(err) {
			return m, nil
		}
	}
	if err := m.session.Save(); err != nil {
		return m, nil
	}
	return m, nil
}

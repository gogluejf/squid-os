package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// toggleThinking toggles thinking mode on/off and persists the setting.
func (m Model) toggleThinking() (Model, tea.Cmd) {
	m.settings.Thinking.Enabled = !m.settings.Thinking.Enabled
	_ = config.SaveSettings(m.paths, m.settings)
	(&m).session.updateConfigMsg(m.settings.Provider, m.settings.Model, m.settings.Thinking)
	(&m).session.invalidateRenderAll()
	if m.settings.Thinking.Enabled {
		(&m).setNotification(ui.NotificationInfo, "thinking on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "thinking off")
	}
	return m, m.setChatMode()
}

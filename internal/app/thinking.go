package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// toggleThinking changes the global default and queues the session transition.
func (m Model) toggleThinking() (Model, tea.Cmd) {
	inference := m.session.EffectiveInference()
	inference.Thinking.Enabled = !inference.Thinking.Enabled
	m.session.SetPendingInference(inference)
	m.settings.Thinking = inference.Thinking
	_ = config.SaveSettings(m.paths, m.settings)
	m.session.invalidateRenderAll()
	if inference.Thinking.Enabled {
		(&m).setNotification(ui.NotificationInfo, "thinking on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "thinking off")
	}
	return m, m.setChatMode()
}

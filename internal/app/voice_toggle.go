package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/ui"
)

// toggleVoice persists the global default, like thinking, without resetting
// chat mode or touching the active inference/tool stream. Enabling applies to
// the next message; disabling immediately silences narration.
func (m Model) toggleVoice() (Model, tea.Cmd) {
	next := m.settings
	next.Voice.Enabled = !next.Voice.Enabled
	log.LogVoice("toggle.attempt", "from=%t to=%t", m.settings.Voice.Enabled, next.Voice.Enabled)
	if err := config.SaveSettings(m.paths, next); err != nil {
		log.LogVoice("toggle.save_failed", "retained=%t requested=%t", m.settings.Voice.Enabled, next.Voice.Enabled)
		m.setNotification(ui.NotificationError, "could not save voice settings: "+err.Error())
		return m, nil
	}
	m.settings = next
	log.LogVoice("toggle.applied", "enabled=%t saved=true", m.settings.Voice.Enabled)
	if !m.settings.Voice.Enabled {
		// Kill in-flight playback now: CancelGroup mutes the group so late
		// chunks are dropped and the running player process is SIGKILLed.
		m.narrator.cancelActive()
	}
	if m.settings.Voice.Enabled {
		(&m).setNotification(ui.NotificationInfo, "voice on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "voice off")
	}
	return m, nil
}

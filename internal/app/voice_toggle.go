package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/ui"
)

// toggleVoice cycles the voice mode: off → step → turn → off.
// Persists like thinking. Disabling (→ off) kills in-flight playback.
func (m Model) toggleVoice() (Model, tea.Cmd) {
	next := m.settings
	var nextMode config.NarrationMode
	switch next.Voice.Mode {
	case config.NarrationOff:
		nextMode = config.NarrationStep
	case config.NarrationStep:
		nextMode = config.NarrationTurn
	default:
		nextMode = config.NarrationOff
	}
	from := next.Voice.Mode
	next.Voice.Mode = nextMode
	log.LogVoice("toggle.attempt", "from=%q to=%q", from, nextMode)
	if err := config.SaveSettings(m.paths, next); err != nil {
		log.LogVoice("toggle.save_failed", "retained=%q requested=%q", from, nextMode)
		m.setNotification(ui.NotificationError, "could not save voice settings: "+err.Error())
		return m, nil
	}
	m.settings = next
	log.LogVoice("toggle.applied", "mode=%q saved=true", nextMode)
	if nextMode == config.NarrationOff {
		m.narrator.cancelActive()
	}
	m.setNotification(ui.NotificationInfo, "voice: "+string(nextMode))
	return m, nil
}

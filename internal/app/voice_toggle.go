package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/ui"
)

// toggleVoice flips the runtime voice flag. The settings file is the startup
// source of truth only: this toggle never writes settings, so the choice
// lives for this session and the next launch re-reads the file. It is a pure
// state flip (like /thinking): it does NOT manage turn/mode state — calling
// setChatMode here would corrupt an in-flight stream if pressed mid-turn.
// Toggling off mid-speech kills the current playback group immediately.
func (m Model) toggleVoice() (Model, tea.Cmd) {
	m.voiceOn = !m.voiceOn
	if !m.voiceOn {
		// Kill in-flight playback now: CancelGroup mutes the group so late
		// chunks are dropped and the running player process is SIGKILLed.
		m.narrator.cancelActive()
	}
	if m.voiceOn {
		(&m).setNotification(ui.NotificationInfo, "voice on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "voice off")
	}
	return m, nil
}

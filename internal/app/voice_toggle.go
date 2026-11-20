package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

// cycleVoice cycles the voice mode: off → step → turn → off.
// Persists like thinking. Disabling (→ off) kills in-flight playback.
func (m Model) cycleVoice() (Model, tea.Cmd) {
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

// openVoiceModePicker shows a picker to explicitly select a voice mode.
func (m Model) openVoiceModePicker() (Model, tea.Cmd) {
	items := []component.PickerItem{
		{Label: "off", Meta: []string{"no speech"}, Value: string(config.NarrationOff)},
		{Label: "step", Meta: []string{"speak every assistant message"}, Value: string(config.NarrationStep)},
		{Label: "turn", Meta: []string{"speak only the final message"}, Value: string(config.NarrationTurn)},
	}
	picker := component.Picker{
		Title:        "Voice Mode",
		Items:        items,
		DefaultValue: string(m.settings.Voice.Mode),
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			mode := config.NarrationMode(item.Value)
			next := m.settings
			next.Voice.Mode = mode
			log.LogVoice("picker.selected", "mode=%q", mode)
			if err := config.SaveSettings(m.paths, next); err != nil {
				m.setNotification(ui.NotificationError, "could not save voice settings: "+err.Error())
				return m.setChatMode()
			}
			m.settings = next
			log.LogVoice("picker.applied", "mode=%q saved=true", mode)
			if mode == config.NarrationOff {
				m.narrator.cancelActive()
			}
			m.setNotification(ui.NotificationInfo, "voice: "+string(mode))
			return m.dismissCommandPicker()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.dismissCommandPicker()
		},
	}
	m.setComponent(&picker)
	return m, nil
}

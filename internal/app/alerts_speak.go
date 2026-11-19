package app

import (
	"squid-os/internal/voice"
)

// speakAlertEvent formats one alert event in the user's last language and
// speaks it as its own group. All call sites run on the UI goroutine, so
// reading m.voiceOn and m.lastUserLang here is safe. No-op when voice is off
// or unwired — alerts must never change behavior when voice is disabled.
func (m *Model) speakAlertEvent(ev voice.Event, detail string) {
	if m.narrator == nil || m.narrator.coord == nil {
		return
	}
	m.narrator.speakAlert(m.voiceOn, voice.AlertText(ev, m.lastUserLang, detail))
}

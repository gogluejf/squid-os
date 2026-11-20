package app

import (
	"squid-os/internal/voice"
)

// speakAlertEvent formats one alert event in the user's last language and
// speaks it as its own group. All call sites run on the UI goroutine, so
// reading m.settings.Voice.Enabled here is safe. Read the synchronized language
// store directly: detection may finish before its UI message is delivered.
// No-op when voice is off or unwired.
func (m *Model) speakAlertEvent(ev voice.Event, detail string) {
	if m.narrator == nil || m.narrator.coord == nil {
		return
	}
	lang := m.lastUserLang
	if m.langStore != nil {
		if detected := m.langStore.Get(); detected != "" {
			lang = detected
		}
	}
	if lang != "fr" {
		lang = "en"
	}
	text := voice.AlertText(ev, lang, detail)
	m.narrator.speakAlert(m.settings.Voice.Enabled, text, lang)
}

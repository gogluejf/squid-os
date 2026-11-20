package app

import (
	"squid-os/internal/log"
	"squid-os/internal/voice"
)

// speakAlertEvent formats one alert event in the user's last language and
// speaks it as its own group. All call sites run on the UI goroutine, so
// reading m.settings.Voice.VoiceEnabled() here is safe. Read the synchronized language
// store directly: detection may finish before its UI message is delivered.
// No-op when voice is off or unwired.
func (m *Model) speakAlertEvent(ev voice.Event, detail string) {
	if m.narrator == nil || m.narrator.coord == nil {
		log.LogVoice("alert.skipped", "event=%q reason=unwired", ev)
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
	log.LogVoice("alert.trigger", "event=%q submission=%d enabled=%t language=%q text=%q", ev, m.langSeq, m.settings.Voice.VoiceEnabled(), lang, text)
	m.narrator.speakAlert(m.settings.Voice.VoiceEnabled(), text, lang)
}

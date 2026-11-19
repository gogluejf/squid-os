package voice

import (
	"strings"
)

// Event identifies a system event announced aloud as its own speech group.
type Event string

const (
	Error         Event = "error"
	Authorization Event = "authorization"
	ContextBust   Event = "context-bust"
	SkillLoaded   Event = "skill-loaded"
	SubAgent      Event = "sub-agent-called"
	ThinkingStart Event = "thinking-started"
)

// alerts is the single source of bilingual alert lead-ins, keyed by event
// then language ("en"/"fr"). Templates may contain {name} placeholders.
var alerts = map[Event]map[string]string{
	Error:         {fallbackLang: "An error occurred.", "fr": "Une erreur est survenue."},
	Authorization: {fallbackLang: "A command requires your authorization.", "fr": "Une commande requiert votre autorisation."},
	ContextBust:   {fallbackLang: "Warning, no context left.", "fr": "Attention, plus de contexte disponible."},
	SkillLoaded:   {fallbackLang: "I loaded the skill {name}.", "fr": "Jai charge la competence {name}."},
	SubAgent:      {fallbackLang: "I sent a sub-agent working on it.", "fr": "Jai lance un sous-agent qui va effectuer le travail."},
	ThinkingStart: {fallbackLang: "Let me think about it now.", "fr": "Laisse-moi y penser."},
}

// FormatAlert renders the lead-in for ev in lang (unknown → EN). detail is
// interpolated into {name} placeholders when present; empty detail leaves
// the template untouched. An Event missing from the table degrades to ""
// rather than panicking — a forgotten table entry must never crash speech.
func FormatAlert(ev Event, lang, detail string) string {
	byLang, ok := alerts[ev]
	if !ok {
		return ""
	}
	tmpl, ok := byLang[lang]
	if !ok {
		tmpl = byLang[fallbackLang]
	}
	return strings.ReplaceAll(tmpl, "{name}", detail)
}

// AlertText returns the full spoken text for one alert event: the lead-in
// in lastUserLang, with the raw English detail appended after it for Error
// events (same group — the server auto-detects the mixed languages).
func AlertText(ev Event, lastUserLang, detail string) string {
	text := FormatAlert(ev, lastUserLang, detail)
	if ev == Error && detail != "" {
		text += " " + strings.TrimSpace(detail)
	}
	return text
}

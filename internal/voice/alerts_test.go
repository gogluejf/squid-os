package voice

import (
	"testing"
)

// TestFormatAlertExactStrings: every event renders the exact EN/FR strings
// from the alerts table.
func TestFormatAlertExactStrings(t *testing.T) {
	cases := []struct {
		ev   Event
		lang string
		want string
	}{
		{Error, "en", "An error occurred."},
		{Error, "fr", "Une erreur est survenue."},
		{Authorization, "en", "A command requires your authorization."},
		{Authorization, "fr", "Une commande requiert votre autorisation."},
		{ContextBust, "en", "Warning, no context left."},
		{ContextBust, "fr", "Attention, plus de contexte disponible."},
		{SkillLoaded, "en", "I loaded the skill ."},
		{SkillLoaded, "fr", "Jai charge la competence ."},
		{SubAgent, "en", "I sent a sub-agent working on it."},
		{SubAgent, "fr", "Jai lance un sous-agent qui va effectuer le travail."},
		{ThinkingStart, "en", "Let me think about it now."},
		{ThinkingStart, "fr", "Laisse-moi y penser."},
	}
	for _, c := range cases {
		if got := FormatAlert(c.ev, c.lang, ""); got != c.want {
			t.Errorf("FormatAlert(%s, %q) = %q, want %q", c.ev, c.lang, got, c.want)
		}
	}
}

// TestFormatAlertUnknownLanguageFallsBackToEN: any unrecognized language
// (including empty) renders the EN template.
func TestFormatAlertUnknownLanguageFallsBackToEN(t *testing.T) {
	for _, lang := range []string{"", "de", "ja"} {
		if got := FormatAlert(ThinkingStart, lang, ""); got != "Let me think about it now." {
			t.Errorf("FormatAlert(ThinkingStart, %q) = %q, want EN fallback", lang, got)
		}
	}
}

// TestFormatAlertInterpolatesName: SkillLoaded interpolates the skill name
// into the {name} placeholder in both languages.
func TestFormatAlertInterpolatesName(t *testing.T) {
	if got := FormatAlert(SkillLoaded, "en", "plan-runner"); got != "I loaded the skill plan-runner." {
		t.Errorf("EN = %q", got)
	}
	if got := FormatAlert(SkillLoaded, "fr", "plan-runner"); got != "Jai charge la competence plan-runner." {
		t.Errorf("FR = %q", got)
	}
}

// TestAlertTextErrorAppendsRawDetail: an Error alert appends the raw English
// detail after the lead-in, in the user's language.
func TestAlertTextErrorAppendsRawDetail(t *testing.T) {
	if got := AlertText(Error, "en", "boom: connection refused"); got != "An error occurred. boom: connection refused" {
		t.Errorf("EN = %q", got)
	}
	if got := AlertText(Error, "fr", "boom: connection refused"); got != "Une erreur est survenue. boom: connection refused" {
		t.Errorf("FR = %q", got)
	}
	// Empty detail → just the lead-in.
	if got := AlertText(Error, "en", ""); got != "An error occurred." {
		t.Errorf("empty detail = %q", got)
	}
}

// TestAlertTextNonErrorIgnoresDetail: non-Error events never append detail.
func TestAlertTextNonErrorIgnoresDetail(t *testing.T) {
	if got := AlertText(ContextBust, "fr", "should not appear"); got != "Attention, plus de contexte disponible." {
		t.Errorf("ContextBust = %q", got)
	}
	if got := AlertText(SubAgent, "en", "should not appear"); got != "I sent a sub-agent working on it." {
		t.Errorf("SubAgent = %q", got)
	}
}

// TestFormatAlertUnknownEventDegradesSilently: an Event added to the type
// but forgotten in the table must render "" (no speech), never panic.
func TestFormatAlertUnknownEventDegradesSilently(t *testing.T) {
	var unknown Event // zero value is not in the table
	if got := FormatAlert(unknown, "en", "x"); got != "" {
		t.Errorf("unknown event = %q, want empty string", got)
	}
}

// TestFormatAlertEmptyNameLeavesPlaceholderBlank: documenting the degenerate
// rendering when SkillLoaded has no name — the placeholder is replaced with
// nothing, leaving "skill ." rather than keeping the literal "{name}".
func TestFormatAlertEmptyNameLeavesPlaceholderBlank(t *testing.T) {
	if got := FormatAlert(SkillLoaded, "en", ""); got != "I loaded the skill ." {
		t.Errorf("empty name = %q", got)
	}
}

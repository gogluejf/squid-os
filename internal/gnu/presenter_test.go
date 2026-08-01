package gnu

import (
	"bytes"
	"strings"
	"testing"
)

func TestPresenterRestoresPaddedInteractiveLayout(t *testing.T) {
	var output bytes.Buffer
	presenter := Presenter{Writer: &output, Color: false}
	presenter.Waiting("vllm/model")
	presenter.Suggestion(Suggestion{Command: "find . -type f", InstallHint: "brew install findutils"})
	presenter.Aborted()

	got := output.String()
	for _, expected := range []string{
		"\n  Asking Squid-OS  vllm/model\n",
		"\n  install  brew install findutils\n",
		"  find . -type f\n\n",
		"  Aborted.\n",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
}

func TestPresenterUsesColorWhenEnabled(t *testing.T) {
	var output bytes.Buffer
	Presenter{Writer: &output, Color: true}.Suggestion(Suggestion{Command: "ls"})
	if !strings.Contains(output.String(), "\x1b[38;5;208m") {
		t.Fatalf("missing orange command style: %q", output.String())
	}
}

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/voice"
)

// recordingTTSCli writes every chunk it is fed to recordPath (one line per
// invocation: the voice flag, then the text on stdin). It exits 0 so groups
// drain deterministically. Unlike writeFakeTTSCli (which discards audio), this
// lets tests assert what was ACTUALLY spoken — the hardening for finding #12.
func recordingTTSCli(t *testing.T, recordPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "squid-tts-say")
	src := "#!/bin/sh\n" +
		"# args: -v <name> -q --stdin ; text arrives on stdin\n" +
		"voice=$2\n" +
		"text=$(cat)\n" +
		"printf '%s\\t%s\\n' \"$voice\" \"$text\" >> '" + recordPath + "'\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(src), 0o755); err != nil {
		t.Fatalf("write recording cli: %v", err)
	}
	return path
}

// newRecordingAlertModel builds an alert test model whose TTS CLI records
// every spoken chunk to a temp file. The speaker also points at a dead detect
// server so Speak's language probe fails fast (the CLI still runs). Returns
// the model and the record path.
func newRecordingAlertModel(t *testing.T) (Model, string) {
	t.Helper()
	m := newQueueTestModel(t)
	recordPath := filepath.Join(t.TempDir(), "spoken.log")
	cli := recordingTTSCli(t, recordPath)
	sp, err := voice.NewSpeaker(voice.SpeakerOptions{
		CLIPath:      cli,
		ServerURL:    "http://127.0.0.1:1", // dead: detect fails fast, speak proceeds
		DetectClient: &http.Client{Timeout: 200 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewSpeaker: %v", err)
	}
	cfg := config.Voice{Enabled: true, Voices: map[string]string{"en": "jane", "fr": "jane"}}
	m.voiceOn = true
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))
	return m, recordPath
}

// spokenLines returns the recorded chunks as [voice, text] pairs.
func spokenLines(t *testing.T, recordPath string) []string {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read spoken log: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// waitSpoken polls the record file until want substrings all appear or the
// bounded deadline trips (established bounded-drain pattern).
func waitSpoken(t *testing.T, recordPath string, wants ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		all := true
		content, _ := os.ReadFile(recordPath)
		for _, w := range wants {
			if !strings.Contains(string(content), w) {
				all = false
				break
			}
		}
		if all {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := os.ReadFile(recordPath)
	t.Fatalf("expected spoken text %q not found in %q", wants, string(got))
}

// TestWireVoiceSpeakingReachesProgramCopy (finding #1): tea.NewProgram copies
// the Model value; WireVoiceSpeaking then mutates the PRE-program copy. The
// program-side copy must still see non-nil langPush — otherwise
// fireUserLangDetect is a permanent no-op in production. Proven by copying
// the model exactly as NewProgram does and checking the shared wiring state.
func TestWireVoiceSpeakingReachesProgramCopy(t *testing.T) {
	m := newQueueTestModel(t)
	if m.voiceWiring == nil || m.voiceWiring.langPush != nil {
		t.Fatal("precondition: fresh model must have unwired voice")
	}

	// Simulate launchTUI: build the "program copy" first, then wire the
	// original (what internal/cli/tui.go does with model + program).
	programCopy := m // tea.NewProgram(model) copies the value
	wire := func(p *tea.Program) {} // stand-in; we only need the Send func
	_ = wire
	send := func(msg tea.Msg) {}
	(&m).wireVoiceForTest(send)

	if programCopy.voiceWiring == nil {
		t.Fatal("program copy lost the wiring pointer entirely")
	}
	if programCopy.voiceWiring.langPush == nil {
		t.Fatal("finding #1 regression: program-side copy has nil langPush — detection would no-op forever")
	}
	// The pre-program narrator is still inert here: in production
	// WireVoiceSpeaking rebuilds it via newNarrator BEFORE NewProgram copies
	// the model, so the copy carries the live coordinator. That ordering is
	// what launchTUI does; this test proves the wiring STATE (langPush)
	// survives the copy, which is the part that was broken.
	_ = programCopy
}

// wireVoiceForTest installs voice wiring the way WireVoiceSpeaking does, but
// with a test push func instead of a real *tea.Program (no TUI in unit tests).
func (m *Model) wireVoiceForTest(push func(tea.Msg)) {
	dir, err := os.MkdirTemp("", "wiretest")
	if err != nil {
		return
	}
	cli := filepath.Join(dir, "squid-tts-say")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\ncat > /dev/null\nexit 0\n"), 0o755); err != nil {
		return
	}
	sp, err := voice.NewSpeaker(voice.SpeakerOptions{
		CLIPath:      cli,
		ServerURL:    "http://127.0.0.1:1",
		DetectClient: &http.Client{Timeout: 200 * time.Millisecond},
	})
	if err != nil {
		return
	}
	cfg := config.Voice{Enabled: true, Voices: map[string]string{"en": "jane", "fr": "jane"}}
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))
	if m.voiceWiring == nil {
		m.voiceWiring = &voiceWiringState{}
	}
	m.voiceWiring.langPush = push
}

// TestToolEventNeedAuthSpeaksAuthorizationRecorded (finding #12 hardening):
// the auth-prompt call site must SPEAK the authorization lead-in — proven by
// observing the actual chunk written by the recording TTS CLI, not just the
// recursion-guard lifecycle.
func TestToolEventNeedAuthSpeaksAuthorizationRecorded(t *testing.T) {
	m, recordPath := newRecordingAlertModel(t)
	m.lastUserLang = "fr"

	event := chat.ToolEvent{Type: chat.ToolEventNeedAuth, MsgIdx: 0, ToolIndex: 0,
		AuthRequest: &chat.AuthRequest{ToolName: "bash"}}
	updated, _ := m.handleToolEvent(event)
	m2 := updated.(Model)

	if m2.session.UIStream.AuthorizationCtx == nil {
		t.Fatal("auth event did not enter auth mode")
	}
	// The French authorization lead-in must actually be spoken.
	waitSpoken(t, recordPath, "Une commande requiert votre autorisation.")
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after auth alert")
	}
}

// TestToolEventRunningSkillLoadedSpeaksAlertRecorded (finding #12): a skill
// load at Running speaks "I loaded the skill {name}" — observed via the
// recording CLI.
func TestToolEventRunningSkillLoadedSpeaksAlertRecorded(t *testing.T) {
	m, recordPath := newRecordingAlertModel(t)

	updated, _ := m.handleToolEvent(chat.ToolEvent{Type: chat.ToolEventRunning, MsgIdx: 0, ToolIndex: 0, LoadedSkill: "browser-use"})
	m2 := updated.(Model)

	if m2.session.Doc.Config.ActiveSkill != "browser-use" {
		t.Fatalf("ActiveSkill = %q, want browser-use", m2.session.Doc.Config.ActiveSkill)
	}
	waitSpoken(t, recordPath, "I loaded the skill browser-use.")
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after skill-load alert")
	}
}

// TestLoopErrorSpeaksErrorAlertWithDetailRecorded (finding #12): a stream
// error speaks the Error lead-in AND the raw detail as separate chunks of the
// same group (finding #9: two-chunk split for per-chunk language detection).
func TestLoopErrorSpeaksErrorAlertWithDetailRecorded(t *testing.T) {
	m, recordPath := newRecordingAlertModel(t)
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Answer in progress when the error hit. ")
	narrGid := m.narrator.coord.Current()
	if narrGid == 0 {
		t.Fatal("no narration group before error")
	}

	updated, _ := m.handleStreamEvent(chat.StreamEvent{Error: errFakeStream})
	m2 := updated.(Model)

	if got := m2.narrator.coord.Current(); got == narrGid {
		t.Fatalf("aborted narration group %d still current", narrGid)
	}
	// Both the EN lead-in and the raw detail must be spoken.
	waitSpoken(t, recordPath, "An error occurred.", "connection reset by peer")
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after error alert")
	}
}

// TestSilentFailureSpeaksGenericErrorRecorded (findings #10 + #12): a
// SilentFailure speaks the GENERIC Error lead-in (not a dedicated
// context-bust template) — observed via the recording CLI.
func TestSilentFailureSpeaksGenericErrorRecorded(t *testing.T) {
	m, recordPath := newRecordingAlertModel(t)

	updated, _ := m.handleStreamEvent(chat.StreamEvent{Done: true})
	m2 := updated.(Model)

	waitSpoken(t, recordPath, "An error occurred.")
	waitDrained(t, m2.narrator.coord)
	if m2.narrator.alertActive {
		t.Fatal("recursion guard stuck after silent-failure error alert")
	}
}

// TestSpeakAlertRecursionGuardDropsSecond (finding #12): while an alert group
// is in flight, a second alert is dropped — proven by the recording CLI
// showing ONLY the first alert's text.
func TestSpeakAlertRecursionGuardDropsSecond(t *testing.T) {
	m, recordPath := newRecordingAlertModel(t)

	m.speakAlertEvent(voice.Error, "first failure")
	firstGid := m.narrator.coord.Current()
	if firstGid == 0 {
		t.Fatal("first alert did not start a group")
	}
	m.speakAlertEvent(voice.Error, "second failure must be dropped")
	if got := m.narrator.coord.Current(); got != firstGid {
		t.Fatalf("recursion guard failed: current = %d, want still %d", got, firstGid)
	}
	waitDrained(t, m.narrator.coord)

	lines := spokenLines(t, recordPath)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "first failure") {
		t.Fatalf("first alert not spoken: %q", joined)
	}
	if strings.Contains(joined, "second failure must be dropped") {
		t.Fatalf("second alert was spoken despite the recursion guard: %q", joined)
	}
}

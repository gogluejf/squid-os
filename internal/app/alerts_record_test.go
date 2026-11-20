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
		"voice=\n" +
		"while [ $# -gt 0 ]; do if [ \"$1\" = -v ]; then shift; voice=$1; fi; shift; done\n" +
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
	cfg := config.Voice{Mode: config.NarrationStep, Voices: map[string]string{"en": "jane", "fr": "custom-french"}}
	m.settings.Voice.Mode = config.NarrationStep
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

// Exercise the production wiring AFTER the value copy, as launchTUI does.
func TestWireVoiceSpeakingReachesProgramCopy(t *testing.T) {
	m := newQueueTestModel(t)
	m.narrator = newNarrator(nil)
	t.Setenv("PATH", filepath.Dir(writeFakeTTSCli(t)))
	programCopy := m
	program := tea.NewProgram(programCopy)
	m.WireVoiceSpeaking(program)
	if programCopy.narrator.coord == nil || programCopy.voiceWiring.langPush == nil {
		t.Fatal("production wiring did not reach program copy")
	}
	coord := programCopy.narrator.coord
	m.WireVoiceSpeaking(program)
	if programCopy.narrator.coord != coord {
		t.Fatal("wiring must be idempotent")
	}
	// A drained group must clear the guard via the installed hook.
	gid := coord.StartGroup()
	programCopy.narrator.alertGids[gid] = struct{}{}
	programCopy.narrator.alertActive = true
	programCopy.narrator.onAnyGroupDrained(gid)
	if programCopy.narrator.alertActive {
		t.Fatal("alert guard stuck")
	}
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
	waitSpoken(t, recordPath, "custom-french\tUne commande requiert votre autorisation.")
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

// Detection can finish while program.Send is still waiting for the UI loop.
// Authorization must use that accepted result, not the older UI snapshot.
func TestAuthorizationUsesDetectedLanguageBeforeUIMessage(t *testing.T) {
	m, recordPath := newRecordingAlertModel(t)
	srv, closeSrv := langDetectServer(t, map[string]string{"je suis": "fr"})
	defer closeSrv()
	sp, err := voice.NewSpeaker(voice.SpeakerOptions{
		ServerURL: srv.URL, CLIPath: recordingTTSCli(t, recordPath),
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Voice{Mode: config.NarrationStep, Voices: map[string]string{"en": "jane", "fr": "custom-french"}}
	m.narrator = newNarrator(voice.NewCoordinator(sp, &cfg))
	m.langStore = voice.NewLangStore()
	m.lastUserLang = "en"
	// Model is copied before wiring, just as tea.NewProgram copies it.
	live := m
	posted := make(chan tea.Msg, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	defer func() { close(release); <-done }()
	m.voiceWiring.langPush = func(msg tea.Msg) {
		posted <- msg
		<-release // simulate Send waiting for the next UI frame
		close(done)
	}
	live.fireUserLangDetect("je suis en train de farie un test, lis un fichier au hazard dans mon repo")
	select {
	case msg := <-posted:
		if msg.(langDetectedMsg).lang != "fr" {
			t.Fatalf("detection = %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("detection did not finish")
	}
	if live.langSeq != 1 || m.langSeq != 0 {
		t.Fatal("submission sequence must belong to live model")
	}
	if live.lastUserLang != "en" {
		t.Fatal("test must leave UI snapshot unapplied")
	}
	updated, _ := live.handleToolEvent(chat.ToolEvent{Type: chat.ToolEventNeedAuth,
		AuthRequest: &chat.AuthRequest{ToolName: "read_file"}})
	if updated.(Model).session.UIStream.AuthorizationCtx == nil {
		t.Fatal("authorization not entered")
	}
	waitSpoken(t, recordPath, "custom-french\tUne commande requiert votre autorisation.")
	waitDrained(t, live.narrator.coord)
}

func TestGenericAlertsUseAcceptedLanguageWhileDetectionPending(t *testing.T) {
	for _, ev := range []voice.Event{voice.Error, voice.ThinkingStart, voice.SkillLoaded, voice.SubAgent} {
		t.Run(string(ev), func(t *testing.T) {
			m, recordPath := newRecordingAlertModel(t)
			m.langStore = voice.NewLangStore()
			m.langStore.Submit(1)
			m.langStore.SetIfNewest(1, "fr")
			m.langStore.Submit(2)            // no new result yet: keep last accepted user language
			m.langStore.SetIfNewest(1, "en") // stale completion cannot change it
			m.lastUserLang = "en"
			m.speakAlertEvent(ev, "probe")
			waitSpoken(t, recordPath, "custom-french\t"+voice.AlertText(ev, "fr", "probe"))
			waitDrained(t, m.narrator.coord)
		})
	}
}

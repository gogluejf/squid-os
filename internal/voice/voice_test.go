package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"squid-os/internal/config"
)

// --- helpers ---------------------------------------------------------------

func testCfg() *config.Voice {
	s := config.Settings{}
	s.NormalizeVoice()
	return &s.Voice
}

// fakeTTS returns a detect server that answers the given language map and a
// Coordinator whose speaker points at it. The returned closer also stops
// any real processes spawned through the coordinator.
func fakeTTS(t *testing.T, langs map[string]string) (*Coordinator, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/detect", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		lang := "en"
		for marker, l := range langs {
			if strings.Contains(body.Text, marker) {
				lang = l
				break
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"language": lang})
	})
	srv := httptest.NewServer(mux)

	sp, err := NewSpeaker(SpeakerOptions{ServerURL: srv.URL})
	if err != nil {
		srv.Close()
		t.Fatalf("NewSpeaker: %v", err)
	}
	c := NewCoordinator(sp, testCfg())
	return c, srv.Close
}

// waitIdle polls until the coordinator has no active group.
func waitIdle(t *testing.T, c *Coordinator) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.Current() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("coordinator still has an active group after 10s")
}

// --- unit tests ------------------------------------------------------------

func TestResolveVoiceFallbackChain(t *testing.T) {
	cfg := testCfg()

	// Known language resolves directly.
	if name, ok := resolveVoice(cfg, "fr", ""); !ok || name != "jane" {
		t.Fatalf("resolveVoice(fr) = %q,%v; want jane,true", name, ok)
	}
	// Unknown language falls back to previous session lang.
	if name, ok := resolveVoice(cfg, "de", "fr"); !ok || name != "jane" {
		t.Fatalf("resolveVoice(de, prev=fr) = %q,%v; want jane,true", name, ok)
	}
	// Unknown language with no previous falls back to en.
	if name, ok := resolveVoice(cfg, "xx", ""); !ok || name != "jane" {
		t.Fatalf("resolveVoice(xx) = %q,%v; want jane,true", name, ok)
	}
	// Empty detection result falls straight through the chain.
	if name, ok := resolveVoice(cfg, "", ""); !ok || name != "jane" {
		t.Fatalf("resolveVoice(\"\") = %q,%v; want jane,true", name, ok)
	}
	// No entry anywhere → error, not panic.
	bare := &config.Voice{Voices: map[string]string{}}
	if _, ok := resolveVoice(bare, "zz", ""); ok {
		t.Fatal("resolveVoice on empty cfg should fail")
	}
}

func TestDetectLanguageBoundedTimeoutNoPanic(t *testing.T) {
	// Server that never responds: bounded timeout, clean error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	sp, err := NewSpeaker(SpeakerOptions{
		ServerURL:    srv.URL,
		DetectClient: &http.Client{Timeout: 200 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = sp.DetectLanguage(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("detect took %v, want bounded well under 3s", elapsed)
	}
}

func TestDetectLanguageServerDownReturnsError(t *testing.T) {
	sp, err := NewSpeaker(SpeakerOptions{
		ServerURL:    "http://127.0.0.1:1", // nothing listens here
		DetectClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.DetectLanguage(context.Background(), "hello"); err == nil {
		t.Fatal("expected error when server is down")
	}
}

func TestSpeakServerDownNeverPanics(t *testing.T) {
	sp, err := NewSpeaker(SpeakerOptions{
		ServerURL:    "http://127.0.0.1:1",
		DetectClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Speak panicked: %v", r)
		}
	}()
	// Detection fails but must fall back to the previous-lang chain and
	// attempt playback; with a bogus CLI the spawn fails cleanly.
	sp.cli = "/nonexistent/squid-tts-say-test"
	if _, err := sp.Speak(context.Background(), "hello", testCfg(), "en"); err == nil {
		t.Fatal("expected spawn error")
	}
}

func TestCLIResolvedFromPATH(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	cli := filepath.Join(dir, "squid-tts-say")
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)

	sp, err := NewSpeaker(SpeakerOptions{})
	if err != nil {
		t.Fatalf("NewSpeaker: %v", err)
	}
	if sp.cli != cli {
		t.Fatalf("cli = %q, want %q (resolved from PATH)", sp.cli, cli)
	}
}

func TestCLIMissingFromPATHIsError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := NewSpeaker(SpeakerOptions{}); err == nil {
		t.Fatal("expected error when squid-tts-say is not on PATH")
	}
}

// --- coordinator arbitration (fake CLI, deterministic) ---------------------

// recordingCLI writes one line per invocation (argv + stdin) to a file so
// tests can assert what was actually spawned and in which order.
func recordingCLI(t *testing.T, logPath string, hang bool) string {
	t.Helper()
	var script string
	if hang {
		script = fmt.Sprintf(`#!/bin/sh
echo "$@" >> %s
cat >> %s
sleep 30
`, logPath, logPath)
	} else {
		script = fmt.Sprintf(`#!/bin/sh
echo "$@" >> %s
cat >> %s
`, logPath, logPath)
	}
	cli := filepath.Join(t.TempDir(), "squid-tts-say")
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return cli
}

func newFakeCoord(t *testing.T, cli, logPath string) (*Coordinator, func()) {
	t.Helper()
	sp, err := NewSpeaker(SpeakerOptions{
		ServerURL:    "http://127.0.0.1:1", // detect always fails → fallback chain
		DetectClient: &http.Client{Timeout: 50 * time.Millisecond},
		CLIPath:      cli,
		SpeakTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := NewCoordinator(sp, testCfg())
	cleanup := func() {
		c.mu.Lock()
		ids := make([]int, 0, len(c.groups))
		for id := range c.groups {
			ids = append(ids, id)
		}
		c.mu.Unlock()
		for _, id := range ids {
			c.CancelGroup(id)
		}
	}
	t.Cleanup(cleanup)
	return c, cleanup
}

// waitLog polls until the fake CLI's log contains needle (or timeout).
func waitLog(t *testing.T, path, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(readLog(t, path), needle) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log never contained %q: %q", needle, readLog(t, path))
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func TestSameGroupBackToBackOrdering(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, false)
	c, _ := newFakeCoord(t, cli, logPath)

	g := c.StartGroup()
	for i, chunk := range []string{"one.", "two.", "three."} {
		if err := c.Emit(context.Background(), g, chunk); err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}
	c.EndGroup(g)
	waitIdle(t, c)

	out := readLog(t, logPath)
	first, second, third := strings.Index(out, "one."), strings.Index(out, "two."), strings.Index(out, "three.")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("log missing chunks: %q", out)
	}
	if !(first < second && second < third) {
		t.Fatalf("chunks out of order: %q", out)
	}
	// All three went through the SAME single process? No — one process per
	// chunk, but crucially no bump happened: exactly 3 spawns for one group.
	if n := strings.Count(out, "--stdin"); n != 3 {
		t.Fatalf("got %d spawns, want 3 (no bump within a group)", n)
	}
}

func TestCrossGroupEmitBumpsPlayingGroup(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, true) // chunks hang so groups overlap
	c, _ := newFakeCoord(t, cli, logPath)

	preempts := make(chan int, 4)
	c.SetOnPreempt(func(old int) {
		preempts <- old
		c.CancelGroup(old) // default policy behaviour
	})

	g1 := c.StartGroup()
	if err := c.Emit(context.Background(), g1, "alpha."); err != nil {
		t.Fatal(err)
	}
	// Give the first chunk time to start.
	waitLog(t, logPath, "alpha.")
	if c.Current() != g1 {
		t.Fatalf("current = %d, want %d", c.Current(), g1)
	}

	g2 := c.StartGroup()
	if err := c.Emit(context.Background(), g2, "beta."); err != nil {
		t.Fatal(err)
	}

	select {
	case old := <-preempts:
		if old != g1 {
			t.Fatalf("onPreempt(%d), want %d", old, g1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onPreempt not called on cross-group Emit")
	}

	// Bumped group is muted; its late chunks are dropped.
	if err := c.Emit(context.Background(), g1, "gamma."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "beta.")
	c.CancelGroup(g2) // EndGroup does not stop a deliberately hanging player.
	waitIdle(t, c)

	out := readLog(t, logPath)
	if !strings.Contains(out, "beta.") {
		t.Fatalf("new group did not speak: %q", out)
	}
	if strings.Contains(out, "gamma.") {
		t.Fatalf("muted group's late chunk leaked: %q", out)
	}
	if c.Current() != 0 {
		t.Fatalf("current = %d after drain, want 0", c.Current())
	}
}

func TestDefaultPolicyCancelsOldGroup(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, true)
	c, _ := newFakeCoord(t, cli, logPath)

	g1 := c.StartGroup()
	if err := c.Emit(context.Background(), g1, "alpha."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "alpha.")

	g2 := c.StartGroup()
	if err := c.Emit(context.Background(), g2, "beta."); err != nil {
		t.Fatal(err)
	}
	// Default onPreempt = CancelGroup: g1 is muted now, so verify via mute
	// semantics (the kill may interrupt the shell before or after its echo):
	if err := c.Emit(context.Background(), g1, "late."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "beta.")
	c.CancelGroup(g2) // hanging fixture must be explicitly cancelled
	waitIdle(t, c)
	if strings.Contains(readLog(t, logPath), "late.") {
		t.Fatal("chunk for default-bumped group was spoken")
	}
}

func TestLateChunkForMutedGroupDropped(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, false)
	c, _ := newFakeCoord(t, cli, logPath)

	g := c.StartGroup()
	if err := c.Emit(context.Background(), g, "first."); err != nil {
		t.Fatal(err)
	}
	c.CancelGroup(g)
	// Late chunk arrives AFTER cancellation — must be dropped silently.
	if err := c.Emit(context.Background(), g, "second."); err != nil {
		t.Fatal(err)
	}
	// Even if the producer retries after EndGroup-style teardown.
	c.EndGroup(g)
	if err := c.Emit(context.Background(), g, "third."); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, c)

	out := readLog(t, logPath)
	if strings.Contains(out, "second.") || strings.Contains(out, "third.") {
		t.Fatalf("muted group leaked chunks: %q", out)
	}
	// Not sticky: the next group speaks normally.
	g2 := c.StartGroup()
	if err := c.Emit(context.Background(), g2, "next message."); err != nil {
		t.Fatal(err)
	}
	c.EndGroup(g2)
	waitIdle(t, c)
	if !strings.Contains(readLog(t, logPath), "next message.") {
		t.Fatal("group after cancellation did not speak (mute is sticky — bug)")
	}
}

func TestEndGroupThenEmitDropped(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, false)
	c, _ := newFakeCoord(t, cli, logPath)

	g := c.StartGroup()
	c.EndGroup(g) // producer signalled end immediately
	if err := c.Emit(context.Background(), g, "too late."); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, c)
	if strings.Contains(readLog(t, logPath), "too late.") {
		t.Fatal("chunk after EndGroup was spoken")
	}
}

func TestUnknownGroupIDIgnored(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, false)
	c, _ := newFakeCoord(t, cli, logPath)

	if err := c.Emit(context.Background(), 999, "ghost."); err != nil {
		t.Fatal(err)
	}
	c.CancelGroup(999) // must not panic
	c.EndGroup(999)    // must not panic
	if strings.Contains(readLog(t, logPath), "ghost.") {
		t.Fatal("unknown group spoke")
	}
}

// --- race coverage ----------------------------------------------------------

func TestRaceConcurrentEmitAndCancel(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, true)
	c, _ := newFakeCoord(t, cli, logPath)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g := c.StartGroup()
			for j := 0; j < 5; j++ {
				_ = c.Emit(context.Background(), g, fmt.Sprintf("g%d-c%d.", i, j))
			}
			c.EndGroup(g)
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 8; i++ {
		c.CancelGroup(i + 1)
	}
	wg.Wait()
	waitIdle(t, c)
}

// --- deterministic subprocess / process-group tests -------------------------

func pidAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	// Zombies have exited; an external parent may not have reaped them yet.
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+2:])
	return len(fields) > 0 && fields[0] != "Z"
}

// findDescendants returns PIDs whose PPid matches any of the given pids.
func findDescendants(t *testing.T, pids ...int) []int {
	t.Helper()
	want := make(map[int]bool, len(pids))
	for _, p := range pids {
		want[p] = true
	}
	var out []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skipf("cannot scan /proc: %v", err)
	}
	for _, e := range entries {
		c := e.Name()[0]
		if c < '0' || c > '9' {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// stat: pid (comm) state ppid ...
		s := string(stat)
		rparen := strings.LastIndex(s, ")")
		fields := strings.Fields(s[rparen+2:])
		if len(fields) < 2 {
			continue
		}
		var ppid int
		fmt.Sscanf(fields[1], "%d", &ppid)
		if want[ppid] {
			var pid int
			fmt.Sscanf(e.Name(), "%d", &pid)
			out = append(out, pid)
		}
	}
	return out
}

// TestCancelKillsWholeProcessGroup verifies real OS group cancellation using
// a deterministic shell + child, without a live TTS server or audio hardware.
func TestCancelKillsWholeProcessGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc")
	}
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, true)
	c, _ := newFakeCoord(t, cli, logPath)
	p, err := c.speaker.Speak(context.Background(), "process group test.", testCfg(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Kill)
	waitLog(t, logPath, "process group test.")
	cliPID := p.Pid()

	// Wait until the aplay child exists inside the group.
	var aplayPID int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		kids := findDescendants(t, cliPID)
		if len(kids) > 0 {
			aplayPID = kids[0]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if aplayPID == 0 {
		p.Kill()
		t.Fatal("fixture child never appeared within 3s — cannot verify group kill")
	}
	if !pidAlive(aplayPID) {
		p.Kill()
		t.Fatal("aplay died before cancel — timing flake, aborting")
	}

	// Kill the whole group mid-playback.
	p.Kill()

	// Both the CLI and aplay must be gone quickly.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(cliPID) && !pidAlive(aplayPID) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(cliPID) {
		t.Errorf("CLI pid %d still alive after group kill", cliPID)
	}
	if pidAlive(aplayPID) {
		t.Errorf("aplay pid %d orphaned after group kill", aplayPID)
	}

	// Wait must return exactly once with an error (killed), no hang.
	done := make(chan error, 1)
	go func() {
		<-p.Done()
		done <- p.Err()
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected non-nil error from killed process")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not report exit after kill")
	}
}

// TestDoubleKillSafe ensures killing twice (and after normal exit bookkeeping)
// never panics.
func TestDoubleKillSafe(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, false)
	c, _ := newFakeCoord(t, cli, logPath)

	g := c.StartGroup()
	if err := c.Emit(context.Background(), g, "short."); err != nil {
		t.Fatal(err)
	}
	c.CancelGroup(g)
	c.CancelGroup(g) // double cancel: no-op, no panic
	waitIdle(t, c)
}

// TestStaleWatcherCannotClearNewerGroup injects a delayed completion after
// replacement, rather than relying on goroutine scheduling or shell timing.
func TestStaleWatcherCannotClearNewerGroup(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	cli := recordingCLI(t, logPath, true)
	c, _ := newFakeCoord(t, cli, logPath)
	g1 := c.StartGroup()
	if err := c.Emit(context.Background(), g1, "stale."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "stale.")
	c.mu.Lock()
	old := c.groups[g1].player
	c.mu.Unlock()
	g2 := c.StartGroup()
	if err := c.Emit(context.Background(), g2, "fresh."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "fresh.")
	c.playerFinished(g1, old)
	if got := c.Current(); got != g2 {
		t.Fatalf("current = %d, want %d", got, g2)
	}
	c.CancelGroup(g2)
	waitIdle(t, c)
}

// Returning from Speak must not cancel its asynchronous child.
func TestSpeakLifetimeOutlivesReturn(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	c, _ := newFakeCoord(t, recordingCLI(t, logPath, true), logPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := c.speaker.Speak(ctx, "still alive.", testCfg(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Kill)
	waitLog(t, logPath, "still alive.")
	select {
	case <-p.Done():
		t.Fatalf("premature exit: %v", p.Err())
	default:
	}
	cancel()
	select {
	case <-p.Done():
		if p.Err() == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not terminate child")
	}
}

// --- speaking hook (task 1.6 indicator) ------------------------------------

// recordingHook captures speaking-state transitions in order, safe for use
// from the coordinator's background goroutines.
type recordingHook struct {
	mu     sync.Mutex
	events []bool
}

func (h *recordingHook) fn() func(on bool) {
	return func(on bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.events = append(h.events, on)
	}
}

func (h *recordingHook) last() (bool, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) == 0 {
		return false, false
	}
	return h.events[len(h.events)-1], true
}

func (h *recordingHook) count(v bool) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, e := range h.events {
		if e == v {
			n++
		}
	}
	return n
}

// TestSpeakingHookFirstChunkStartsClearsOnExit: the hook fires on=true when
// the first chunk starts playing and on=false when the player exits — the
// two edges that drive the status-row indicator.
func TestSpeakingHookFirstChunkStartsClearsOnExit(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	c, _ := newFakeCoord(t, recordingCLI(t, logPath, false), logPath)
	hook := &recordingHook{}
	c.SetSpeakingHook(hook.fn())

	g := c.StartGroup()
	if err := c.Emit(context.Background(), g, "first sentence."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "first sentence.")
	c.EndGroup(g)
	waitIdle(t, c)

	hook.wait(t, false)
	last, ok := hook.last()
	if !ok || last {
		t.Fatalf("hook events after drain = %v, want final state false", hook.events)
	}
	if hook.count(true) != 1 {
		t.Fatalf("on-transitions = %d, want exactly 1 (first chunk start)", hook.count(true))
	}
	if hook.count(false) != 1 {
		t.Fatalf("off-transitions = %d, want exactly 1 (player exit)", hook.count(false))
	}
}

// TestSpeakingHookBackToBackNoFlicker: a second chunk of the SAME group must
// not re-fire on=true (the indicator stays lit while audio continues).
func TestSpeakingHookBackToBackNoFlicker(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	c, _ := newFakeCoord(t, recordingCLI(t, logPath, false), logPath)
	hook := &recordingHook{}
	c.SetSpeakingHook(hook.fn())

	g := c.StartGroup()
	if err := c.Emit(context.Background(), g, "alpha."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "alpha.")
	if err := c.Emit(context.Background(), g, "beta."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "beta.")
	c.EndGroup(g)
	waitIdle(t, c)

	if n := hook.count(true); n != 1 {
		t.Fatalf("on-transitions across back-to-back chunks = %d, want 1", n)
	}
	hook.wait(t, false)
	if n := hook.count(false); n != 1 {
		t.Fatalf("off-transitions = %d, want 1", n)
	}
}

// TestSpeakingHookCancelKillsAndClears: cancellation kills the playing chunk
// and the kill path clears the indicator (on=false) — never stuck on.
func TestSpeakingHookCancelKillsAndClears(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	c, _ := newFakeCoord(t, recordingCLI(t, logPath, true), logPath) // hanging CLI
	hook := &recordingHook{}
	c.SetSpeakingHook(hook.fn())

	g := c.StartGroup()
	if err := c.Emit(context.Background(), g, "long utterance."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "long utterance.")
	hook.wait(t, true)
	on, ok := hook.last()
	if !ok || !on {
		t.Fatalf("indicator not on during playback: events=%v", hook.events)
	}

	c.CancelGroup(g) // kill path: SIGKILL the process group
	waitIdle(t, c)

	hook.wait(t, false)
	last, ok := hook.last()
	if !ok || last {
		t.Fatalf("indicator stuck on after cancel: events=%v", hook.events)
	}
}

// TestSpeakingHookStaleCallbackCannotClearNewerGroup: a delayed completion
// callback from a superseded group must NOT clear the newer group's
// indicator. The guard is the coordinator's per-group player identity check
// (a stale watcher finds g.player already replaced) plus the current-group
// check in finishChunkLocked; only the NEWER group's own exit may turn the
// indicator off.
func TestSpeakingHookStaleCallbackCannotClearNewerGroup(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	c, _ := newFakeCoord(t, recordingCLI(t, logPath, true), logPath) // hanging CLI
	hook := &recordingHook{}
	c.SetSpeakingHook(hook.fn())

	g1 := c.StartGroup()
	if err := c.Emit(context.Background(), g1, "stale group."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "stale group.")
	c.mu.Lock()
	old := c.groups[g1].player
	c.mu.Unlock()

	// A newer group bumps g1 (default policy cancels it) and takes over.
	g2 := c.StartGroup()
	if err := c.Emit(context.Background(), g2, "fresh group."); err != nil {
		t.Fatal(err)
	}
	waitLog(t, logPath, "fresh group.")
	if got := c.Current(); got != g2 {
		t.Fatalf("current = %d, want %d", got, g2)
	}

	// The stale watcher finally fires for g1's old player.
	c.playerFinished(g1, old)

	// The newer group's indicator must still be ON: the stale callback
	// neither cleared Current nor fired an off-transition for it.
	if got := c.Current(); got != g2 {
		t.Fatalf("stale callback cleared the newer group: current=%d, want %d", got, g2)
	}
	hook.wait(t, true)
	on, _ := hook.last()
	if !on {
		t.Fatalf("stale callback cleared the newer group's indicator: events=%v", hook.events)
	}

	// Only g2's own exit may turn the indicator off.
	c.CancelGroup(g2)
	waitIdle(t, c)
	hook.wait(t, false)
	last, _ := hook.last()
	if last {
		t.Fatalf("indicator stuck on after g2 killed: events=%v", hook.events)
	}
}

func (h *recordingHook) wait(t *testing.T, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := h.last(); ok && v == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("speaking transition %v not delivered", want)
}

func TestAsyncPlaybackCancelAndClose(t *testing.T) {
	for _, closeAll := range []bool{false, true} {
		t.Run(fmt.Sprint(closeAll), func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "log")
			c, _ := newFakeCoord(t, recordingCLI(t, logPath, true), logPath)
			defer c.Close()
			g := c.StartGroup()
			c.Emit(context.Background(), g, "first")
			c.Emit(context.Background(), g, "queued")
			c.EndGroup(g)
			waitLog(t, logPath, "first")
			if closeAll {
				c.Close()
			} else {
				c.CancelGroup(g)
			}
			c.Emit(context.Background(), g, "late")
			deadline := time.Now().Add(time.Second)
			for {
				c.mu.Lock()
				running := c.worker
				c.mu.Unlock()
				if !running {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("worker failed to retire")
				}
				time.Sleep(time.Millisecond)
			}
			if out := readLog(t, logPath); strings.Contains(out, "queued") || strings.Contains(out, "late") {
				t.Fatal(out)
			}
		})
	}
}

func TestNotificationsOrderedWithoutBlockingProducer(t *testing.T) {
	c := NewCoordinator(nil, testCfg())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var events []bool
	c.SetSpeakingHook(func(on bool) {
		if len(events) == 0 {
			close(entered)
			<-release
		}
		events = append(events, on)
		if len(events) == 3 {
			close(done)
		}
	})
	c.mu.Lock()
	c.setSpeakingLocked(true)
	c.mu.Unlock()
	<-entered
	// First hook is parked; subsequent transitions must queue, not park callers
	// or overtake it. All mutations use the same lock as real lifecycle edges.
	c.mu.Lock()
	c.setSpeakingLocked(false)
	c.setSpeakingLocked(true)
	c.mu.Unlock()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notification worker blocked")
	}
	if len(events) != 3 || !events[0] || events[1] || !events[2] {
		t.Fatalf("out of order: %v", events)
	}
}

func TestNarrationDelegatesDetectionAndAlertUsesConfiguredVoice(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		requests = append(requests, body.Text)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"language": "fr"})
	}))
	defer srv.Close()
	logPath := filepath.Join(t.TempDir(), "log")
	sp, err := NewSpeaker(SpeakerOptions{ServerURL: srv.URL, CLIPath: recordingCLI(t, logPath, false)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Voice{Voices: map[string]string{"en": "english-choice", "fr": "french-choice"}}
	c := NewCoordinator(sp, cfg)
	defer c.Close()
	g := c.StartGroup()
	c.Emit(context.Background(), g, "Assistant bonjour.")
	c.EndGroup(g)
	waitIdle(t, c)
	out := readLog(t, logPath)
	if !strings.Contains(out, "raw -q --stdin -l auto -s "+srv.URL) || strings.Contains(out, "-v") {
		t.Fatalf("narration argv: %s", out)
	}
	store := NewLangStore()
	store.Submit(1)
	DetectLang(sp, "USER bonjour", 1, store)
	if store.Get() != "fr" {
		t.Fatal("user language not detected")
	}
	g = c.StartGroup()
	c.EmitAlert(context.Background(), g, FormatAlert(Authorization, store.Get(), ""), store.Get())
	c.EndGroup(g)
	waitIdle(t, c)
	out = readLog(t, logPath)
	if !strings.Contains(out, "-v french-choice") || !strings.Contains(out, "Une commande requiert votre autorisation.") {
		t.Fatal(out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || requests[0] != "USER bonjour" {
		t.Fatalf("detect requests: %v", requests)
	}
}

func TestNarrationKeepsCommonConfiguredVoice(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log")
	c, _ := newFakeCoord(t, recordingCLI(t, logPath, false), logPath)
	g := c.StartGroup()
	c.Emit(context.Background(), g, "Narration.")
	c.EndGroup(g)
	waitIdle(t, c)
	if !strings.Contains(readLog(t, logPath), "-v jane") {
		t.Fatal("common configured voice lost")
	}
}

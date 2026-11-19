package cli

import (
	"strings"
	"testing"

	"squid-os/internal/localinput"
)

func TestVoiceStopEndToEndViaCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	srv, fs := newTestServer(t, "voice-test", &fakeStore{})
	defer srv.Close()

	fs.mu.Lock()
	fs.voiceActive = true
	fs.mu.Unlock()

	out := &strings.Builder{}
	err := voiceStop(&voiceOptions{}, CommandIO{Out: out})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	res := out.String()
	if !strings.Contains(res, `"status": "cancelled"`) {
		t.Fatalf("reply = %s", res)
	}

	// Second stop: nothing playing, still exit 0.
	out.Reset()
	err = voiceStop(&voiceOptions{}, CommandIO{Out: out})
	if err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "nothing_playing"`) {
		t.Fatalf("reply = %s", out.String())
	}
}

func TestVoiceStopNoRunningTUI(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	out := &strings.Builder{}
	err := voiceStop(&voiceOptions{}, CommandIO{Out: out})
	if err == nil || !strings.Contains(err.Error(), "no running TUI") {
		t.Fatalf("err = %v", err)
	}
}

// TestVoiceStopReceiverNotReadyExitsNonZero verifies a wedged UI loop
// (server rejects with receiver_unavailable) is NOT treated as a successful
// nothing_playing: the CLI must return an error (non-zero exit).
func TestVoiceStopReceiverNotReadyExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	srv, fs := newTestServer(t, "voice-fail", &fakeStore{})
	defer srv.Close()

	// Simulate a wedged UI loop: the app-side callback reports not-ready.
	fs.mu.Lock()
	fs.voiceStatus = localinput.VoiceStopNotReady
	fs.mu.Unlock()

	out := &strings.Builder{}
	err := voiceStop(&voiceOptions{}, CommandIO{Out: out})
	if err == nil {
		t.Fatal("expected non-zero exit for receiver_not_ready, got nil error")
	}
	if !strings.Contains(out.String(), `"status": "rejected"`) {
		t.Fatalf("reply = %s, want rejected status", out.String())
	}
	if !strings.Contains(out.String(), `"code": "receiver_unavailable"`) {
		t.Fatalf("reply = %s, want receiver_unavailable code", out.String())
	}
}

func TestVoiceScaffoldNotImplemented(t *testing.T) {
	out := &strings.Builder{}
	err := voiceScaffold(&voiceOptions{}, CommandIO{Out: out})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("err = %v", err)
	}
}

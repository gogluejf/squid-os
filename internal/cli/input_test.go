package cli

import (
	"strings"
	"testing"
)

func TestDiscoverInstanceNoRunningTUI(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := discoverInstance(""); err == nil || !strings.Contains(err.Error(), "no running TUI") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverInstanceAmbiguous(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	a, _ := newTestServer(t, "inst-a", &fakeStore{})
	b, _ := newTestServer(t, "inst-b", &fakeStore{})
	defer a.Close()
	defer b.Close()
	_, err := discoverInstance("")
	if err == nil || !strings.Contains(err.Error(), "ambiguous_target") {
		t.Fatalf("err = %v", err)
	}
	// Explicit selection resolves.
	p, err := discoverInstance("inst-a")
	if err != nil || p == "" {
		t.Fatalf("explicit: %v %q", err, p)
	}
}

func TestInputSendEndToEndViaCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	srv, fc := newTestServer(t, "cli-test", &fakeStore{})
	defer srv.Close()

	in := strings.NewReader("hello via cli\n")
	out := &strings.Builder{}
	o := &inputOptions{When: "step", Source: "test"}
	err := inputSend(o, CommandIO{In: in, Out: out})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	res := out.String()
	if !strings.Contains(res, `"status": "accepted"`) {
		t.Fatalf("reply = %s", res)
	}
	if got := len(fc.store); got != 1 {
		t.Fatalf("coordinator store = %d entries, want 1", got)
	}
}

func TestInputSendEmptyStdinRejected(t *testing.T) {
	in := strings.NewReader("\n")
	out := &strings.Builder{}
	err := inputSend(&inputOptions{}, CommandIO{In: in, Out: out})
	if err == nil || !strings.Contains(err.Error(), "empty input") {
		t.Fatalf("err = %v", err)
	}
}

func TestMintUUIDFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		u := mintUUID()
		if len(u) != 36 || strings.Count(u, "-") != 4 {
			t.Fatalf("bad uuid %q", u)
		}
	}
	// Uniqueness.
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		u := mintUUID()
		if seen[u] {
			t.Fatalf("duplicate uuid %q", u)
		}
		seen[u] = true
	}
}

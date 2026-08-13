package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func testRoot(t *testing.T, gotTUI **TUIOptions, gotRun **RunOptions, gotGNU **GNUOptions) *cobra.Command {
	t.Helper()
	tuiExec := func(o *TUIOptions) error {
		if gotTUI != nil {
			copy := *o
			*gotTUI = &copy
		}
		return nil
	}
	runExec := func(o *RunOptions) error {
		if gotRun != nil {
			copy := *o
			*gotRun = &copy
		}
		return nil
	}
	gnuExec := func(o *GNUOptions) error {
		if gotGNU != nil {
			copy := *o
			*gotGNU = &copy
		}
		return nil
	}
	return buildRoot(tuiCmd{execute: tuiExec}, runCmd{execute: runExec}, gnuCmd{execute: gnuExec})
}

func TestCommandSpecificAuthorizationModes(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		wantErr bool
	}{
		{[]string{"tui", "--auth-mode", "ask-on-write"}, false},
		{[]string{"tui", "--auth-mode", "end-on-write"}, true},
		{[]string{"run", "--auth-mode", "end-on-all", "--prompt", "x"}, false},
		{[]string{"run", "--auth-mode", "ask-for-all", "--prompt", "x"}, true},
	} {
		cmd := testRoot(t, nil, nil, nil)
		cmd.SetArgs(tt.args)
		err := cmd.Execute()
		if (err != nil) != tt.wantErr {
			t.Fatalf("args %v: %v", tt.args, err)
		}
	}
}

func TestRunAgentConflict(t *testing.T) {
	cmd := testRoot(t, nil, nil, nil)
	cmd.SetArgs([]string{"run", "one", "--agent", "two", "--prompt", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestPromptAndStdinCompose(t *testing.T) {
	var got *RunOptions
	cmd := testRoot(t, nil, &got, nil)
	cmd.SetIn(strings.NewReader("body"))
	cmd.SetArgs([]string{"run", "--prompt", "prefix"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "prefix\n\nbody" {
		t.Fatalf("%q", got.Prompt)
	}
}

func TestSessionAllowsSaveName(t *testing.T) {
	var got *RunOptions
	cmd := testRoot(t, nil, &got, nil)
	cmd.SetIn(bytes.NewBufferString("prompt"))
	cmd.SetArgs([]string{"run", "--session", "old", "--save-name", "new"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.Autosave == nil || !*got.Autosave || got.AutosaveName != "new" {
		t.Fatalf("%+v", got)
	}
}

func TestRunRejectsIncompleteChildLineageFlags(t *testing.T) {
	cmd := testRoot(t, nil, nil, nil)
	cmd.SetArgs([]string{
		"run", "--prompt", "x",
		"--session-id", "child",
		"--root-session-id", "root",
		"--session-depth", "1",
		"--parent-session-dir", "/tmp/parent",
	})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--parent-session-id") {
		t.Fatalf("expected missing parent session ID error, got %v", err)
	}
}

func TestTUIOverrides(t *testing.T) {
	var got *TUIOptions
	cmd := testRoot(t, &got, nil, nil)
	cmd.SetArgs([]string{"tui", "--save", "--thinking", "--max-agent-depth", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.Autosave == nil || !*got.Autosave || got.Thinking == nil || !*got.Thinking || !got.Limits.MaxAgentDepthSet {
		t.Fatalf("%+v", got)
	}
}

func TestSessionRejectsBootstrapFlags(t *testing.T) {
	cmd := testRoot(t, nil, nil, nil)
	cmd.SetIn(bytes.NewBufferString("x"))
	cmd.SetArgs([]string{"run", "--session", "s", "--system", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestArgumentValidation(t *testing.T) {
	cmd := testRoot(t, nil, nil, nil)
	cmd.SetArgs([]string{"run", "one", "two", "--prompt", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected too many arguments error")
	}
}

func TestRootRunsTUI(t *testing.T) {
	var got *TUIOptions
	cmd := testRoot(t, &got, nil, nil)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("root did not run TUI")
	}
}

func TestModelWorkerHiddenAtRoot(t *testing.T) {
	cmd := testRoot(t, nil, nil, nil)
	worker, _, err := cmd.Find([]string{"model-cache-refresh"})
	if err != nil || worker == nil || !worker.Hidden {
		t.Fatalf("worker: %v, err: %v", worker, err)
	}
	models, _, _ := cmd.Find([]string{"models"})
	for _, child := range models.Commands() {
		if child.Name() == "model-cache-refresh" {
			t.Fatal("worker nested under models")
		}
	}
}

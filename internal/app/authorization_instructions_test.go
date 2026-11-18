package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goai_provider "github.com/zendev-sh/goai/provider"

	"github.com/charmbracelet/bubbles/textarea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/ui"
)

// Synthetic regression fixture inspired by the historical duplicate user
// messages found in a real session (adjacent identical stored instructions).
// No private session text is copied here.
const tuiAuthInstructionText = "Synthetic instruction: run the tests before committing."

func toolEventModel(s *chat.Session) *Model {
	u := &UISession{Session: s, expand: ui.NewExpandTracker(false)}
	m := &Model{session: u, width: 80, height: 24}
	m.textarea = textarea.New()
	m.textarea.SetWidth(m.width)
	return m
}

// TestHandleToolEventDoneDoesNotDuplicateWorkerAppend is the worker-to-TUI
// integration guard for the authorization double-insertion bug: the worker
// already appended the captured instruction to the session, and the TUI must
// only invalidate its render — never append the same text again.
func TestHandleToolEventDoneDoesNotDuplicateWorkerAppend(t *testing.T) {
	for _, tc := range []struct {
		name     string
		approved bool
	}{
		{"approved with instruction", true},
		{"rejected with instruction", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &chat.Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
			s.Doc.Config.Tools = []string{"bash"}
			s.Doc.Messages = []config.Message{{
				ID:   "msg_1",
				Role: config.RoleAssistant,
				ToolCalls: []config.ToolCallEntry{
					authToolCallEntry("tool_1"),
					authToolCallEntry("tool_2"),
				},
			}}
			m := toolEventModel(s)
			before := len(s.Doc.Messages)

			ch := chat.StartToolExec(context.Background(), s, chat.ToolExecOptions{
				MsgIdx:   0,
				Decision: &chat.AuthDecision{Approved: tc.approved, Instructions: tuiAuthInstructionText},
			})
			var done chat.ToolEvent
			for ev := range ch {
				if ev.Type == chat.ToolEventDone {
					done = ev
				}
			}
			if done.AppendedUserMsgIdx != before {
				t.Fatalf("worker event must carry the appended index: got %d, want %d", done.AppendedUserMsgIdx, before)
			}

			// TUI side effect: handleToolEvent must not grow the history.
			m.handleToolEvent(done)

			users := userMessages(s.Doc.Messages[before:])
			if len(users) != 1 || users[0].Text != tuiAuthInstructionText {
				t.Fatalf("history must hold exactly one instruction message after the TUI handled Done, got %+v", users)
			}
			if got := countProjectedUserText(s.BuildContext().Messages, tuiAuthInstructionText); got != 1 {
				t.Fatalf("provider context must project the instruction once, got %d", got)
			}
		})
	}
}

// TestHandleToolEventPlainYesNoAppendsNothing proves a plain approval without
// instructions leaves the history untouched by the TUI handler.
func TestHandleToolEventPlainYesNoAppendsNothing(t *testing.T) {
	s := &chat.Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"bash"}
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			authToolCallEntry("tool_1"),
			authToolCallEntry("tool_2"),
		},
	}}
	m := toolEventModel(s)
	before := len(s.Doc.Messages)

	ch := chat.StartToolExec(context.Background(), s, chat.ToolExecOptions{
		MsgIdx:   0,
		Decision: &chat.AuthDecision{Approved: true, Instructions: ""},
	})
	var done chat.ToolEvent
	for ev := range ch {
		if ev.Type == chat.ToolEventDone {
			done = ev
		}
	}
	if done.AppendedUserMsgID != "" || done.AppendedUserMsgIdx != -1 {
		t.Fatalf("plain yes must not report an appended message: id=%q idx=%d", done.AppendedUserMsgID, done.AppendedUserMsgIdx)
	}

	m.handleToolEvent(done)

	if got := len(userMessages(s.Doc.Messages[before:])); got != 0 {
		t.Fatalf("plain yes must add no user message, got %d", got)
	}
}

// TestAuthorizationInstructionCheckpointCapturesSingleMessage verifies the
// boundary checkpoint (autosave) persists exactly one instruction message,
// not a duplicate. The worker's final checkpoint runs before it appends the
// captured instruction; the boundary save that captures the instruction is
// the TUI's post-Done autosave, which must see exactly one copy.
func TestAuthorizationInstructionCheckpointCapturesSingleMessage(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "session")
	os.MkdirAll(sessionDir, 0755)

	s := &chat.Session{Doc: config.SessionDoc{FileState: map[string]config.FileStateEntry{}}}
	s.Doc.Config.Tools = []string{"bash"}
	s.Doc.Config.Autosave.Enabled = true
	s.Doc.Config.Autosave.Name = "session"
	s.SessionDir = sessionDir
	s.Doc.Messages = []config.Message{{
		ID:   "msg_1",
		Role: config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{
			authToolCallEntry("tool_1"),
			authToolCallEntry("tool_2"),
		},
	}}
	m := toolEventModel(s)
	// Point paths at a temp dir so the boundary autosave's SaveSettings
	// never writes settings.json into the package directory.
	m.paths = config.Paths{Root: t.TempDir()}
	// Simulate a previously persisted session so Save skips the temp-workspace
	// migration (which would relocate the session directory).
	s.InitWorkspace()
	before := len(s.Doc.Messages)

	checkpointErrs := 0
	ch := chat.StartToolExec(context.Background(), s, chat.ToolExecOptions{
		MsgIdx:   0,
		Decision: &chat.AuthDecision{Approved: true, Instructions: tuiAuthInstructionText},
		Checkpoint: func() error {
			err := m.persistAutoSave()
			if err != nil {
				checkpointErrs++
			}
			return err
		},
	})
	for range ch {
	}
	if checkpointErrs > 0 {
		t.Fatalf("checkpoint failed %d times", checkpointErrs)
	}

	// The worker's final checkpoint runs before the instruction append; the
	// boundary save that captures it is the TUI's post-Done autosave.
	if err := m.persistAutoSave(); err != nil {
		t.Fatalf("boundary autosave: %v", err)
	}

	// The persisted document must contain the instruction exactly once.
	data, err := os.ReadFile(filepath.Join(sessionDir, "chat.json"))
	if err != nil {
		t.Fatalf("read persisted session: %v", err)
	}
	if got := strings.Count(string(data), tuiAuthInstructionText); got != 1 {
		t.Fatalf("persisted session must contain the instruction exactly once, got %d", got)
	}

	loaded, err := config.LoadSessionDoc(sessionDir)
	if err != nil {
		t.Fatalf("load persisted session: %v", err)
	}
	users := userMessages(loaded.Messages[before:])
	if len(users) != 1 || users[0].Text != tuiAuthInstructionText {
		t.Fatalf("reloaded document must hold exactly one instruction message, got %+v", users)
	}
}

func authToolCallEntry(id string) config.ToolCallEntry {
	e := config.ToolCallEntry{ID: id}
	e.Instruction.Name = "bash"
	e.Instruction.Arguments = `{"command":"echo ok","destructive":false}`
	return e
}

func userMessages(msgs []config.Message) []config.Message {
	var out []config.Message
	for _, m := range msgs {
		if m.Role == config.RoleUser {
			out = append(out, m)
		}
	}
	return out
}

func countProjectedUserText(msgs []goai_provider.Message, text string) int {
	n := 0
	for _, m := range msgs {
		if m.Role != goai_provider.RoleUser {
			continue
		}
		for _, part := range m.Content {
			if strings.Contains(part.Text, text) {
				n++
			}
		}
	}
	return n
}

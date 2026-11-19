package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/google/uuid"

	"squid-os/internal/config"
	"squid-os/internal/inputqueue"
	"squid-os/internal/ui"
)

// queueChip builds the right-hand status-row chip from the live coordinator.
func (m *Model) queueChip() string {
	if m.submissions == nil || m.session == nil {
		return ""
	}
	id := m.liveSessionID()
	c := m.submissions.Counts(id)
	return ui.RenderQueueChip(c.Step, c.Turn, m.width-2)
}

// liveSessionID returns the immutable identity of the current session.
func (m *Model) liveSessionID() string {
	if m.session == nil {
		return ""
	}
	return m.session.Doc.Identity.ID
}

// admitQueuedInput admits one queued message through the shared coordinator.
// boundary selects next-step (steer current turn) or after-turn (wait for the
// logical turn to end). On success the draft is cleared; on rejection the
// draft is preserved and the error surfaced.
func (m *Model) admitQueuedInput(text string, boundary inputqueue.Boundary) bool {
	if m.submissions == nil {
		(m).setNotification(ui.NotificationError, "input queue unavailable")
		return false
	}
	req := inputqueue.AdmissionRequest{
		RequestID: uuid.NewString(),
		SessionID: m.liveSessionID(),
		Source:    "keyboard",
		Text:      text,
		Boundary:  boundary,
	}
	res := m.submissions.AdmitSubmission(req, m.liveSessionID())
	if !res.OK {
		(m).setNotification(ui.NotificationError, "queued input rejected: "+res.Msg)
		return false
	}
	// Voice barge-in (task 2.2): a submitted queued message cancels the
	// active speech group at admission — even though delivery waits for the
	// next safe boundary. Plain typing never reaches here.
	m.narrator.cancelActive()
	// Async user-language detection (task 3.1): fire at ADMISSION, not at
	// boundary delivery — the language is known as soon as the text is in.
	m.fireUserLangDetect(text)
	m.textarea.SetValue("")
	m.autoSizeTextarea()
	label := "next step"
	if boundary == inputqueue.BoundaryTurn {
		label = "after turn"
	}
	(m).setNotification(ui.NotificationInfo, "queued for "+label)
	return true
}

// flushQueues discards all pending queued input and announces how many were
// cleared. Returns the number discarded.
func (m *Model) flushQueues() int {
	if m.submissions == nil {
		return 0
	}
	n := m.submissions.Discard(m.liveSessionID())
	if n > 0 {
		(m).setNotification(ui.NotificationInfo, "cleared "+itoaUI(n)+" queued message(s)")
	}
	return n
}

func itoaUI(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return strings.TrimSpace(string(append([]byte{}, b[i:]...)))
}

// startStreamStub is a test seam: when set, Model.startStream returns the
// stub's result instead of calling the real provider. Nil in production.
var startStreamStub func(*Model) tea.Cmd

// ── Boundary draining (task 1.4) ────────────────────────────────────────────

// drainBoundary is the safe-boundary transaction for one queue group:
// select+prepare atomically, append each entry EXACTLY ONCE as a user message
// with stable msg_N IDs, then finalize the entries as injected with those IDs.
// It never runs while a tool worker owns the session — callers invoke it only
// at ToolEventDone (after the worker finished) or LoopDone (terminal).
func (m *Model) drainBoundary(boundary inputqueue.Boundary) int {
	if m.submissions == nil {
		return 0
	}
	id := m.liveSessionID()
	if id == "" {
		return 0
	}
	entries := m.submissions.Drain(id, boundary)
	if len(entries) == 0 {
		return 0
	}
	msgIDsByRequest := make(map[string][]string, len(entries))
	for _, e := range entries {
		msgID := fmt.Sprintf("msg_%d", len(m.session.Doc.Messages)+1)
		m.session.Append(config.Message{
			ID:          msgID,
			Role:        config.RoleUser,
			CreatedAt:   time.Now(),
			Text:        e.Text,
			InputTokens: countTokensApprox(e.Text),
			Params:      sourceParams(e.Source),
		})
		msgIDsByRequest[e.RequestID] = []string{msgID}
	}
	// Checkpoint BEFORE marking injected so crash recovery reconciles against
	// the persisted history (design §4). The queue journal update below is the
	// explicit minimal queue checkpoint that runs even when chat autosave is
	// off (documented behavior, design §4).
	if m.session.Doc.Config.Autosave.Enabled && !m.incognito {
		_ = m.persistAutoSave()
	}
	m.submissions.MarkDrained(id, msgIDsByRequest)
	m.session.invalidateRenderAll()
	return len(entries)
}

// flushQueuesToDraft moves all pending queued entries into the textarea as
// visible text with headers, then discards them from the coordinator. Called
// on cancel/error so the user sees everything in one box and decides what to
// send.
func (m *Model) flushQueuesToDraft() {
	if m.submissions == nil {
		return
	}
	id := m.liveSessionID()
	if id == "" {
		return
	}
	stepEntries := m.submissions.Pending(id)
	var parts []string
	for _, e := range stepEntries {
		label := "queued step"
		if e.Boundary == inputqueue.BoundaryTurn {
			label = "queued after-turn"
		}
		parts = append(parts, label+" ─────────────\n"+e.Text)
	}
	if len(parts) == 0 {
		return
	}
	existing := m.textarea.Value()
	if existing != "" {
		m.textarea.SetValue(existing + "\n" + strings.Join(parts, "\n"))
	} else {
		m.textarea.SetValue(strings.Join(parts, "\n"))
	}
	m.autoSizeTextarea()
	m.submissions.Discard(id)
	(m).setNotification(ui.NotificationInfo, fmt.Sprintf("flushed %d queued message(s) to draft", len(parts)))
}

// hasPendingQueue reports whether the live session has any pending queued
// input (used to block identity-changing actions, design §3 rule 6).
func (m *Model) hasPendingQueue() bool {
	if m.submissions == nil {
		return false
	}
	c := m.submissions.Counts(m.liveSessionID())
	return c.Step+c.Turn > 0
}

// guardIdentityChange blocks session-identity changes while pending queued
// input exists. Returns true (and notifies) when blocked.
func (m *Model) guardIdentityChange(action string) bool {
	if !m.hasPendingQueue() {
		return false
	}
	(m).setNotification(ui.NotificationWarning,
		action+" blocked: queued input pending · /queue-clear to discard first")
	return true
}

// sourceParams builds the provenance metadata for a queued message: how it
// arrived (keyboard / voice / cli / …). Persisted in chat.json and shown as a
// chip in the TUI; never sent to the provider (Params is display-only).
func sourceParams(source string) map[string]string {
	if source == "" {
		return nil
	}
	return map[string]string{"source": source}
}

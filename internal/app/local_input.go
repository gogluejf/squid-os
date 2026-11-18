package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/inputqueue"
	"squid-os/internal/localinput"
	"squid-os/internal/ui"
)

// localSocketMsg is pushed onto the Bubble Tea update loop when a socket
// submission arrives while the TUI is idle. Carrying the request ID lets the
// UI side mark the entry injected after the real send — the socket goroutine
// must never touch session state directly.
type localSocketMsg struct {
	requestID string
	sessionID string
	text      string
	source    string
}

// Socket callbacks ask the UI loop for current state; they never retain a Model.
type localSessionQuery struct{ reply chan string }
type localAdmissionMsg struct {
	req   inputqueue.AdmissionRequest
	reply chan localinput.ReceiptInfo
}

// StartLocalInputServer binds the private input socket for this TUI instance
// and wires it to the shared submission coordinator. Returns nil (no-op) when
// no instance ID is configured. The socket is published only after binding;
// Close is called at shutdown.
//
// Delivery rule: a submission arriving while BUSY is parked in the queue for
// the next safe boundary (steering). One arriving while IDLE is sent
// immediately as a normal user message — parking an idle arrival leaves a
// mystery "1 next step" chip that stacks into the user's next message.
func (m *Model) StartLocalInputServer(instanceID string, push func(tea.Msg)) (*localinput.Server, error) {
	if instanceID == "" || m.submissions == nil {
		return nil, nil
	}
	coordinator := m.submissions
	cfg := localinput.ServerConfig{
		InstanceID: instanceID,
		SessionID: func() string {
			if push == nil {
				return ""
			}
			reply := make(chan string, 1)
			push(localSessionQuery{reply: reply})
			select {
			case id := <-reply:
				return id
			case <-time.After(5 * time.Second):
				return ""
			}
		},
		Admit: func(req inputqueue.AdmissionRequest) localinput.ReceiptInfo {
			if push == nil {
				return localinput.ReceiptInfo{Code: "receiver_unavailable", Message: "UI unavailable"}
			}
			reply := make(chan localinput.ReceiptInfo, 1)
			push(localAdmissionMsg{req: req, reply: reply})
			select {
			case result := <-reply:
				return result
			case <-time.After(5 * time.Second):
				return localinput.ReceiptInfo{Code: "receiver_unavailable", Message: "UI admission timed out; check receipt before retrying"}
			}
		},
		Status: func(sessionID string) (localinput.Counts, bool, error) {
			c := coordinator.Counts(sessionID)
			return localinput.Counts{Step: c.Step, Turn: c.Turn}, false, nil
		},
		Control: func(action, sessionID string) (int, error) {
			switch action {
			case "clear":
				return coordinator.Discard(sessionID), nil
			default:
				return 0, fmt.Errorf("unknown control %q", action)
			}
		},
	}
	return cfg.Start()
}

// sendLocalSocketMessage delivers an idle socket submission as a normal user
// message: append to the session, mark the queue entry injected (so no
// boundary ever double-fires it), then start the stream. Runs on the UI
// goroutine only — the socket Admit callback merely pushed this msg.
func (m Model) sendLocalSocketMessage(msg localSocketMsg) (tea.Model, tea.Cmd) {
	if m.session == nil || msg.text == "" {
		return m, nil
	}
	userMsg := config.Message{
		ID:          fmt.Sprintf("msg_%d", len(m.session.Doc.Messages)+1),
		Role:        config.RoleUser,
		CreatedAt:   time.Now(),
		Text:        msg.text,
		InputTokens: countTokensApprox(msg.text),
		Params:      sourceParams(msg.source),
	}
	m.session.Append(userMsg)
	// Mark the parked entry injected with its stable ID: the queue's job is
	// done, the message is in history. If the send below fails (provider
	// unconfigured), the message still stands in history — same contract as
	// a typed Enter.
	if m.submissions != nil {
		m.submissions.MarkDrained(msg.sessionID, map[string][]string{msg.requestID: {userMsg.ID}})
	}
	m.textarea.SetValue("")
	m.autoSizeTextarea()
	(&m).clearNotification()
	(&m).setNotification(ui.NotificationInfo, "local input received")

	if blocked, cmd := (&m).ensureProviderConfigured(); blocked {
		return m, cmd
	}
	if err := chat.PrepareTurn(m.session.Session); err != nil {
		(&m).setNotification(ui.NotificationError, err.Error())
		return m, nil
	}
	return (&m).startStream()
}

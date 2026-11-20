package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/inputqueue"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
	"squid-os/internal/voice"
)

// setStreamMode initializes TUI state for a new request.
// Shared stream state and metrics are initialized by chat.StartStreamWithContext.
func (m *Model) setStreamMode() {
	m.session.UIStream.reset()
	m.session.UIStream.ID = uuid.NewString()
	m.mode = ModeStreaming
	m.session.UIStream.Stopwatch.Start()
	m.textarea.Placeholder = "ctrl+c to cancel..."
	// Voice: one speech group per assistant message. The group survives the
	// UIStreamState.reset() calls between tool phases; only true message
	// completion ends it (narrator.endMessage).
	m.narrator.beginMessage(m.settings.Voice.Enabled)
}

// setAuthMode builds a Question component for tool authorization and sets it as the active component.
func (m *Model) setAuthMode() tea.Cmd {
	m.session.UIStream.Stopwatch.Pause()
	ctx := m.session.UIStream.AuthorizationCtx

	// Description: tool-name · display-value (truncation handled by Question render)
	var description string
	if ctx.DisplayValue != "" {
		description = ctx.ToolName + " · " + ctx.DisplayValue
	}

	q := &component.Question{
		Title:       "Do you authorize this tool?",
		Description: description,
		Options:     []string{"Yes", "No"},
		ShowInput:   true,
		OnConfirm: func(selection int, instructions string, ctx any) tea.Cmd {
			m := ctx.(*Model)
			m.session.UIStream.Stopwatch.Resume()
			m.session.UIStream.AuthorizationCtx.Result = AuthResult{
				Approved:     selection == 0,
				Instructions: instructions,
			}
			_, cmd := m.resumeToolExecution()
			return cmd
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			m.session.UIStream.Stopwatch.Resume()
			m.session.UIStream.AuthorizationCtx.Result = AuthResult{
				Approved:     false,
				Instructions: "",
			}
			_, cmd := m.resumeToolExecution()
			return cmd
		},
	}

	m.setComponent(q)
	m.refreshViewportAtBottom()
	return q.BlinkCmd()
}

// setChatMode sets mode to ModeChat, resets the textarea placeholder, recomputes layout,
// and refreshes the viewport while retaining sticky-bottom behavior.
func (m *Model) setChatMode() tea.Cmd {
	m.textarea.Placeholder = "Type a message..."
	m.mode = ModeChat
	m.turnActive = false
	m.activeComponent = nil
	m.textarea.Focus()
	m.refreshViewportFollowing()
	return textarea.Blink
}

// sendMessage reads the textarea, adds the user turn, and starts streaming
// the assistant reply via the configured provider.
func (m Model) sendMessage() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}

	// Check provider configuration before sending
	if blocked, cmd := (&m).ensureProviderConfigured(); blocked {
		return m, cmd
	}

	if !m.incognito {
		config.AddHistoryEntry(&m.history, text, m.settings.MaxHistory)
		_ = config.SaveHistory(m.paths, m.history)
	}
	m.historyIdx = -1
	m.draft = ""

	// Voice barge-in (task 2.2): a NEW submitted message cancels the active
	// speech group at ADMISSION, before processing begins — from every
	// admission path (idle Enter, queued Enter/Alt+Q, socket). Plain typing
	// without submitting never reaches here, so listen-while-drafting holds.
	m.narrator.cancelActive()

	userMsg := config.Message{
		ID:          fmt.Sprintf("msg_%d", len(m.session.Doc.Messages)+1),
		Role:        config.RoleUser,
		CreatedAt:   time.Now(),
		Text:        text,
		InputTokens: countTokensApprox(text),
		Params:      sourceParams("keyboard"),
	}

	m.session.Append(userMsg)

	if err := chat.PrepareTurn(m.session.Session); err != nil {
		(&m).setNotification(ui.NotificationError, err.Error())
		return m, nil
	}

	m.session.undoStack = nil

	m.textarea.SetValue("")
	// Keep the textarea FOCUSED while streaming: the busy composer must stay
	// editable so the user can draft and queue (Enter/Alt+Q) mid-turn.
	m.textarea.Focus()
	(&m).clearNotification()

	// Async user-language detection (task 3.1): fire-and-forget at admission;
	// adds no turn latency, failures leave lastUserLang intact.
	(&m).fireUserLangDetect(text)

	return (&m).startStream()
}

// handleStreamEvent processes a single token, thinking chunk, error, or done signal
// from the active inference stream by delegating pure processing to chat.ProcessStreamEvent
// and handling TUI side effects here.
func (m Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd) {
	result := chat.ProcessStreamEvent(m.session.Session, event)

	switch result.Action {
	case chat.LoopError:
		msg := "stream error"
		if result.Error != nil {
			msg = result.Error.Error()
		}

		text, truncated := m.session.CancelTruncate()
		if truncated {
			m.session.invalidateRenderFrom(len(m.session.Doc.Messages))
			if text != "" {
				m.textarea.SetValue(text)
			}
		}

		if result.IsAuthError {
			_ = config.PersistProviderAuthState(m.paths, m.endpoints, m.session.CurrentInference().Provider)
			m.modelEntries = nil
			msg = "Authentication failed — use /model to re-authenticate"
			result.MsgIdx = chat.AppendAuthErrorMessage(m.session.Session)
		} else if result.SilentFailure {
			// Generic error (finding #10 ruling): an empty stream with no stop
			// reason is NOT classified as its own ContextBust event. Voice
			// alerts are reactive — they announce what just happened, they do
			// not drive a preventive "compact your context" workflow — so the
			// dedicated context-bust template buys nothing here; the standard
			// Error lead-in + raw detail covers it.
			(&m).speakAlertEvent(voice.Error, msg)
			_ = config.PersistRefreshedProvider(m.paths, m.endpoints, m.session.CurrentInference().Provider)
			result.MsgIdx = chat.AppendSilentFailureMessage(m.session.Session)
		} else {
			// Voice alert (task 3.3): localized lead-in + raw English detail,
			// same group — spoken BEFORE the aborted narration group dies.
			(&m).speakAlertEvent(voice.Error, msg)
			_ = config.PersistRefreshedProvider(m.paths, m.endpoints, m.session.CurrentInference().Provider)
			result.MsgIdx = chat.AppendStreamErrorMessage(m.session.Session, result.Error)
		}
		// Error boundary: flush queued entries to draft for user review.
		(&m).flushQueuesToDraft()
		(&m).setNotification(ui.NotificationError, msg)

		nm, autoSaveCmd := m.autoSave()
		m = nm
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		(&m).narrator.cancelActive() // aborted message: kill group, drop buffer
		(&m).setChatMode()
		return m, autoSaveCmd

	case chat.LoopToolCalls:
		return (&m).resumeToolExecution()

	case chat.LoopDone:
		_ = config.PersistRefreshedProvider(m.paths, m.endpoints, m.session.CurrentInference().Provider)
		if result.Cancelled {
			text, truncated := m.session.CancelTruncate()
			if truncated {
				m.session.invalidateRenderFrom(len(m.session.Doc.Messages))
				if text != "" {
					m.textarea.SetValue(text)
				}
			} else {
				result.MsgIdx = chat.AppendStreamCancelledMessage(m.session.Session)
			}
			// Cancel: flush queued entries to draft for user review.
			(&m).flushQueuesToDraft()
			(&m).setNotification(ui.NotificationInfo, "stream cancelled")
			m.session.Stream.Reset()
			m.session.UIStream.reset()
			(&m).narrator.cancelActive() // aborted message: kill group, drop buffer
			(&m).setChatMode()
			nm, autoSaveCmd := m.autoSave()
			return nm, autoSaveCmd
		}
		// Terminal inference (success boundary, design §3 rule 2): the
		// assistant message is already appended by ProcessStreamEvent.
		// Step queue nonempty → extend the logical turn; else after-turn
		// queue nonempty → one new turn with ALL of it; else end normally.
		if n := (&m).drainBoundary(inputqueue.BoundaryStep); n > 0 {
			m.session.Stream.Reset()
			m.session.UIStream.reset()
			(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("injected %d queued next-step message(s)", n))
			return (&m).startStream()
		}
		if n := (&m).drainBoundary(inputqueue.BoundaryTurn); n > 0 {
			m.session.Stream.Reset()
			m.session.UIStream.reset()
			(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("injected %d queued after-turn message(s)", n))
			return (&m).startStream()
		}
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		// Voice: terminal inference — flush the final buffer and close the
		// group. Done BEFORE setChatMode so the utterance is complete when
		// the turn ends; queued next-step messages start a fresh group via
		// setStreamMode.
		(&m).narrator.endMessage(context.Background())
		(&m).setChatMode()
		nm, autoSaveCmd := m.autoSave()
		return nm, autoSaveCmd

	case chat.LoopContinue:
		// Notify user about media-related events (omissions, retries)
		if event.Text != "" && strings.HasPrefix(event.Text, "Retrying without attachments") {
			(&m).setNotification(ui.NotificationInfo, event.Text)
		}

		if event.ToolCallDelta != "" {
			m.refreshViewportFollowing()
			return m, waitForStreamEvent(m.session.UIStream.Ch)
		}
		// Voice: feed the rendered text delta into the chunker NOW so the
		// first sentence speaks while later sentences are still generating.
		if event.Text != "" {
			(&m).narrator.feedDelta(event.Text)
		}
		// Thinking-start alert (task 3.3): once per thinking session, only
		// when thinking mode is actually enabled in the session config AND
		// the model enters a think block. The engine sets InThinking on the
		// first thinking delta and it stays true until the block ends, so
		// this fires exactly once.
		if event.InThinking && !m.session.UIStream.ThinkingAnnounced &&
			m.session.EffectiveInference().Thinking.Enabled {
			m.session.UIStream.ThinkingAnnounced = true
			(&m).speakAlertEvent(voice.ThinkingStart, "")
		}
		m.session.UIStream.TokenCount++
		if m.session.UIStream.TokenCount%3 == 0 {
			m.refreshViewportFollowing()
		}
		return m, waitForStreamEvent(m.session.UIStream.Ch)
	}

	return m, waitForStreamEvent(m.session.UIStream.Ch)
}

// resumeToolExecution starts tool execution in a background worker and returns
// a command that delivers its first event. Slow tools (bash, agents) no longer
// block the Bubble Tea event loop; each event is handled by handleToolEvent.
func (m *Model) resumeToolExecution() (tea.Model, tea.Cmd) {
	msgIdx := m.session.UIStream.MsgIdx
	var decision *chat.AuthDecision
	if m.session.UIStream.AuthorizationCtx != nil {
		msgIdx = m.session.UIStream.MsgIdx
		decision = &chat.AuthDecision{
			Approved:     m.session.UIStream.AuthorizationCtx.Result.Approved,
			Instructions: m.session.UIStream.AuthorizationCtx.Result.Instructions,
		}
		m.session.UIStream.AuthorizationCtx = nil
	}

	opts := chat.ToolExecOptions{
		Decision: decision,
		MsgIdx:   msgIdx,
		Checkpoint: func() error {
			return m.persistAutoSave()
		},
	}
	// Derive the worker context from the turn context so a single
	// CancelTurnCtx (ctrl+c) stops the loop and kills in-flight children.
	parent := m.session.Session.TurnContext()
	if parent == nil {
		parent = context.Background()
	}
	ch := chat.StartToolExec(parent, m.session.Session, opts)
	m.session.UIStream.ToolCh = ch
	return m, waitForToolEvent(ch)
}

// handleToolEvent processes one tool-execution event from the worker goroutine.
// All session mutation happens inside the worker; here we only apply TUI side
// effects (render invalidation, modes, notifications) and drive the next step.
func (m Model) handleToolEvent(event chat.ToolEvent) (tea.Model, tea.Cmd) {
	switch event.Type {
	case chat.ToolEventRunning:
		if event.MsgIdx >= 0 {
			m.session.invalidateRenderFrom(event.MsgIdx)
		}
		if event.LoadedSkill != "" {
			m.session.Doc.Config.ActiveSkill = event.LoadedSkill
			if m.session.Doc.Pending != nil {
				m.session.Doc.Pending.ActiveSkill = nil
			}
			// Voice alert (task 3.3): "I loaded the skill {name}" — fired at
			// RUNNING (pre-execution visibility), matching ToolEventDone's
			// later state application; both events carry LoadedSkill.
			(&m).speakAlertEvent(voice.SkillLoaded, event.LoadedSkill)
		}
		// Sub-agent alert (task 3.3): "I sent a sub-agent working on it" at
		// PRE-EXECUTION — AgentName is set by the worker only when this
		// Running event corresponds to an agent tool starting.
		if event.AgentName != "" {
			(&m).speakAlertEvent(voice.SubAgent, event.AgentName)
		}
		m.refreshViewportFollowing()
		return m, waitForToolEvent(m.session.UIStream.ToolCh)

	case chat.ToolEventNeedAuth:
		m.session.UIStream.AuthorizationCtx = &AuthorizationContext{
			ToolName:      event.AuthRequest.ToolName,
			Args:          event.AuthRequest.Args,
			ArgsJSON:      event.AuthRequest.ArgsJSON,
			DisplayValue:  event.AuthRequest.DisplayValue,
			IsDestructive: event.AuthRequest.IsDestructive,
		}
		m.session.UIStream.MsgIdx = event.MsgIdx
		// Voice alert (task 3.3): "a command requires your authorization" in
		// the user's language, as its own group — get-my-eyes-here moment.
		(&m).speakAlertEvent(voice.Authorization, "")
		m.setAuthMode()
		return m, nil

	case chat.ToolEventDone:
		m.session.UIStream.AuthorizationCtx = nil
		if event.MsgIdx >= 0 {
			m.session.invalidateRenderFrom(event.MsgIdx)
		}
		if event.LoadedSkill != "" {
			m.session.Doc.Config.ActiveSkill = event.LoadedSkill
			if m.session.Doc.Pending != nil {
				m.session.Doc.Pending.ActiveSkill = nil
			}
		}
		// The worker already appended any captured authorization instruction
		// to the session; only invalidate its render here. Appending again
		// would duplicate the user message in history and provider context.
		if event.AppendedUserMsgIdx >= 0 {
			m.session.invalidateRenderFrom(event.AppendedUserMsgIdx)
		}
		// Tool batch done = safe steering boundary (design §3 rule 1): the
		// worker finished all mutations, so drain pending NEXT-STEP entries
		// now — each exactly once — before issuing ONE continuation.
		// After-turn entries stay pending.
		if n := (&m).drainBoundary(inputqueue.BoundaryStep); n > 0 {
			(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("injected %d queued next-step message(s)", n))
		}
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		m.refreshViewportFollowing()
		return (&m).startStream()

	case chat.ToolEventCancelled:
		// Mirror the stream-cancel path: the worker marked the stream
		// cancelled and finalized the tool entries; record the synthetic
		// "aborted" message from the shared CancelMessage and go to chat.
		m.session.invalidateRenderAll()
		chat.AppendStreamCancelledMessage(m.session.Session)
		// Cancel: flush queued entries to draft for user review.
		(&m).flushQueuesToDraft()
		(&m).setNotification(ui.NotificationInfo, event.CancelMessage)
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		(&m).narrator.cancelActive() // aborted turn: kill group, drop buffer
		(&m).setChatMode()
		nm, autoSaveCmd := m.autoSave()
		return nm, autoSaveCmd

	case chat.ToolEventError:
		msg := "tool execution failed"
		if event.Error != nil {
			msg = event.Error.Error()
		}
		// Error: flush queued entries to draft for user review.
		(&m).flushQueuesToDraft()
		(&m).setNotification(ui.NotificationError, msg)
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		(&m).narrator.cancelActive() // aborted turn: kill group, drop buffer
		return m, m.setChatMode()
	}
	return m, nil
}

// startStream builds API messages from current session state and starts a new stream.
func (m *Model) startStream() (tea.Model, tea.Cmd) {
	m.setStreamMode()
	m.turnActive = true

	// Test seam: boundary tests stub the provider call so they never touch
	// the network or spawn real streams. Nil in production.
	if startStreamStub != nil {
		return m, startStreamStub(m)
	}

	ch := chat.StartStream(context.Background(), m.session.Session, m.endpoints)
	m.session.UIStream.Ch = ch

	m.refreshViewportAtBottom() // should it be following ? refreshViewportFollowing ? only sendmessage to gotobottom ??
	return m, waitForStreamEvent(ch)
}

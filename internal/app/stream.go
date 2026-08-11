package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

// setStreamMode initializes TUI state for a new request.
// Shared stream state and metrics are initialized by chat.StartStreamWithContext.
func (m *Model) setStreamMode() {
	m.session.UIStream.reset()
	m.session.UIStream.ID = uuid.NewString()
	m.mode = ModeStreaming
	m.session.UIStream.Stopwatch.Start()
	m.textarea.Placeholder = "ctrl+c to cancel..."
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
	m.refreshViewportFollowing()
	return q.BlinkCmd()
}

// setChatMode sets mode to ModeChat, resets the textarea placeholder, recomputes layout,
// and refreshes the viewport while retaining sticky-bottom behavior.
func (m *Model) setChatMode() tea.Cmd {
	m.textarea.Placeholder = "Type a message..."
	m.mode = ModeChat
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

	userMsg := config.Message{
		ID:          fmt.Sprintf("msg_%d", len(m.session.Doc.Messages)+1),
		Role:        config.RoleUser,
		CreatedAt:   time.Now(),
		Text:        text,
		ImagePath:   m.attachedImage,
		InputTokens: countTokensApprox(text),
	}

	m.session.Append(userMsg)

	if err := chat.PrepareTurn(m.session.Session); err != nil {
		(&m).setNotification(ui.NotificationError, err.Error())
		return m, nil
	}

	m.session.undoStack = nil

	m.textarea.SetValue("")
	m.textarea.Blur()
	m.attachedImage = ""
	(&m).clearNotification()

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

		text, image, truncated := m.session.CancelTruncate()
		if truncated {
			m.session.invalidateRenderFrom(len(m.session.Doc.Messages))
			if text != "" {
				m.textarea.SetValue(text)
				m.attachedImage = image
			}
		}

		if result.IsAuthError {
			_ = config.PersistProviderAuthState(m.paths, m.endpoints, m.session.CurrentInference().Provider)
			m.modelEntries = nil
			msg = "Authentication failed — use /model to re-authenticate"
			result.MsgIdx = chat.AppendAuthErrorMessage(m.session.Session)
		} else if result.SilentFailure {
			_ = config.PersistRefreshedProvider(m.paths, m.endpoints, m.session.CurrentInference().Provider)
			result.MsgIdx = chat.AppendSilentFailureMessage(m.session.Session)
		} else {
			_ = config.PersistRefreshedProvider(m.paths, m.endpoints, m.session.CurrentInference().Provider)
			result.MsgIdx = chat.AppendStreamErrorMessage(m.session.Session, result.Error)
		}
		(&m).setNotification(ui.NotificationError, msg)

		nm, autoSaveCmd := m.autoSave()
		m = nm
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		(&m).setChatMode()
		return m, autoSaveCmd

	case chat.LoopToolCalls:
		return (&m).resumeToolExecution()

	case chat.LoopDone:
		_ = config.PersistRefreshedProvider(m.paths, m.endpoints, m.session.CurrentInference().Provider)
		if result.Cancelled {
			text, image, truncated := m.session.CancelTruncate()
			if truncated {
				m.session.invalidateRenderFrom(len(m.session.Doc.Messages))
				if text != "" {
					m.textarea.SetValue(text)
					m.attachedImage = image
				}
			} else {
				result.MsgIdx = chat.AppendStreamCancelledMessage(m.session.Session)
			}
			(&m).setNotification(ui.NotificationInfo, "stream cancelled")
		}
		m.session.Stream.Reset()
		m.session.UIStream.reset()
		(&m).setChatMode()
		nm, autoSaveCmd := m.autoSave()
		return nm, autoSaveCmd

	case chat.LoopContinue:
		if event.ToolCallDelta != "" {
			m.refreshViewportFollowing()
			return m, waitForStreamEvent(m.session.UIStream.Ch)
		}
		m.session.UIStream.TokenCount++
		if m.session.UIStream.TokenCount%3 == 0 {
			m.refreshViewportFollowing()
		}
		return m, waitForStreamEvent(m.session.UIStream.Ch)
	}

	return m, waitForStreamEvent(m.session.UIStream.Ch)
}

// resumeToolExecution runs or resumes pure tool execution and handles TUI side effects.
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

	for {
		result := chat.ExecuteTools(m.session.Session, chat.ToolExecOptions{
			Decision: decision,
			MsgIdx:   msgIdx,
		})
		decision = nil

		if result.MsgIdx >= 0 {
			m.session.invalidateRenderFrom(result.MsgIdx)
		}
		if result.LoadedSkill != "" {
			m.session.Doc.Config.ActiveSkill = result.LoadedSkill
			if m.session.Doc.Pending != nil {
				m.session.Doc.Pending.ActiveSkill = nil
			}
		}

		switch result.Action {
		case chat.ToolExecNeedAuth:
			m.session.UIStream.AuthorizationCtx = &AuthorizationContext{
				ToolName:      result.AuthRequest.ToolName,
				Args:          result.AuthRequest.Args,
				ArgsJSON:      result.AuthRequest.ArgsJSON,
				DisplayValue:  result.AuthRequest.DisplayValue,
				IsDestructive: result.AuthRequest.IsDestructive,
			}
			m.session.UIStream.MsgIdx = result.MsgIdx
			m.setAuthMode()
			m.refreshViewportFollowing()
			nm, autoSaveCmd := m.autoSave()
			if autoSaveCmd != nil {
				return nm, autoSaveCmd
			}
			return m, nil

		case chat.ToolExecContinue:
			m.refreshViewportFollowing()
			msgIdx = result.MsgIdx
			continue

		case chat.ToolExecDone:
			m.session.UIStream.AuthorizationCtx = nil
			if result.CapturedUserText != "" {
				userMsg := config.Message{
					ID:        fmt.Sprintf("msg_%d", len(m.session.Doc.Messages)+1),
					Role:      config.RoleUser,
					CreatedAt: time.Now(),
					Text:      result.CapturedUserText,
				}

				m.session.Append(userMsg)
			}
			nm, autoSaveCmd := m.autoSave()
			m.session.Stream.Reset()
			m.session.UIStream.reset()
			m.refreshViewportFollowing()
			if autoSaveCmd != nil {
				nextModel, nextCmd := nm.startStream()
				return nextModel, tea.Batch(nextCmd, autoSaveCmd)
			}
			return m.startStream()
		}
	}
}

// startStream builds API messages from current session state and starts a new stream.
func (m *Model) startStream() (tea.Model, tea.Cmd) {
	m.setStreamMode()

	ch := chat.StartStream(m.session.Session, m.endpoints)
	m.session.UIStream.Ch = ch

	m.refreshViewportAtBottom()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd(m.session.UIStream.ID))
}

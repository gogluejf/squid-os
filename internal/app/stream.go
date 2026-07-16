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
	"squid-os/internal/tools"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

// needsAuthorization checks the current authorization mode and tool destructiveness
// to determine if user confirmation is required before execution.
func (m Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool {
	switch m.settings.ValidateAuthorization() {
	case config.AuthorizationAskForAll:
		return true
	case config.AuthorizationAskOnWrite:
		return tool != nil && tool.IsDestructive != nil && tool.IsDestructive(args)
	default: // auto
		return false
	}
}

// setStreamMode initializes the stream state for a new request.
func (m *Model) setStreamMode() {
	m.session.Stream.Reset()
	m.session.UIStream.reset()
	m.session.UIStream.ID = uuid.NewString()
	m.session.Stream.Active = true
	m.session.Stream.Metrics.Start = time.Now()
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
	m.updateViewportContent()
	return q.BlinkCmd()
}

// setChatMode sets mode to ModeChat, resets the textarea placeholder, recomputes layout,
// and re-renders the viewport. Callers should not call updateViewportContent() separately
// after setChatMode — the layout must be recalculated first (to restore full viewport
// height after component overlays) before rendering.
func (m *Model) setChatMode() tea.Cmd {
	m.textarea.Placeholder = "Type a message..."
	m.mode = ModeChat
	m.activeComponent = nil
	m.textarea.Focus()
	m.updateViewportContent()
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

	// Commit all pending per-turn transitions immediately after the user message,
	// so they belong to this turn and are removed together by destroy-last-sequence.
	if m.session.Doc.Session.Skill.Next != nil && *m.session.Doc.Session.Skill.Next != m.session.Doc.Session.Skill.Current {
		(&m).injectSkillChangeSynthetic(m.session.Doc.Session.Skill.Current, *m.session.Doc.Session.Skill.Next)
		m.session.Doc.Session.Skill.Current = *m.session.Doc.Session.Skill.Next
		m.session.Doc.Session.Skill.Next = nil
	}
	current := m.session.Doc.Session.Inference.Current
	if current.Model != m.settings.Model || current.Provider != m.settings.Provider {
		m.session.PushModelSwitch(current.Provider+"/"+current.Model, m.settings.Provider+"/"+m.settings.Model)
	}
	if current.Thinking != m.settings.Thinking {
		m.session.PushThinkingSwitch(m.settings.Thinking)
	}
	if current.Provider != m.settings.Provider || current.Model != m.settings.Model || current.Thinking != m.settings.Thinking {
		m.session.SetInference(config.InferenceConfig{Provider: m.settings.Provider, Model: m.settings.Model, Thinking: m.settings.Thinking})
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
		if text != "" {
			m.textarea.SetValue(text)
			m.attachedImage = image
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

		if truncated {
			m.session.invalidateRenderFrom(len(m.session.Doc.Messages))
		}

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
			m.updateViewportContent()
			return m, waitForStreamEvent(m.session.UIStream.Ch)
		}
		m.session.UIStream.TokenCount++
		if m.session.UIStream.TokenCount%3 == 0 {
			m.updateViewportContent()
		}
		return m, waitForStreamEvent(m.session.UIStream.Ch)
	}

	return m, waitForStreamEvent(m.session.UIStream.Ch)
}

// resumeToolExecution runs or resumes pure tool execution and handles TUI side effects.
func (m *Model) resumeToolExecution() (tea.Model, tea.Cmd) {
	msgIdx := -1
	startIndex := 0
	var decision *chat.AuthDecision
	if m.session.UIStream.AuthorizationCtx != nil {
		msgIdx = m.session.UIStream.MsgIdx
		startIndex = m.session.UIStream.PendingToolIndex
		decision = &chat.AuthDecision{
			Approved:     m.session.UIStream.AuthorizationCtx.Result.Approved,
			Instructions: m.session.UIStream.AuthorizationCtx.Result.Instructions,
		}
		m.session.UIStream.AuthorizationCtx = nil
	}

	for {
		result := chat.ExecuteTools(m.session.Session, m.toolReg, chat.ToolExecOptions{
			AuthorizationMode: m.settings.ValidateAuthorization(),
			Decision:          decision,
			MsgIdx:            msgIdx,
			StartIndex:        startIndex,
		})
		decision = nil

		if result.MsgIdx >= 0 {
			m.session.invalidateRenderFrom(result.MsgIdx)
		}
		if result.WorkingDir != "" {
			m.applyWorkingDir(result.WorkingDir)
		}
		if result.LoadedSkill != "" {
			m.session.Doc.Session.Skill.Current = result.LoadedSkill
			m.session.Doc.Session.Skill.Next = nil
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
			m.session.UIStream.PendingToolIndex = result.ToolIndex
			m.session.UIStream.MsgIdx = result.MsgIdx
			m.setAuthMode()
			m.updateViewportContent()
			nm, autoSaveCmd := m.autoSave()
			if autoSaveCmd != nil {
				return nm, autoSaveCmd
			}
			return m, nil

		case chat.ToolExecContinue:
			m.updateViewportContent()
			msgIdx = result.MsgIdx
			startIndex = result.NextIndex
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
			m.updateViewportContent()
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
	m.toolReg = tools.GetRegistry()

	ch := chat.StartStream(m.session.Session, m.endpoints)
	m.session.UIStream.Ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd(m.session.UIStream.ID))
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/chat"
	"rig-chat/internal/config"
	"rig-chat/internal/tools"
	"rig-chat/internal/ui"
)

// streamState bundles all transient fields for an active inference stream.
type streamState struct {
	text          string
	thinking      string
	inThinking    bool
	active        bool
	markdown      string // glamour cache for completed lines
	markdownEnd   int
	metrics       StreamMetrics
	cancelFn      context.CancelFunc
	ch            <-chan chat.StreamEvent
	userCancelled bool        // true if user pressed cancel
	pendingTools  []chat.ToolCall // accumulated tool calls across stream events
}

// AddTextChunk appends text and updates metrics.
func (ss *streamState) AddTextChunk(text string) {
	ss.text += text
	ss.metrics.addTextChars(len(text))
}

// AddThinkChunk appends thinking text and updates metrics.
func (ss *streamState) AddThinkChunk(think string) {
	ss.thinking += think
	ss.metrics.addThinkChars(len(think))
}

// reset clears all stream state before a new request.
func (ss *streamState) reset() {
	ss.text = ""
	ss.thinking = ""
	ss.inThinking = false
	ss.active = false
	ss.markdown = ""
	ss.markdownEnd = -1
	ss.metrics = StreamMetrics{}
	ss.cancelFn = nil
	ss.ch = nil
	ss.userCancelled = false
}

// setStreamMode initializes the stream state for a new request.
func (m *Model) setStreamMode() {
	m.stream.reset()
	m.stream.active = true
	m.stream.metrics.Start = time.Now()
	m.mode = ModeStreaming
	m.textarea.Placeholder = "ctrl+c to cancel..."
}

// setChatMode sets mode to ModeChat, resets the textarea placeholder, and recomputes layout.
func (m *Model) setChatMode() tea.Cmd {
	m.textarea.Placeholder = "Type a message..."
	m.mode = ModeChat
	m.textarea.Focus()
	m.recalcLayout()
	return textarea.Blink
}

// scanModelsCmd launches an async model scan and returns the result as a modelsLoadedMsg.
func (m Model) scanModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := chat.ScanModels(ctx, m.endpoints)
		return modelsLoadedMsg{models: models}
	}
}

// sendMessage reads the textarea, adds the user turn, and starts streaming
// the assistant reply via the configured provider.
func (m Model) sendMessage() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}

	if !m.incognito {
		config.AddHistoryEntry(&m.history, text, m.settings.MaxHistory)
		_ = config.SaveHistory(m.paths, m.history)
	}
	m.historyIdx = -1
	m.draft = ""

	userMsg := config.Message{
		ID:         fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:       "user",
		CreatedAt:  time.Now(),
		Text:       text,
		ImagePath:  m.attachedImage,
		UserTokens: countTokensApprox(text),
	}

	m.session.appendMsg(userMsg)
	m.session.undoStack = nil

	m.textarea.SetValue("")
	m.textarea.Blur()

	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.file.Messages)
	m.attachedImage = ""

	(&m).setStreamMode()
	(&m).availTools = tools.GetTools()
	(&m).clearNotification()

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.stream.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs, m.availTools)
	m.stream.ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

// handleStreamEvent processes a single token, thinking chunk, error, or done signal
// from the active inference stream.
func (m Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd) {
	if event.Error != nil {
		(&m).setNotification(ui.NotificationError, event.Error.Error())

		text, image := m.session.truncateToUser()
		m.textarea.SetValue(text)
		m.attachedImage = image

		m.stream.reset()
		cmd := (&m).setChatMode()
		m.updateViewportContent()
		return m, cmd
	}

	if event.Done {
		if m.stream.userCancelled {
			text, image := m.session.truncateToUser()
			m.textarea.SetValue(text)
			m.attachedImage = image

			(&m).setNotification(ui.NotificationInfo, "stream cancelled")

			m.stream.reset()
			blinkCmd := (&m).setChatMode()
			m.updateViewportContent()
			nm, autoSaveCmd := m.autoSave()
			return nm, tea.Batch(blinkCmd, autoSaveCmd)
		}

		// Tool calls: save assistant msg, execute tools synchronously, resume streaming
		if event.StopReason == "tool_calls" && len(m.stream.pendingTools) > 0 {
			pendingTools:= m.stream.pendingTools // capture before reset
			assistantMsg := config.Message{
				ID:                         fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
				Role:                       "assistant",
				CreatedAt:                  m.stream.metrics.Start,
				Text:                       m.stream.text,
				ThinkingText:               m.stream.thinking,
				ThinkingTokens:             m.stream.metrics.ThinkingTokens(),
				ThinkingDurationMs:         m.stream.metrics.ThinkingDuration().Milliseconds(),
				ThinkingTimeToFirstTokenMs: m.stream.metrics.TimeToFirstThinkingToken().Milliseconds(),
				TextTokens:                 m.stream.metrics.TextTokens(),
				TextDurationMs:             m.stream.metrics.TextDuration().Milliseconds(),
				TextTimeToFirstTokenMs:     m.stream.metrics.TimeToFirstTextToken().Milliseconds(),
				TokensPerSecond:            m.stream.metrics.AvgTokenPerSec(),
				Tokens:                     m.stream.metrics.TotalTokens(),
				DurationTimeMs:             m.stream.metrics.Duration().Milliseconds(),
				TimeToFirstTokenMs:         m.stream.metrics.TimeToFirstToken().Milliseconds(),
				StopReason:                 event.StopReason,
				ToolCalls:                  make([]config.ToolCallEntry, len(pendingTools)),
			}
			for i, tc := range pendingTools {
				assistantMsg.ToolCalls[i] = config.ToolCallEntry{
					ID:        tc.ID,
					Type:      tc.Type,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Args,
				}
			}
			m.session.appendMsg(assistantMsg)

			// Execute tools inline - they're fast I/O ops
			(&m).executeTools(pendingTools)

			m.stream.reset()
			m.updateViewportContent()

			// Resume streaming with tool results in history
			return (&m).startStream()
		}

		// Normal completion: save assistant message
		assistantMsg := config.Message{
			ID:                         fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
			Role:                       "assistant",
			CreatedAt:                  m.stream.metrics.Start,
			Text:                       m.stream.text,
			ThinkingText:               m.stream.thinking,
			ThinkingTokens:             m.stream.metrics.ThinkingTokens(),
			ThinkingDurationMs:         m.stream.metrics.ThinkingDuration().Milliseconds(),
			ThinkingTimeToFirstTokenMs: m.stream.metrics.TimeToFirstThinkingToken().Milliseconds(),
			TextTokens:                 m.stream.metrics.TextTokens(),
			TextDurationMs:             m.stream.metrics.TextDuration().Milliseconds(),
			TextTimeToFirstTokenMs:     m.stream.metrics.TimeToFirstTextToken().Milliseconds(),
			TokensPerSecond:            m.stream.metrics.AvgTokenPerSec(),
			Tokens:                     m.stream.metrics.TotalTokens(),
			DurationTimeMs:             m.stream.metrics.Duration().Milliseconds(),
			TimeToFirstTokenMs:         m.stream.metrics.TimeToFirstToken().Milliseconds(),
			StopReason:                 event.StopReason,
		}
		m.session.appendMsg(assistantMsg)

		m.stream.reset()
		blinkCmd := (&m).setChatMode()
		m.updateViewportContent()
		nm, autoSaveCmd := m.autoSave()
		return nm, tea.Batch(blinkCmd, autoSaveCmd)
	}

	if len(event.ToolCalls) > 0 {
		m.stream.pendingTools = append(m.stream.pendingTools, event.ToolCalls...)
	}
	if event.Text != "" {
		m.stream.AddTextChunk(event.Text)
	}
	if event.Thinking != "" {
		m.stream.AddThinkChunk(event.Thinking)
	}
	if m.stream.inThinking && !event.InThinking {
		m.stream.metrics.MarkThinkingDone()
	}
	m.stream.inThinking = event.InThinking
	m.updateViewportContent()
	return m, waitForStreamEvent(m.stream.ch)
}

// executeTools runs all pending tool calls, appends results to session,
// and updates the assistant message with results.
func (m *Model) executeTools(toolCalls []chat.ToolCall) {
	var toolResults []config.ToolResultEntry

	for _, tc := range toolCalls {
		tool := findTool(tc.Function.Name, m.availTools)
		if tool == nil {
			toolResults = append(toolResults, config.ToolResultEntry{
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Result:     "",
				Error:      fmt.Sprintf("unknown tool: %s", tc.Function.Name),
			})
			continue
		}

		var args map[string]interface{}
		if tc.Function.Args != "" {
			_ = json.Unmarshal([]byte(tc.Function.Args), &args)
		}

		result, err := tool.Execute(args)
		if err != nil {
			toolResults = append(toolResults, config.ToolResultEntry{
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Result:     result,
				Error:      err.Error(),
			})
		} else {
			toolResults = append(toolResults, config.ToolResultEntry{
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Result:     result,
			})
		}

		// Update assistant message tool call with result
		for i := len(m.session.file.Messages) - 1; i >= 0; i-- {
			msg := &m.session.file.Messages[i]
			if msg.Role == "assistant" && msg.ToolCalls != nil {
				for j, tce := range msg.ToolCalls {
					if tce.ID == tc.ID {
						if err != nil {
							msg.ToolCalls[j].Result = ""
							msg.ToolCalls[j].Error = err.Error()
						} else {
							msg.ToolCalls[j].Result = result
							msg.ToolCalls[j].Error = ""
						}
						break
					}
				}
			}
		}
	}

	// Append tool result message
	toolMsg := config.Message{
		ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:        "tool",
		CreatedAt:   time.Now(),
		ToolResults: toolResults,
	}
	m.session.appendMsg(toolMsg)
}

// startStream builds API messages from current session state and starts a new stream.
func (m *Model) startStream() (tea.Model, tea.Cmd) {
	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.file.Messages)

	m.setStreamMode()

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.stream.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs, m.availTools)
	m.stream.ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

func findTool(name string, toolList []tools.Tool) *tools.Tool {
	for _, t := range toolList {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

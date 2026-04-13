package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/chat"
	"rig-chat/internal/config"
)

// scanModelsCmd launches an async model scan and returns the result as a modelsLoadedMsg.
func (m Model) scanModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := chat.ScanModels(ctx, m.endpoints, m.modelCache)
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

	userMsg := config.DisplayMessage{
		Message: config.Message{
			ID:          fmt.Sprintf("msg_%d", len(m.messages)+1),
			Role:        "user",
			CreatedAt:   time.Now(),
			Text:        text,
			ImagePath:   m.attachedImage,
			InputTokens: countTokensApprox(text),
		},
	}
	m.messages = append(m.messages, userMsg)

	m.textarea.SetValue("")
	m.textarea.Blur()

	apiMsgs := m.buildAPIMessages()
	m.attachedImage = ""

	m.streaming = true
	m.mode = ModeStreaming
	m.textarea.Placeholder = "ctrl+c to cancel..."
	m.streamText = ""
	m.streamThinking = ""
	m.inThinking = false
	m.tokenCount = 0
	m.streamStart = time.Now()
	m.firstTokenTime = time.Time{}
	m.lastError = ""

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)

	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs)
	m.streamCh = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

// handleStreamEvent processes a single token, thinking chunk, error, or done signal
// from the active inference stream.
func (m Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd) {
	if event.Error != nil {
		m.lastError = event.Error.Error()
		m.streaming = false
		m.mode = ModeChat
		m.textarea.Placeholder = "Type a message..."
		m.textarea.Focus()
		m.recalcLayout()
		m.updateViewportContent()
		return m, nil
	}

	if event.Done {
		assistantMsg := config.DisplayMessage{
			Message: config.Message{
				ID:              fmt.Sprintf("msg_%d", len(m.messages)+1),
				Role:            "assistant",
				CreatedAt:       m.streamStart,
				Text:            m.streamText,
				ThinkingText:    m.streamThinking,
				OutputTokens:    m.tokenCount,
				TokensPerSecond: m.calcTokPerSec(),
				ResponseTimeMs:  time.Since(m.streamStart).Milliseconds(),
				StopReason:      event.StopReason,
			},
		}
		m.messages = append(m.messages, assistantMsg)
		m.session.Messages = m.extractSessionMessages()
		m.totalTokens += m.tokenCount
		m.tokenCount = 0 // flush so footer (totalTokens + tokenCount) doesn't double-count
		m.streaming = false
		m.mode = ModeChat
		m.textarea.Placeholder = "Type a message..."
		m.textarea.Focus()
		m.recalcLayout()
		m.updateViewportContent()
		nm, cmd := m.autoSave()
		return nm, cmd
	}

	if event.Text != "" {
		m.streamText += event.Text
		m.tokenCount += countTokensApprox(event.Text)
		if m.firstTokenTime.IsZero() {
			m.firstTokenTime = time.Now()
		}
	}
	if event.Thinking != "" {
		m.streamThinking += event.Thinking
		m.tokenCount += countTokensApprox(event.Thinking)
		if m.firstTokenTime.IsZero() {
			m.firstTokenTime = time.Now()
		}
	}
	m.inThinking = event.InThinking
	m.updateViewportContent()
	return m, waitForStreamEvent(m.streamCh)
}

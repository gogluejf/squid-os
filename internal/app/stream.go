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

// streamState bundles all transient fields for an active inference stream.
type streamState struct {
	text           string
	thinking       string
	inThinking     bool
	active         bool
	markdown       string // glamour cache for completed lines
	markdownEnd    int
	tokenCount     int
	start          time.Time
	firstTokenTime time.Time
	cancelFn       context.CancelFunc
	ch             <-chan chat.StreamEvent
}

// reset clears all stream state before a new request.
func (ss *streamState) reset() {
	ss.text = ""
	ss.thinking = ""
	ss.inThinking = false
	ss.active = false
	ss.markdown = ""
	ss.markdownEnd = -1
	ss.tokenCount = 0
	ss.start = time.Time{}
	ss.firstTokenTime = time.Time{}
	ss.cancelFn = nil
	ss.ch = nil
}

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
			ID:          fmt.Sprintf("msg_%d", len(m.session.messages)+1),
			Role:        "user",
			CreatedAt:   time.Now(),
			Text:        text,
			ImagePath:   m.attachedImage,
			InputTokens: countTokensApprox(text),
		},
	}
	m.session.appendMsg(userMsg)

	m.textarea.SetValue("")
	m.textarea.Blur()

	apiMsgs := m.buildAPIMessages()
	m.attachedImage = ""

	m.stream.reset()
	m.stream.active = true
	m.stream.start = time.Now()
	m.mode = ModeStreaming
	m.textarea.Placeholder = "ctrl+c to cancel..."
	m.lastError = ""

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.stream.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs)
	m.stream.ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

// handleStreamEvent processes a single token, thinking chunk, error, or done signal
// from the active inference stream.
func (m Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd) {
	if event.Error != nil {
		m.lastError = event.Error.Error()
		m.stream.active = false
		(&m).returnToChat()
		m.updateViewportContent()
		return m, nil
	}

	if event.Done {
		assistantMsg := config.DisplayMessage{
			Message: config.Message{
				ID:              fmt.Sprintf("msg_%d", len(m.session.messages)+1),
				Role:            "assistant",
				CreatedAt:       m.stream.start,
				Text:            m.stream.text,
				ThinkingText:    m.stream.thinking,
				OutputTokens:    m.stream.tokenCount,
				TokensPerSecond: m.calcTokPerSec(),
				ResponseTimeMs:  time.Since(m.stream.start).Milliseconds(),
				StopReason:      event.StopReason,
			},
		}
		m.session.appendMsg(assistantMsg)
		m.session.file.Messages = m.session.extractMessages()
		m.session.totalTokens += m.stream.tokenCount
		m.stream.tokenCount = 0
		m.stream.active = false
		(&m).returnToChat()
		m.updateViewportContent()
		nm, cmd := m.autoSave()
		return nm, cmd
	}

	if event.Text != "" {
		m.stream.text += event.Text
		m.stream.tokenCount += countTokensApprox(event.Text)
		if m.stream.firstTokenTime.IsZero() {
			m.stream.firstTokenTime = time.Now()
		}
	}
	if event.Thinking != "" {
		m.stream.thinking += event.Thinking
		m.stream.tokenCount += countTokensApprox(event.Thinking)
		if m.stream.firstTokenTime.IsZero() {
			m.stream.firstTokenTime = time.Now()
		}
	}
	m.stream.inThinking = event.InThinking
	m.updateViewportContent()
	return m, waitForStreamEvent(m.stream.ch)
}

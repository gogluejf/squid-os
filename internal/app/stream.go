package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
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
	userCancelled  bool   // true if user pressed cancel
	originalText   string // Store original textarea value for restore on cancel
	originalImage  string // Store original attached image for restore on cancel
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
	ss.userCancelled = false
	ss.originalText = ""
	ss.originalImage = ""
}

// setStreamMode initializes the stream state for a new request.
func (m *Model) setStreamMode(originalText, originalImage string) {
	m.stream.reset()
	m.stream.originalText = originalText
	m.stream.originalImage = originalImage
	m.stream.active = true
	m.stream.start = time.Now()
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

	// Store original values in temp variables for restore on cancel
	originalText := text
	originalImage := m.attachedImage

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

	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.messages)
	m.attachedImage = ""

	(&m).setStreamMode(originalText, originalImage)
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

		// Remove the user message that was added before streaming started
		n := len(m.session.messages)
		if n > 0 && m.session.messages[n-1].Role == "user" {
			m.session.truncateTo(n - 1)
		}

		// Restore textarea and image
		if m.stream.originalText != "" {
			m.textarea.SetValue(m.stream.originalText)
		}
		if m.stream.originalImage != "" {
			m.attachedImage = m.stream.originalImage
		}

		m.stream.reset()
		cmd := (&m).setChatMode()
		m.updateViewportContent()
		return m, cmd
	}

	if event.Done {
		// If user cancelled, perform cleanup
		if m.stream.userCancelled {
			// Remove the user message
			n := len(m.session.messages)
			if n > 0 && m.session.messages[n-1].Role == "user" {
				m.session.truncateTo(n - 1)
			}

			// Restore textarea and image
			if m.stream.originalText != "" {
				m.textarea.SetValue(m.stream.originalText)
			}
			if m.stream.originalImage != "" {
				m.attachedImage = m.stream.originalImage
			}
		} else {
			// Save assistant message if not cancelled
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
		}

		m.stream.reset()
		blinkCmd := (&m).setChatMode()
		m.updateViewportContent()
		nm, autoSaveCmd := m.autoSave()
		return nm, tea.Batch(blinkCmd, autoSaveCmd)
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

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/tools"
	"squid-os/internal/ui"
)

// partialTool holds the streaming-in-progress state for a single tool call.
// It is the single source of truth — populated incrementally from ToolCallDelta
// events and enriched with ID/Type from the ToolCalls flush event.
type partialTool struct {
	id      string
	typeStr string
	name    string
	args    string
	chars   int
	firstAt time.Time
	doneAt  time.Time
}

// toStreamingToolCalls converts all partial tools with a non-empty name into
// display-ready StreamingToolCall values for the streaming viewport.
func (ss *streamState) toStreamingToolCalls() []ui.StreamingToolCall {
	var out []ui.StreamingToolCall
	for _, p := range ss.partialTools {
		if p.name == "" {
			continue
		}
		dur := time.Duration(0)
		if !p.firstAt.IsZero() {
			dur = p.doneAt.Sub(p.firstAt)
		}
		out = append(out, ui.StreamingToolCall{
			Name:      p.name,
			Arguments: p.args,
			Tokens:    countTokensApproxInt(p.chars),
			Duration:  dur,
		})
	}
	return out
}

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
	userCancelled bool
	partialTools  []partialTool // live state during arg streaming, indexed by tool call index
	lastToolIdx   int           // index of the last tool that received a delta (-1 if none)
	tokenCount    int           // counter for throttling viewport updates
}

// AddTextChunk appends text and updates metrics.
func (ss *streamState) AddTextChunk(text string) {
	ss.text += text
	ss.metrics.addTextChars(text)
}

// AddThinkChunk appends thinking text and updates metrics.
func (ss *streamState) AddThinkChunk(think string) {
	ss.thinking += think
	ss.metrics.addThinkChars(think)
}

// AddToolCallChunk tracks tool call argument streaming for timing/token metrics.
func (ss *streamState) AddToolCallChunk(delta string) {
	ss.metrics.addToolCallChars(delta)
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
	ss.partialTools = nil
	ss.lastToolIdx = -1
	ss.tokenCount = 0
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
		ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:        config.RoleUser,
		CreatedAt:   time.Now(),
		Text:        text,
		ImagePath:   m.attachedImage,
		InputTokens: countTokensApprox(text),
	}

	m.session.appendMsg(userMsg)
	m.session.undoStack = nil

	m.textarea.SetValue("")
	m.textarea.Blur()

	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.file.Messages)
	m.attachedImage = ""

	(&m).setStreamMode()
	(&m).toolReg = tools.GetRegistry()
	(&m).clearNotification()

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.stream.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs, tools.GetTools())
	m.stream.ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

// handleStreamEvent processes a single token, thinking chunk, error, or done signal
// from the active inference stream.
func (m Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd) {
	if event.Error != nil {
		(&m).setNotification(ui.NotificationError, event.Error.Error())

		text, image, _ := m.session.cancelTruncate()
		if text != "" {
			m.textarea.SetValue(text)
			m.attachedImage = image
		}

		errText := "Stream error: " + event.Error.Error()
		m.session.appendMsg(config.Message{
			ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
			Role:        config.RoleSynthetic,
			CreatedAt:   time.Now(),
			Text:        errText,
			Label:       "stream error",
			TextMetrics: config.ContentMetrics{Tokens: countTokensApprox(errText)},
		})

		nm, autoSaveCmd := m.autoSave()
		m = nm

		m.stream.reset()
		cmd := (&m).setChatMode()
		m.updateViewportContent()
		return m, tea.Batch(cmd, autoSaveCmd)
	}

	if event.Done {
		if m.stream.userCancelled {
			text, image, truncated := m.session.cancelTruncate()

			if truncated {
				if text != "" {
					m.textarea.SetValue(text)
					m.attachedImage = image
				}
			} else {
				// Push a synthetic message only if the user message was NOT truncated
				// (i.e., we cancelled mid-tool-loop, user message is still in history).
				text := "Stream aborted by user"
				m.session.appendMsg(config.Message{
					ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
					Role:        config.RoleSynthetic,
					CreatedAt:   time.Now(),
					Text:        text,
					Label:       "aborted",
					TextMetrics: config.ContentMetrics{Tokens: countTokensApprox(text)},
				})
			}

			(&m).setNotification(ui.NotificationInfo, "stream cancelled")

			m.stream.reset()
			blinkCmd := (&m).setChatMode()
			m.updateViewportContent()
			nm, autoSaveCmd := m.autoSave()
			return nm, tea.Batch(blinkCmd, autoSaveCmd)
		}

		// Detect silent failure: stream ended with no content and no stop reason.
		// This happens when the server drops the connection (VRAM OOM, etc.) without
		// sending an error or proper finish_reason. Treat it as an error.
		hasContent := m.stream.text != "" || m.stream.thinking != "" || len(m.stream.partialTools) > 0
		if !hasContent && event.StopReason == "" {
			(&m).setNotification(ui.NotificationError, "stream ended unexpectedly — server may be overloaded")

			text, image, _ := m.session.cancelTruncate()
			if text != "" {
				m.textarea.SetValue(text)
				m.attachedImage = image
			}

			errText := "Stream ended unexpectedly — server may be overloaded (VRAM / model loading)"
			m.session.appendMsg(config.Message{
				ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
				Role:        config.RoleSynthetic,
				CreatedAt:   time.Now(),
				Text:        errText,
				Label:       "stream error",
				TextMetrics: config.ContentMetrics{Tokens: countTokensApprox(errText)},
			})

			nm, autoSaveCmd := m.autoSave()
			m = nm

			m.stream.reset()
			cmd := (&m).setChatMode()
			m.updateViewportContent()
			return m, tea.Batch(cmd, autoSaveCmd)
		}

		//need to be processed before execution of tools, because tool calls can update the stream state (e.g. end thinking)
		avgTokenPerSec := m.stream.metrics.AvgTokenPerSec()

		// Tool calls: save assistant msg, execute tools synchronously, resume streaming
		if event.StopReason == "tool_calls" && len(m.stream.partialTools) > 0 {
			toolEntries := (&m).executeTools(m.stream.partialTools)
			(&m).appendAssistantMsg(config.Message{
				ID:                 fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
				Role:               config.RoleAssistant,
				CreatedAt:          m.stream.metrics.Start,
				ThinkingText:       strings.TrimLeft(m.stream.thinking, "\n"),
				ThinkingMetrics:    config.ContentMetrics{Tokens: m.stream.metrics.ThinkingTokens(), InferenceDuractionMs: m.stream.metrics.ThinkingDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstThinkingToken().Milliseconds()},
				Text:               strings.TrimLeft(m.stream.text, "\n"),
				TextMetrics:        config.ContentMetrics{Tokens: m.stream.metrics.TextTokens(), InferenceDuractionMs: m.stream.metrics.TextDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstTextToken().Milliseconds()},
				ToolCalls:          toolEntries,
				ToolCallMetrics:    config.ContentMetrics{Tokens: m.stream.metrics.ToolCallTokens(), InferenceDuractionMs: m.stream.metrics.ToolCallDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstToolCallToken().Milliseconds()},
				TokensPerSecond:    avgTokenPerSec,
				OutputTokens:       m.stream.metrics.TotalOutputTokens(),
				InputTokens:        config.TotalExecutionTokens(toolEntries),
				DurationTimeMs:     m.stream.metrics.Duration().Milliseconds(),
				TimeToFirstTokenMs: m.stream.metrics.TimeToFirstToken().Milliseconds(),
				StopReason:         event.StopReason,
			})

			m.stream.reset()
			m.updateViewportContent()

			// Resume streaming with tool results in history
			return (&m).startStream()
		}

		// Normal completion: save assistant message
		(&m).appendAssistantMsg(config.Message{
			ID:                 fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
			Role:               config.RoleAssistant,
			CreatedAt:          m.stream.metrics.Start,
			ThinkingText:       strings.TrimLeft(m.stream.thinking, "\n"),
			ThinkingMetrics:    config.ContentMetrics{Tokens: m.stream.metrics.ThinkingTokens(), InferenceDuractionMs: m.stream.metrics.ThinkingDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstThinkingToken().Milliseconds()},
			Text:               strings.TrimLeft(m.stream.text, "\n"),
			TextMetrics:        config.ContentMetrics{Tokens: m.stream.metrics.TextTokens(), InferenceDuractionMs: m.stream.metrics.TextDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstTextToken().Milliseconds()},
			TokensPerSecond:    avgTokenPerSec,
			OutputTokens:       m.stream.metrics.TotalOutputTokens(),
			DurationTimeMs:     m.stream.metrics.Duration().Milliseconds(),
			TimeToFirstTokenMs: m.stream.metrics.TimeToFirstToken().Milliseconds(),
			StopReason:         event.StopReason,
		})

		m.stream.reset()
		blinkCmd := (&m).setChatMode()
		m.updateViewportContent()
		nm, autoSaveCmd := m.autoSave()
		return nm, tea.Batch(blinkCmd, autoSaveCmd)
	}

	// ToolCallDelta: accumulate per-tool state and update display.
	if event.ToolCallDelta != "" {
		m.stream.AddToolCallChunk(event.ToolCallDelta)
		for len(m.stream.partialTools) <= event.ToolCallIdx {
			m.stream.partialTools = append(m.stream.partialTools, partialTool{})
		}
		m.stream.lastToolIdx = event.ToolCallIdx
		p := &m.stream.partialTools[event.ToolCallIdx]
		if event.ToolCallName != "" {
			p.name = event.ToolCallName
		}
		p.args += event.ToolCallDelta
		p.chars += len(event.ToolCallDelta)
		if p.firstAt.IsZero() {
			p.firstAt = time.Now()
		}
		p.doneAt = time.Now()

		if m.stream.inThinking {
			m.stream.inThinking = false
		}
		m.updateViewportContent()
		return m, waitForStreamEvent(m.stream.ch)
	}

	// ToolCalls flush: enrich partialTools with ID/Type.
	if len(event.ToolCalls) > 0 {
		for i, tc := range event.ToolCalls {
			if i < len(m.stream.partialTools) {
				m.stream.partialTools[i].id = tc.ID
				m.stream.partialTools[i].typeStr = tc.Type
			}
		}

	}
	if event.Text != "" {
		m.stream.AddTextChunk(event.Text)
	}
	if event.Thinking != "" {
		m.stream.AddThinkChunk(event.Thinking)
	}

	m.stream.inThinking = event.InThinking
	// Throttle viewport updates: render every 5 tokens to avoid SetContent() spam
	m.stream.tokenCount++
	if m.stream.tokenCount%3 == 0 {
		m.updateViewportContent()
	}
	return m, waitForStreamEvent(m.stream.ch)
}

// executeTools runs all pending tool calls and returns ToolCallEntry slice
// with both Instruction and Execution populated. Validates against session-level
// file state before each write/edit, and accumulates results into session state.
func (m *Model) executeTools(partials []partialTool) []config.ToolCallEntry {
	entries := make([]config.ToolCallEntry, len(partials))

	if m.session.file.FileState == nil {
		m.session.file.FileState = make(map[string]config.FileStateEntry)
	}
	sessionState := m.session.file.FileState

	for i, p := range partials {
		dur := p.doneAt.Sub(p.firstAt).Milliseconds()
		entries[i] = config.ToolCallEntry{
			ID:   p.id,
			Type: p.typeStr,
			Instruction: struct {
				Name       string `json:"name"`
				Arguments  string `json:"arguments"`
				Tokens     int    `json:"tokens,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
			}{Name: p.name, Arguments: p.args, Tokens: countTokensApprox(p.args), DurationMs: dur},
		}

		tool := m.toolReg.Get(p.name)
		if tool == nil {
			entries[i].Execution.Status = tools.ResultStatusError
			entries[i].Execution.Error = fmt.Sprintf("unknown tool: %s", p.name)
			continue
		}

		var args map[string]interface{}
		if p.args != "" {
			_ = json.Unmarshal([]byte(p.args), &args)
		}

		// Validate against session-level file state for all tools except read_file.
		// We skip read_file because if the checksum is stale, reading it is exactly how
		// the model refreshes its understanding of the file.
		if p.name != "read_file" {
			if pathVal, ok := args["path"].(string); ok {
				if err := tools.Validate(pathVal, sessionState); err != nil {
					entries[i].Execution.Status = tools.ResultStatusError
					entries[i].Execution.Error = fmt.Sprintf("file changed externally: %s (call read_file again before editing)", pathVal)
					// Cancel remaining tools in this batch: they likely depend on stale state
					for j := i + 1; j < len(partials); j++ {
						entries[j].Execution.Status = tools.ResultStatusError
						entries[j].Execution.Error = "cancelled: prior tool failed due to file change, remaining tools skipped"
					}
					break
				}
			}
		}

		resultStart := time.Now()
		result := tool.Execute(args)
		entries[i].Execution.Status = result.Status
		entries[i].Execution.Result = result.Result
		entries[i].Execution.Error = result.Error

		content := result.Result
		if result.Status == tools.ResultStatusError {
			content = result.Error
		}

		entries[i].Execution.Tokens = countTokensApprox(content)

		entries[i].Execution.DurationMs = time.Since(resultStart).Milliseconds()
		entries[i].Execution.Files = result.Files

		// Accumulate into session state immediately
		tools.MergeEntries(result.Files, sessionState)

		if p.name == "set_working_dir" && result.Status == tools.ResultStatusSuccess {
			if pathVal, ok := args["path"].(string); ok {
				m.applyWorkingDir(pathVal)
			}
		}
	}
	return entries
}

// startStream builds API messages from current session state and starts a new stream.
func (m *Model) startStream() (tea.Model, tea.Cmd) {
	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.file.Messages)

	m.setStreamMode()
	m.toolReg = tools.GetRegistry()

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.stream.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs, tools.GetTools())
	m.stream.ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

// appendAssistantMsg saves an assistant message and maintains SequenceStat on the
// sequence head (first assistant message after the last user message).
// InputTokens accumulates tool execution result tokens (fed back to the model).
func (m *Model) appendAssistantMsg(msg config.Message) {
	seqIdx := config.FindSequenceHeadIdx(m.session.file.Messages)
	if seqIdx == -1 {
		// First of sequence — init SequenceStat + FileState
		stat := &config.SequenceStat{
			OutputTokens:         msg.OutputTokens,
			DurationMs:           msg.DurationTimeMs,
			InferenceDuractionMs: msg.TextMetrics.InferenceDuractionMs + msg.ThinkingMetrics.InferenceDuractionMs + msg.ToolCallMetrics.InferenceDuractionMs,
			AvgTokensPerSec:      msg.TokensPerSecond,
			InputTokens:          msg.InputTokens,
			FileState:            make(map[string]config.FileStateEntry),
		}
		for _, tc := range msg.ToolCalls {
			stat.ExecDurMs += tc.Execution.DurationMs
			for _, fe := range tc.Execution.Files {
				stat.FileState[fe.Path] = config.FileStateEntry{
					Checksum:  fe.Checksum,
					Trace:     fe.Trace,
					UpdatedAt: fe.Time,
				}
			}
		}
		msg.SequenceStat = stat
		m.session.appendMsg(msg)
	} else {
		// Subsequent message — accumulate into sequence head + merge file state
		m.session.appendMsg(msg)
		head := m.session.file.Messages[seqIdx].SequenceStat
		head.Accumulate(msg)
		if head.FileState == nil {
			head.FileState = make(map[string]config.FileStateEntry)
		}
		for _, tc := range msg.ToolCalls {
			tools.MergeEntries(tc.Execution.Files, head.FileState)
		}
	}
}

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
	"squid-os/internal/ui/component"
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
	text             string
	thinking         string
	inThinking       bool
	active           bool
	markdown         string // glamour cache for completed lines
	markdownEnd      int
	metrics          StreamMetrics
	cancelFn         context.CancelFunc
	ch               <-chan chat.StreamEvent
	userCancelled    bool
	partialTools     []partialTool         // live state during arg streaming, indexed by tool call index
	tokenCount       int                   // counter for throttling viewport updates
	authorizationCtx *AuthorizationContext // non-nil when paused awaiting auth
	pendingToolIndex int                   // index into partialTools being authorized
	msgIdx           int                   // index of the saved assistant message with tool calls (-1 if none)
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

	ss.tokenCount = 0
	ss.authorizationCtx = nil
	ss.pendingToolIndex = -1
	ss.msgIdx = -1
}

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
	m.stream.reset()
	m.stream.active = true
	m.stream.metrics.Start = time.Now()
	m.mode = ModeStreaming
	m.textarea.Placeholder = "ctrl+c to cancel..."
}

// setAuthMode builds a Question component for tool authorization and sets it as the active component.
func (m *Model) setAuthMode() tea.Cmd {
	ctx := m.stream.authorizationCtx

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
			m.stream.authorizationCtx.Result = AuthResult{
				Approved:     selection == 0,
				Instructions: instructions,
			}
			_, cmd := m.resumeToolExecution(nil, 0)
			return cmd
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			m.stream.authorizationCtx.Result = AuthResult{
				Approved:     false,
				Instructions: "",
			}
			_, cmd := m.resumeToolExecution(nil, 0)
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
		ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:        config.RoleUser,
		CreatedAt:   time.Now(),
		Text:        text,
		ImagePath:   m.attachedImage,
		InputTokens: countTokensApprox(text),
	}

	m.session.appendMsg(userMsg)

	// Commit all pending per-turn transitions immediately after the user message,
	// so they belong to this turn and are removed together by destroy-last-sequence.
	if m.session.file.Session.Skill.Next != nil && *m.session.file.Session.Skill.Next != m.session.file.Session.Skill.Current {
		(&m).injectSkillChangeSynthetic(m.session.file.Session.Skill.Current, *m.session.file.Session.Skill.Next)
		m.session.file.Session.Skill.Current = *m.session.file.Session.Skill.Next
		m.session.file.Session.Skill.Next = nil
	}
	current := m.session.file.Session.Inference.Current
	if current.Model != m.settings.Model || current.Provider != m.settings.Provider {
		m.session.pushModelSwitchMsg(current.Provider+"/"+current.Model, m.settings.Provider+"/"+m.settings.Model)
	}
	if current.Thinking != m.settings.Thinking {
		m.session.pushThinkingSwitchMsg(m.settings.Thinking)
	}
	if current.Provider != m.settings.Provider || current.Model != m.settings.Model || current.Thinking != m.settings.Thinking {
		m.session.commitCurrentInference(m.settings.Provider, m.settings.Model, m.settings.Thinking)
	}

	m.session.undoStack = nil

	m.textarea.SetValue("")
	m.textarea.Blur()

	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.file.Messages)
	m.attachedImage = ""

	(&m).setStreamMode()
	(&m).toolReg = tools.GetRegistry()
	(&m).clearNotification()

	providerSettings := config.ResolveProviderSettings(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(providerSettings, m.settings.Model, m.settings.Thinking)

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
		(&m).setChatMode()
		return m, autoSaveCmd
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
			(&m).setChatMode()
			nm, autoSaveCmd := m.autoSave()
			return nm, autoSaveCmd
		}

		// Detect silent failure: stream ended with no content and no stop reason.
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
			(&m).setChatMode()
			return nm, autoSaveCmd
		}

		avgTokenPerSec := m.stream.metrics.AvgTokenPerSec()

		// Tool calls: end the stream, save eagerly, start execution
		if event.StopReason == "tool_calls" && len(m.stream.partialTools) > 0 {
			(&m).stream.active = false // inference done, entering tool execution
			_ = avgTokenPerSec
			return (&m).resumeToolExecution(nil, 0)
		}

		// Normal completion: save assistant message
		_ = (&m).appendAssistantMsg(config.Message{
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
		(&m).setChatMode()
		nm, autoSaveCmd := m.autoSave()
		return nm, autoSaveCmd
	}

	// ToolCallDelta: accumulate per-tool state and update display.
	if event.ToolCallDelta != "" {
		m.stream.AddToolCallChunk(event.ToolCallDelta)
		for len(m.stream.partialTools) <= event.ToolCallIdx {
			m.stream.partialTools = append(m.stream.partialTools, partialTool{})
		}

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

	// ToolCalls flush: treat flushed tool calls as the canonical final state.
	// They may arrive even if delta-time partial state was incomplete, so make
	// sure partialTools is long enough and backfill missing fields, especially name.
	// Args are already repaired in chat.flushToolCalls().
	if len(event.ToolCalls) > 0 {
		for i, tc := range event.ToolCalls {
			for len(m.stream.partialTools) <= i {
				m.stream.partialTools = append(m.stream.partialTools, partialTool{})
			}
			m.stream.partialTools[i].id = tc.ID
			m.stream.partialTools[i].typeStr = tc.Type
			if m.stream.partialTools[i].name == "" {
				m.stream.partialTools[i].name = tc.Name
			}
			m.stream.partialTools[i].args = tc.ArgsJSON
		}
	}
	if event.Text != "" {
		m.stream.AddTextChunk(event.Text)
	}
	if event.Thinking != "" {
		m.stream.AddThinkChunk(event.Thinking)
	}

	m.stream.inThinking = event.InThinking
	m.stream.tokenCount++
	if m.stream.tokenCount%3 == 0 {
		m.updateViewportContent()
	}
	return m, waitForStreamEvent(m.stream.ch)
}

// buildInstructionEntry creates a ToolCallEntry with Instruction populated, Execution empty.
func buildInstructionEntry(p partialTool) config.ToolCallEntry {
	dur := p.doneAt.Sub(p.firstAt).Milliseconds()
	return config.ToolCallEntry{
		ID:   p.id,
		Type: p.typeStr,
		Instruction: struct {
			Name       string `json:"name"`
			Arguments  string `json:"arguments"`
			Tokens     int    `json:"tokens,omitempty"`
			DurationMs int64  `json:"duration_ms,omitempty"`
		}{Name: p.name, Arguments: p.args, Tokens: countTokensApprox(p.args), DurationMs: dur},
	}
}

// resumeToolExecution runs the single tool-execution loop from startIndex.
// On first call from handleStreamEvent, entries is nil (will be built and saved).
// On resume after auth response, entries is nil (will be fetched from saved message).
//
// Single loop, three gates:
//  1. Apply collected auth result (from resume after user response)
//  2. Auth gate — pause and yield if confirmation needed
//  3. File change gate — cancel remaining if file was modified externally
//
// Then execute. The assistant message is saved before the loop starts with
// initial metrics, and mutated in place as tools execute. After the loop:
// finalize remaining metrics, optionally append user message, start stream.
func (m *Model) resumeToolExecution(entries []config.ToolCallEntry, startIndex int) (tea.Model, tea.Cmd) {
	partials := m.stream.partialTools

	if m.session.file.FileState == nil {
		m.session.file.FileState = make(map[string]config.FileStateEntry)
	}
	sessionState := m.session.file.FileState

	var msgIdx int

	// First call: build all instruction entries and save eagerly.
	// Resume after auth: fetch entries from the already-saved message.
	if entries == nil {
		if m.stream.authorizationCtx != nil {
			// Resuming after auth — entries already live in the saved message.
			msgIdx = m.stream.msgIdx
			startIndex = m.stream.pendingToolIndex
			entries = m.session.file.Messages[msgIdx].ToolCalls
		} else {
			// Initial call from handleStreamEvent: build and save before the loop.
			entries = make([]config.ToolCallEntry, len(partials))
			for i, p := range partials {
				entries[i] = buildInstructionEntry(p)
				entries[i].Execution.Status = tools.ResultStatusPending
			}

			// Save eagerly with all metrics known at stream end.
			// Only InputTokens (tool execution result tokens) is unknown — finalized after loop.
			msgIdx = m.appendAssistantMsg(config.Message{
				ID:                 fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
				Role:               config.RoleAssistant,
				CreatedAt:          m.stream.metrics.Start,
				ThinkingText:       strings.TrimLeft(m.stream.thinking, "\n"),
				ThinkingMetrics:    config.ContentMetrics{Tokens: m.stream.metrics.ThinkingTokens(), InferenceDuractionMs: m.stream.metrics.ThinkingDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstThinkingToken().Milliseconds()},
				Text:               strings.TrimLeft(m.stream.text, "\n"),
				TextMetrics:        config.ContentMetrics{Tokens: m.stream.metrics.TextTokens(), InferenceDuractionMs: m.stream.metrics.TextDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstTextToken().Milliseconds()},
				ToolCalls:          entries,
				ToolCallMetrics:    config.ContentMetrics{Tokens: m.stream.metrics.ToolCallTokens(), InferenceDuractionMs: m.stream.metrics.ToolCallDuration().Milliseconds(), TimeToFirstTokenMs: m.stream.metrics.TimeToFirstToolCallToken().Milliseconds()},
				TokensPerSecond:    m.stream.metrics.AvgTokenPerSec(),
				OutputTokens:       m.stream.metrics.TotalOutputTokens(),
				DurationTimeMs:     m.stream.metrics.Duration().Milliseconds(),
				TimeToFirstTokenMs: m.stream.metrics.TimeToFirstToken().Milliseconds(),
				StopReason:         "tool_calls",
				// InputTokens = 0 — finalized after loop
			})

			// Store msgIdx for auth resume.
			m.stream.msgIdx = msgIdx

			// entries now shares the underlying array with the saved message's ToolCalls,
			// so mutations to entries[i] are mutations to the persisted message.
			startIndex = 0
		}
	}

	// Captured from auth result if the user provided instructions (approved + instructions).
	// After the loop we decide: inject if non-empty, saveAndResume otherwise.
	var capturedInstructions string

	for i := startIndex; i < len(partials); i++ {
		p := partials[i]

		tool := m.toolReg.Get(p.name)
		if tool == nil {
			entries[i].Execution.Status = tools.ResultStatusError
			entries[i].Execution.Error = fmt.Sprintf("unknown tool: %s", p.name)
			continue
		}

		var args map[string]interface{}
		if p.args != "" {
			argsJSON := p.args
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				// Try repairing malformed JSON before giving up
				argsJSON, repaired := chat.RepairArgs(argsJSON)
				if repaired {
					if err2 := json.Unmarshal([]byte(argsJSON), &args); err2 != nil {
						err = err2
					}
				}
				if args == nil {
					entries[i].Execution.Status = tools.ResultStatusError
					entries[i].Execution.Error = fmt.Sprintf("malformed tool arguments from model: %v", err)
					for j := i + 1; j < len(entries); j++ {
						entries[j].Execution.Status = tools.ResultStatusError
						entries[j].Execution.Error = "cancelled: prior tool had malformed arguments"
					}
					break
				}
			}
		}

		// --- Apply previously collected auth result (from resume after user response) ---
		if m.stream.authorizationCtx != nil && m.stream.pendingToolIndex == i {
			result := m.stream.authorizationCtx.Result
			m.stream.authorizationCtx = nil

			if result.Instructions != "" {
				capturedInstructions = result.Instructions
			}

			if !result.Approved {
				// Rejected — cancel this tool and all remaining, break.
				entries[i].Execution.Status = tools.ResultStatusError
				entries[i].Execution.Error = "rejected by user — tool was not executed, don't retry."
				for j := i + 1; j < len(entries); j++ {
					entries[j].Execution.Status = tools.ResultStatusError
					entries[j].Execution.Error = "cancelled: previous tool was not approved"
				}
				break
			}
			// Approved (with or without instructions) — skip the auth gate, fall through to execute.
			goto doExecute
		}

		// --- Gate 1: File change validation (BEFORE auth) ---
		if p.name != "read_file" && p.name != "open" {
			if pathVal, ok := args["path"].(string); ok {
				resolvedPath := tools.ResolvePath(pathVal)
				if err := tools.Validate(resolvedPath, sessionState); err != nil {
					entries[i].Execution.Status = tools.ResultStatusError
					entries[i].Execution.Error = fmt.Sprintf("blocked: file changed externally: %s — tool was not executed. Read the file again uisng the tool \"read_file\" and retry your command.", resolvedPath)
					for j := i + 1; j < len(entries); j++ {
						entries[j].Execution.Status = tools.ResultStatusError
						entries[j].Execution.Error = "cancelled: prior tool failed due to file change, remaining tools skipped"
					}
					break
				}
			}
		}

		// --- Gate 2: Authorization ---
		if m.needsAuthorization(tool, args) {
			// Run on-demand preview before showing the auth question.
			if tool.Preview != nil {
				preview := tool.Preview(args)
				if preview.Status == tools.ResultStatusError {
					entries[i].Execution.Status = tools.ResultStatusError
					entries[i].Execution.Error = preview.Error
					continue
				}
				entries[i].Execution.Status = tools.ResultStatusPending
				entries[i].Execution.Result = preview.Result
				entries[i].Execution.Files = preview.Files
				for j := range preview.Files {
					preview.Files[j].ToolCallID = p.id
				}
			} else {
				entries[i].Execution.Status = tools.ResultStatusPending
			}
			for j := i + 1; j < len(partials); j++ {
				entries[j].Execution.Status = tools.ResultStatusPending
				entries[j].Execution.Error = "waiting: prior tool requires authorization"
			}
			isDestructive := false
			if tool.IsDestructive != nil {
				isDestructive = tool.IsDestructive(args)
			}
			m.stream.authorizationCtx = &AuthorizationContext{
				ToolName:      p.name,
				Args:          args,
				ArgsJSON:      p.args,
				DisplayValue:  tool.DisplayValue(p.args),
				IsDestructive: isDestructive,
			}
			m.stream.pendingToolIndex = i
			m.setAuthMode()
			m.flushToolMessage(msgIdx)
			m.updateViewportContent()
			nm, autoSaveCmd := m.autoSave()
			if autoSaveCmd != nil {
				return nm, autoSaveCmd
			}
			return m, nil
		}

		// --- Execute the tool ---
	doExecute:

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

		for j := range result.Files {
			result.Files[j].ToolCallID = p.id
		}
		entries[i].Execution.Files = result.Files

		tools.MergeEntries(result.Files, sessionState)

		if p.name == "set_working_dir" && result.Status == tools.ResultStatusSuccess {
			if pathVal, ok := args["path"].(string); ok {
				m.applyWorkingDir(pathVal)
			}
		}

		if p.name == "skill_load" && result.Status == tools.ResultStatusSuccess {
			if name, ok := args["name"].(string); ok {
				m.session.file.Session.Skill.Current = name
				m.session.file.Session.Skill.Next = nil
			}
		}

		// After execution: if user provided instructions, cancel remaining and stop.
		if capturedInstructions != "" {
			for j := i + 1; j < len(entries); j++ {
				entries[j].Execution.Status = tools.ResultStatusError
				entries[j].Execution.Error = "cancelled: user provided instructions before this tool could execute"
			}
			break
		}

		// Flush metrics and update viewport after each tool.
		m.flushToolMessage(msgIdx)
		m.updateViewportContent()
	}

	// --- Loop done ---
	m.stream.authorizationCtx = nil

	// Final pass: ensure metrics reflect complete state (covers early breaks
	// where the last loop iteration didn't reach the incremental update).
	m.flushToolMessage(msgIdx)

	if capturedInstructions != "" {
		userMsg := config.Message{
			ID:        fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
			Role:      config.RoleUser,
			CreatedAt: time.Now(),
			Text:      capturedInstructions,
		}
		m.session.appendMsg(userMsg)
	}

	nm, autoSaveCmd := m.autoSave()

	m.stream.reset()
	m.updateViewportContent()
	if autoSaveCmd != nil {
		nextModel, nextCmd := nm.startStream()
		return nextModel, tea.Batch(nextCmd, autoSaveCmd)
	}
	return m.startStream()
}

// startStream builds API messages from current session state and starts a new stream.
func (m *Model) startStream() (tea.Model, tea.Cmd) {
	apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, m.session.file.Messages)

	m.setStreamMode()
	m.toolReg = tools.GetRegistry()

	providerSettings := config.ResolveProviderSettings(m.endpoints, m.settings.Provider)
	engine := chat.NewEngine(providerSettings, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.stream.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs, tools.GetTools())
	m.stream.ch = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

// flushToolMessage updates metrics, recomputes sequence stats, and invalidates
// the render cache for the saved assistant message at msgIdx.
func (m *Model) flushToolMessage(msgIdx int) {
	msg := &m.session.file.Messages[msgIdx]
	msg.DurationTimeMs = m.stream.metrics.Duration().Milliseconds()
	msg.InputTokens = config.TotalExecutionTokens(msg.ToolCalls)
	recomputeSequenceStats(m.session.file.Messages)
	m.session.invalidateRenderFrom(msgIdx)
}

// recomputeSequenceStats scans all assistant messages after the last user message
// and writes a fresh SequenceStat on the head (first assistant). This replaces
// incremental Accumulate with a full recompute — simpler, correct, and cheap
// (~20 messages = <1μs).
func recomputeSequenceStats(messages []config.Message) {
	seqIdx := config.FindSequenceHeadIdx(messages)
	if seqIdx == -1 {
		return
	}

	head := messages[seqIdx]
	stat := &config.SequenceStat{}

	for i := seqIdx; i < len(messages); i++ {
		msg := messages[i]
		stat.OutputTokens += msg.OutputTokens
		stat.DurationMs += msg.DurationTimeMs
		stat.InferenceDuractionMs += msg.TextMetrics.InferenceDuractionMs
		stat.InferenceDuractionMs += msg.ThinkingMetrics.InferenceDuractionMs
		stat.InferenceDuractionMs += msg.ToolCallMetrics.InferenceDuractionMs
		stat.InputTokens += msg.InputTokens
		for _, tc := range msg.ToolCalls {
			stat.ExecDurMs += tc.Execution.DurationMs
		}
	}

	if stat.InferenceDuractionMs > 0 {
		stat.AvgTokensPerSec = float64(stat.OutputTokens) / float64(stat.InferenceDuractionMs) * 1000.0
	}

	head.SequenceStat = stat
	messages[seqIdx] = head
}

// appendAssistantMsg saves an assistant message and recomputes SequenceStat on
// the sequence head (first assistant message after the last user message).
// Returns the index of the appended message in the messages slice.
func (m *Model) appendAssistantMsg(msg config.Message) int {
	m.session.appendMsg(msg)
	recomputeSequenceStats(m.session.file.Messages)
	return len(m.session.file.Messages) - 1
}

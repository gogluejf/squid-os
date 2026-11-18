package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/inputqueue"
	"squid-os/internal/ui"
)

// handleKey dispatches key events to the handler for the current mode.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {

	case ModeChat:
		return m.handleChatKey(msg)

	case ModeStreaming:
		return m.handleStreamingKey(msg)

	case ModeHelp:
		if key.Matches(msg, keys.Help) || key.Matches(msg, keys.Cancel) || key.Matches(msg, keys.Escape) {
			return m, m.setChatMode()
		}

	case ModeComponent:
		return m.handleComponent(msg)

	case ModeHistorySearch:
		return m.handleHistorySearchKey(msg)
	}

	return m, nil
}

// handleViewportScroll checks for scroll keys and applies them to the viewport.
// Returns true if a scroll key was matched and handled.
func (m *Model) handleViewportScroll(msg tea.KeyMsg) bool {
	switch {
	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return true
	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return true
	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return true
	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return true
	}
	return false
}

// handleComponent delegates all key handling to the active component
// (Picker or Prompt). Callbacks do the real work.
func (m Model) handleComponent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle viewport scroll keys first (these bypass the component).
	if m.handleViewportScroll(msg) {
		return m, nil
	}

	// Auth mode cycling works from any component overlay — allows switching
	// to auto mid-prompt without dismissing the dialog.
	if key.Matches(msg, keys.AuthMode) {
		return m.cycleAuthorization(false)
	}

	// Expand/collapse works during component overlays (e.g. auth questions).
	if key.Matches(msg, keys.Expand) {
		m.session.setExpanded(!m.session.expand.Global)
		m.refreshViewportAnchored(m.session.invalidateRenderAll)
		return m, nil
	}

	var cmds []tea.Cmd

	if m.activeComponent != nil {
		if cmd := m.activeComponent.HandleKey(msg, &m); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Do NOT call Update with key messages — non-key messages reach
		// Update() in update.go which forwards them to the component.
	}

	(&m).recalcLayout()
	return m, tea.Batch(cmds...)
}

// handleChatKey handles all key input while in the default chat mode.
func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Destroy):
		userText := m.session.destroyLastSequence()
		m.textarea.SetValue(userText)
		(&m).setNotification(ui.NotificationInfo, "last message removed  ·  ctrl+z to restore")
		m.autoSave()
		m.refreshViewportFollowing()
		return m, nil

	case key.Matches(msg, keys.UndoDestroy):
		if textarea, ok := m.session.undoDestroy(); ok {
			m.textarea.SetValue(textarea)
			remaining := len(m.session.undoStack)
			if remaining > 0 {
				(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("message restored  ·  %d more in buffer", remaining))
			} else {
				(&m).setNotification(ui.NotificationInfo, "message restored")
			}
			m.autoSave()
			m.refreshViewportFollowing()
		}
		return m, nil

	case key.Matches(msg, keys.Cancel):
		// Idle Ctrl+C follows the same precedence as busy: clear-draft first,
		// then flush pending queued input. A parked "1 next step" entry must
		// be undoable without restarting the app (design §3 rule 5 escape hatch).
		if m.textarea.Value() != "" {
			m.textarea.SetValue("")
			m.autoSizeTextarea()
			return m, nil
		}
		if n := (&m).flushQueues(); n > 0 {
			return m, nil
		}
		return m, nil

	case key.Matches(msg, keys.Yank):
		if m.killBuffer == "" {
			return m, nil
		}
		m.textarea.InsertString(m.killBuffer)
		m.autoSizeTextarea()
		return m, nil

	case key.Matches(msg, keys.Escape):
		if completion, ok := m.activeCapabilityCompletion(); ok {
			m.completionDismissed = completion.key()
			m.clearCapabilitySelection()
			m.recalcLayout()
			return m, nil
		}
		// Esc exits the app completely.
		return m, m.quitCmd

	case key.Matches(msg, keys.Expand):
		m.session.setExpanded(!m.session.expand.Global)
		m.refreshViewportAnchored(m.session.invalidateRenderAll)
		return m, nil

	case key.Matches(msg, keys.Incognito):
		return m.toggleIncognito()

	case key.Matches(msg, keys.HistorySearch):
		return m.startHistorySearch()

	case key.Matches(msg, keys.Paste) || msg.Type == tea.KeyInsert:
		return m.handlePaste()

	case key.Matches(msg, keys.Send):
		if m.isBusy() {
			// Busy: Enter queues the draft as a next-step steer. The draft is
			// cleared only after successful admission; rejection keeps it.
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			(&m).admitQueuedInput(text, inputqueue.BoundaryStep)
			m.refreshViewportFollowing()
			return m, nil
		}
		if completion, ok := m.activeCapabilityCompletion(); ok {
			_, selected := m.selectedCapabilityCandidate(completion)
			if selected || len(completion.candidates) == 1 {
				updated, _ := (&m).applyCapabilityCompletion(completion)
				m = *updated.(*Model)
			}
		}
		return m.sendMessage()

	case m.handleViewportScroll(msg):
		return m, nil

	case matchCommandKey(msg) != "":
		return m.executeCommandByName(matchCommandKey(msg))

	case msg.Alt && msg.Type == tea.KeyEnter:
		m.textarea.InsertRune('\n')
		m.autoSizeTextarea()
		return m, nil

	case key.Matches(msg, keys.QueueAfterTurn):
		// Alt+Q: idle → submit normally (raw draft, like Enter); busy → queue
		// after the logical turn (trimmed, as all queued text is).
		if m.textarea.Value() == "" {
			return m, nil
		}
		if m.isBusy() {
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			(&m).admitQueuedInput(text, inputqueue.BoundaryTurn)
			m.refreshViewportFollowing()
			return m, nil
		}
		return m.sendMessage()

	case msg.Type == tea.KeyTab && !msg.Alt:
		if completion, ok := m.activeCapabilityCompletion(); ok {
			return (&m).applyCapabilityCompletion(completion)
		}
		return m.applyListify()

	case key.Matches(msg, keys.Left):
		if (&m).moveCapabilitySelection(-1) {
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case key.Matches(msg, keys.Right):
		if (&m).moveCapabilitySelection(1) {
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case key.Matches(msg, keys.Up):
		// Only browse history if cursor is on the first line of the textarea
		if m.textarea.Line() > 0 {
			// Not on first line: let textarea handle it (move cursor up)
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		return m.historyUp()

	case key.Matches(msg, keys.Down):
		// Only browse history if cursor is on the last line of the textarea
		if m.textarea.Line() < m.textarea.LineCount()-1 {
			// Not on last line: let textarea handle it (move cursor down)
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		return m.historyDown()

	default:
		_, hadCapabilitySuggestion := m.activeCapabilityCompletion()
		if m.completionSelectKey != "" {
			m.clearCapabilitySelection()
		}
		// Capture text removed by kill bindings (ctrl+k, ctrl+u, ctrl+w, alt+d)
		// into the kill ring so ctrl+y can restore it.
		before := m.textarea.Value()
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		if killed := diffKilled(before, m.textarea.Value()); killed != "" {
			m.killBuffer = killed
		}
		// Soft wrapping can change visual height without changing logical lines.
		m.autoSizeTextarea()
		// Editing or moving away naturally re-evaluates a dismissed suggestion.
		if m.completionDismissed != "" {
			if completion, ok := m.capabilityCompletionAtCursor(); !ok || completion.key() != m.completionDismissed {
				m.completionDismissed = ""
			}
		}
		// Reset history navigation when user starts typing (not cursor movement)
		if m.historyIdx != -1 && !key.Matches(msg, keys.Left) && !key.Matches(msg, keys.Right) {
			m.draft = ""
			m.historyIdx = -1
		}
		_, hasCapabilitySuggestion := m.activeCapabilityCompletion()
		if hadCapabilitySuggestion != hasCapabilitySuggestion {
			m.recalcLayout()
		}
		// Only trigger command palette when "/" is the very first character (textarea was empty).
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '/' && m.textarea.Value() == "/" {
			m.openCommandPicker()
		}
		return m, cmd
	}
}

// handleStreamingKey handles key input while an inference stream or tool
// worker is active. The composer stays fully editable: ordinary typing edits
// the draft, Enter queues a next-step steer, Alt+Q queues after-turn, and
// Ctrl+C follows clear-draft → flush-queues → abort-turn precedence.
func (m Model) handleStreamingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		// Precedence 1: a nonempty draft is cleared, never aborted over.
		if m.textarea.Value() != "" {
			m.textarea.SetValue("")
			m.autoSizeTextarea()
			return m, nil
		}
		// Precedence 2: pending queued input is flushed (marked discarded).
		if n := (&m).flushQueues(); n > 0 {
			return m, nil
		}
		// Precedence 3: one cancel source — the turn context. Cancelling it
		// stops the tool worker loop (derived) and SIGKILLs in-flight children
		// (derived). Stream.Cancel covers pure inference with no tools running.
		m.session.CancelTurnCtx()
		m.session.Stream.Cancel("Stream aborted by user")
		return m, nil

	case key.Matches(msg, keys.Expand):
		m.session.setExpanded(!m.session.expand.Global)
		m.refreshViewportAnchored(m.session.invalidateRenderAll)
		return m, nil

	case matchCommandKey(msg) != "":
		return m.executeCommandByName(matchCommandKey(msg))

	case m.handleViewportScroll(msg):
		return m, nil

	case msg.Alt && msg.Type == tea.KeyEnter:
		// Alt+Enter keeps inserting a newline even while busy.
		m.textarea.InsertRune('\n')
		m.autoSizeTextarea()
		return m, nil

	case key.Matches(msg, keys.Send):
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		(&m).admitQueuedInput(text, inputqueue.BoundaryStep)
		m.refreshViewportFollowing()
		return m, nil

	case key.Matches(msg, keys.QueueAfterTurn):
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		(&m).admitQueuedInput(text, inputqueue.BoundaryTurn)
		m.refreshViewportFollowing()
		return m, nil

	case msg.Type == tea.KeyTab && !msg.Alt:
		// Tab still numberizes / moves list items while busy.
		return m.applyListify()

	default:
		// Everything else edits the draft exactly like idle chat mode.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.autoSizeTextarea()
		return m, cmd
	}
}

// isBusy reports whether an assistant response is in flight (streaming tokens,
// executing tools, or inside a turn that has not yet fully ended), i.e. keyboard
// and socket submissions must be queued instead of sent. turnActive covers the
// gap between "tool batch done" and "first token of next step".
func (m Model) isBusy() bool {
	return m.mode == ModeStreaming || m.session.Stream.Active || m.session.UIStream.ToolCh != nil || m.turnActive
}

// startHistorySearch enters history search mode and populates the overlay with prompt history.
func (m Model) startHistorySearch() (tea.Model, tea.Cmd) {
	// Save current textarea content to restore on escape
	m.draft = m.textarea.Value()

	m.mode = ModeHistorySearch
	m.historySearch = ui.NewHistorySearchOverlay(m.history.Entries)

	// Don't preview anything until user types at least one character
	return m, nil
}

// handleHistorySearchKey handles all key input while in history search mode.
func (m Model) handleHistorySearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		// Escape or Ctrl+C → restore original textarea content and exit search mode
		m.textarea.SetValue(m.draft)
		m.autoSizeTextarea()
		m.mode = ModeChat
		m.historySearch.Reset()
		return m, m.setChatMode()

	case key.Matches(msg, keys.HistorySearch), key.Matches(msg, keys.Up), key.Matches(msg, keys.Down):
		// Ctrl+R and Up move older through reverse-search results; Down moves newer.
		if key.Matches(msg, keys.Down) {
			m.historySearch.PrevMatch()
		} else {
			m.historySearch.NextMatch()
		}
		m.textarea.SetValue(m.historySearch.SelectedText())
		m.autoSizeTextarea()
		return m, nil

	case key.Matches(msg, keys.Send), key.Matches(msg, keys.Left), key.Matches(msg, keys.Right):
		// Enter, Left, or Right → confirm selection and keep text in textarea
		m.textarea.SetValue(m.historySearch.SelectedText())
		m.autoSizeTextarea()
		m.mode = ModeChat
		m.historySearch.Reset()
		return m, m.setChatMode()

	case msg.Type == tea.KeyBackspace:
		// Backspace → delete character from filter
		filter := m.historySearch.FilterText()
		if len(filter) > 0 {
			runes := []rune(filter)
			m.historySearch.Filter(string(runes[:len(runes)-1]))
			if m.historySearch.FilterText() == "" {
				m.textarea.SetValue(m.draft)
			} else {
				m.textarea.SetValue(m.historySearch.SelectedText())
			}
			m.autoSizeTextarea()
		}
		return m, nil

	default:
		// Handle character input for filter text
		switch msg.Type {
		case tea.KeyRunes:
			m.historySearch.Filter(m.historySearch.FilterText() + string(msg.Runes))
			m.textarea.SetValue(m.historySearch.SelectedText())
		case tea.KeySpace:
			m.historySearch.Filter(m.historySearch.FilterText() + " ")
			m.textarea.SetValue(m.historySearch.SelectedText())
		}
		m.autoSizeTextarea()
		return m, nil
	}
}

package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
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

	// Expand/collapse works during component overlays (e.g. auth questions).
	if key.Matches(msg, keys.Expand) {
		m.expanded = !m.expanded
		m.session.invalidateRenderAll()
		m.updateViewportContent()
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
		userText, userImage := m.session.destroyLastSequence()
		m.textarea.SetValue(userText)
		m.attachedImage = userImage
		(&m).setNotification(ui.NotificationInfo, "last message removed  ·  ctrl+u to restore")
		m.autoSave()
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.UndoDestroy):
		if textarea, image, ok := m.session.undoDestroy(); ok {
			m.textarea.SetValue(textarea)
			m.attachedImage = image
			remaining := len(m.session.undoStack)
			if remaining > 0 {
				(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("message restored  ·  %d more in buffer", remaining))
			} else {
				(&m).setNotification(ui.NotificationInfo, "message restored")
			}
			m.autoSave()
			m.updateViewportContent()
		}
		return m, nil

	case key.Matches(msg, keys.Cancel):
		if m.textarea.Value() != "" {
			m.textarea.SetValue("")
			m.autoSizeTextarea()
		} else {
			_ = config.SaveHistory(m.paths, m.history)
			return m, tea.Quit
		}
		return m, nil

	case key.Matches(msg, keys.Escape):
		return m, nil

	case key.Matches(msg, keys.Expand):
		m.expanded = !m.expanded
		m.session.invalidateRenderAll()
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.Incognito):
		return m.toggleIncognito()

	case key.Matches(msg, keys.HistorySearch):
		return m.startHistorySearch()

	case key.Matches(msg, keys.Send):
		return m.sendMessage()

	case m.handleViewportScroll(msg):
		return m, nil

	case matchCommandKey(msg) != "":
		return m.executeCommandByName(matchCommandKey(msg))

	case msg.Alt && msg.Type == tea.KeyEnter:
		m.textarea.InsertRune('\n')
		m.autoSizeTextarea()
		return m, nil

	case key.Matches(msg, keys.CycleSkill) && !msg.Alt:
		return m.cycleSkill()

	case msg.Type == tea.KeyTab && !msg.Alt:
		return m.applyListify()

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
		oldLines := m.textarea.LineCount()
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		// Resize if line count changed (e.g., backspace removing a line, Alt+Enter adding one)
		if m.textarea.LineCount() != oldLines {
			m.autoSizeTextarea()
		}
		// Reset history navigation when user starts typing (not cursor movement)
		if m.historyIdx != -1 && !key.Matches(msg, keys.Left) && !key.Matches(msg, keys.Right) {
			m.draft = ""
			m.historyIdx = -1
		}
		// Only trigger command palette when "/" is the very first character (textarea was empty).
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '/' && m.textarea.Value() == "/" {
			m.openCommandPicker()
		}
		return m, cmd
	}
}

// handleStreamingKey handles key input while an inference stream is active.
func (m Model) handleStreamingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		if m.stream.cancelFn != nil {
			m.stream.userCancelled = true
			m.stream.cancelFn()
		}
		return m, nil

	case key.Matches(msg, keys.Expand):
		m.expanded = !m.expanded
		m.session.invalidateRenderAll()
		m.updateViewportContent()
		return m, nil

	case matchCommandKey(msg) != "":
		return m.executeCommandByName(matchCommandKey(msg))

	case key.Matches(msg, keys.CycleSkill):
		return m.cycleSkill()

	case msg.Type == tea.KeyTab && !msg.Alt:
		return m.applyListify()

	case m.handleViewportScroll(msg):
		return m, nil
	}
	return m, nil
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
		// Ctrl+R and Up → previous match, Down → next match
		if key.Matches(msg, keys.Down) {
			m.historySearch.NextMatch()
		} else {
			m.historySearch.PrevMatch()
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

package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/config"
	"rig-chat/internal/ui"
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

	case ModeModelPicker:
		return m.handlePickerKey(msg, "model")

	case ModeSessionPicker:
		return m.handlePickerKey(msg, "session")

	case ModeFilePicker:
		return m.handlePickerKey(msg, m.filePickerFor)

	case ModeSavePrompt:
		return m.handleSavePromptKey(msg)

	case ModeHistorySearch:
		return m.handleHistorySearchKey(msg)
	}

	return m, nil
}

// handleChatKey handles all key input while in the default chat mode.
func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Destroy):
		userText, userImage := m.session.destroyLastPair()
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
		_ = config.SaveHistory(m.paths, m.history)
		return m, tea.Quit

	case key.Matches(msg, keys.Escape):
		if m.cmdPalette.Visible {
			m.cmdPalette.Visible = false
			m.recalcLayout()
		}
		return m, nil

	case key.Matches(msg, keys.Help):
		m.mode = ModeHelp
		return m, nil

	case key.Matches(msg, keys.ExpandThinking):
		m.thinkingExpanded = !m.thinkingExpanded
		m.session.invalidateRenderAll()
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.Save):
		return m.startManualSave()

	case key.Matches(msg, keys.Load):
		return m.startLoad()

	case key.Matches(msg, keys.Model):
		return m, m.scanModelsCmd()

	case key.Matches(msg, keys.NewSession):
		return m.clearSession()

	case key.Matches(msg, keys.Incognito):
		return m.toggleIncognito()

	case key.Matches(msg, keys.HistorySearch):
		return m.startHistorySearch()

	case msg.Alt && msg.Type == tea.KeyEnter:
		m.textarea.InsertRune('\n')
		return m, nil

	case key.Matches(msg, keys.Send):
		if m.cmdPalette.Visible && m.cmdPalette.SelectedCommand() != "" {
			return m.executeCommand(m.cmdPalette.SelectedCommand())
		}
		return m.sendMessage()

	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return m, nil

	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return m, nil

	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.cmdPalette.Visible {
			m.cmdPalette.MoveUp()
			return m, nil
		}
		return m.historyUp()

	case key.Matches(msg, keys.Down):
		if m.cmdPalette.Visible {
			m.cmdPalette.MoveDown()
			return m, nil
		}
		return m.historyDown()

	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		// Reset history navigation when user starts typing
		if m.historyIdx != -1 {
			m.draft = ""
			m.historyIdx = -1
		}
		m.updateCommandPalette()
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

	case key.Matches(msg, keys.ExpandThinking):
		m.thinkingExpanded = !m.thinkingExpanded
		m.session.invalidateRenderAll()
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return m, nil

	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return m, nil

	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return m, nil
	}
	return m, nil
}

// updateCommandPalette re-evaluates whether the command palette should be
// shown based on the current textarea content.
func (m *Model) updateCommandPalette() {
	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") {
		filter := val[1:]
		if filter != m.cmdPalette.Filter {
			m.cmdPalette.Filter = filter
			m.cmdPalette.Selected = 0
		}
		if len(m.cmdPalette.FilteredItems()) > 0 {
			m.cmdPalette.Visible = true
		} else {
			m.cmdPalette.Visible = false
		}
	} else {
		m.cmdPalette.Visible = false
	}
	m.recalcLayout()
}

// startHistorySearch enters history search mode and populates the overlay with prompt history.
func (m Model) startHistorySearch() (tea.Model, tea.Cmd) {
	// Save current textarea content to restore on escape
	m.draft = m.textarea.Value()

	m.mode = ModeHistorySearch
	m.historySearch = ui.NewHistorySearchOverlay()
	m.historySearch.Items = m.history.Entries
	m.historySearch.MatchIdx = 0

	// Don't preview anything until user types at least one character
	return m, nil
}

// handleHistorySearchKey handles all key input while in history search mode.
func (m Model) handleHistorySearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		// Escape or Ctrl+C → restore original textarea content and exit search mode
		m.textarea.SetValue(m.draft)
		m.mode = ModeChat
		m.historySearch.Reset()
		return m, m.setChatMode()

	case key.Matches(msg, keys.HistorySearch), key.Matches(msg, keys.Up), key.Matches(msg, keys.Down):
		// Ctrl+R, Up, or Down → cycle through matches (Up=prev, Down/ctrl+r=next)
		if key.Matches(msg, keys.Up) {
			m.historySearch.PrevMatch()
		} else {
			m.historySearch.NextMatch()
		}
		matches := m.historySearch.FilteredItems()
		if len(matches) > 0 {
			m.textarea.SetValue(matches[m.historySearch.MatchIdx])
		} else {
			m.textarea.SetValue("")
		}
		return m, nil

	case key.Matches(msg, keys.Send), key.Matches(msg, keys.Left), key.Matches(msg, keys.Right):
		// Enter, Left, or Right → confirm selection and keep text in textarea
		if item := m.historySearch.SelectedText(); item != "" {
			m.textarea.SetValue(item)
		}
		m.mode = ModeChat
		m.historySearch.Reset()
		return m, m.setChatMode()

	case msg.Type == tea.KeyBackspace:
		// Backspace → delete character from filter
		filter := m.historySearch.Filter
		if len(filter) > 0 {
			runes := []rune(filter)
			m.historySearch.Filter = string(runes[:len(runes)-1])
			m.historySearch.MatchIdx = 0
			// If filter is now empty, clear textarea
			if m.historySearch.Filter == "" {
				m.textarea.SetValue(m.draft)
			} else {

				matches := m.historySearch.FilteredItems()
				if len(matches) > 0 {
					m.textarea.SetValue(matches[0])
				} else {
					m.textarea.SetValue("")
				}
			}
		}
		return m, nil

	default:
		// Handle character input for filter text
		if msg.Type == tea.KeyRunes {
			m.historySearch.Filter += string(msg.Runes[0])
			m.historySearch.MatchIdx = 0
			matches := m.historySearch.FilteredItems()
			if len(matches) > 0 {
				m.textarea.SetValue(matches[0])
			} else {
				m.textarea.SetValue("")
			}
		}
		return m, nil
	}
}

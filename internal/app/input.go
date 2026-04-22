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
	m.mode = ModeHistorySearch
	m.historySearch = ui.NewHistorySearchOverlay()
	m.historySearch.Items = m.history.Entries
	m.historySearch.Visible = true
	m.recalcLayout()
	matches := len(m.historySearch.FilteredItems())
	if matches > 0 {
		(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("search prompt history, esc to exit [%d/%d]", 1, matches))
	} else {
		(&m).setNotification(ui.NotificationInfo, "search prompt history, esc to exit [0/0]")
	}
	return m, nil
}

// handleHistorySearchKey handles all key input while in history search mode.
func (m Model) handleHistorySearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		m.mode = ModeChat
		m.historySearch.Reset()
		m.recalcLayout()
		return m, nil

	case key.Matches(msg, keys.HistorySearch):
		// Ctrl+R → cycle to next match
		m.historySearch.NextMatch()
		matches := len(m.historySearch.FilteredItems())
		if matches > 0 {
			(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("search prompt history, esc to exit [%d/%d]", m.historySearch.MatchIdx+1, matches))
		} else {
			(&m).setNotification(ui.NotificationInfo, "search prompt history, esc to exit [0/0]")
		}
		return m, nil

	case key.Matches(msg, keys.Send):
		// Enter → confirm selection and insert text into textarea
		if item := m.historySearch.SelectedText(); item != "" {
			m.textarea.SetValue(item)
		}
		m.mode = ModeChat
		m.historySearch.Reset()
		m.recalcLayout()
		return m, nil

	default:
		// Handle character input for filter text
		if msg.Type == tea.KeyRunes {
			m.historySearch.Filter += string(msg.Runes[0])
			m.historySearch.MatchIdx = 0
			matches := len(m.historySearch.FilteredItems())
			if matches > 0 {
				(&m).setNotification(ui.NotificationInfo, fmt.Sprintf("search prompt history, esc to exit [%d/%d]", 1, matches))
			} else {
				(&m).setNotification(ui.NotificationInfo, "search prompt history, esc to exit [0/0]")
			}
		}
		return m, nil
	}
}

package app

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/config"
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
			m.mode = ModeChat
			m.textarea.Focus()
			return m, nil
		}

	case ModeModelPicker:
		return m.handlePickerKey(msg, "model")

	case ModeSessionPicker:
		return m.handlePickerKey(msg, "session")

	case ModeFilePicker:
		return m.handlePickerKey(msg, m.filePickerFor)

	case ModeSavePrompt:
		return m.handleSavePromptKey(msg)
	}

	return m, nil
}

// handleChatKey handles all key input while in the default chat mode.
func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
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
		m.toggleLastThinking()
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
		if m.cancelFn != nil {
			m.cancelFn()
		}
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "user" {
			m.messages = m.messages[:len(m.messages)-1]
		}
		m.streaming = false
		m.tokenCount = 0 // discard partial stream; don't pollute footer total
		m.mode = ModeChat
		m.textarea.Focus()
		m.streamText = ""
		m.streamThinking = ""
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.ExpandThinking):
		// Could toggle live thinking visibility in the future
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

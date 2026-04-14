package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/chat"
	"rig-chat/internal/ui"
)

// Update is the top-level Bubble Tea update function — routes every incoming
// message to the appropriate handler based on its type and current mode.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.recalcLayout()
		m.updateViewportContent()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.viewport.ScrollUp(3)
		case tea.MouseButtonWheelDown:
			m.viewport.ScrollDown(3)
		}
		return m, nil

	case streamTickMsg:
		if m.stream.active {
			m.updateViewportContent()
			return m, streamTickCmd()
		}
		return m, nil

	case streamEventMsg:
		return m.handleStreamEvent(chat.StreamEvent(msg))

	case modelsLoadedMsg:
		m.modelEntries = msg.models
		labels := make([]string, len(msg.models))
		for i, e := range msg.models {
			labels[i] = fmt.Sprintf("%-12s  %s", e.Provider, e.ID)
		}
		m.modelPicker = ui.NewPickerList("Select Model", labels)
		m.mode = ModeModelPicker
		m.recalcLayout()
		return m, nil
	}

	if m.mode == ModeChat {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// recalcLayout recomputes the viewport height based on current mode and terminal size.
func (m *Model) recalcLayout() {
	const inputHeight = 6
	const headerHeight = 1
	const footerHeight = 2

	overlayHeight := 0
	if m.cmdPalette.Visible {
		overlayHeight = m.cmdPalette.RenderHeight()
	} else {
		switch m.mode {
		case ModeModelPicker:
			overlayHeight = m.modelPicker.RenderHeight()
		case ModeSessionPicker:
			overlayHeight = m.sessionPicker.RenderHeight()
		case ModeFilePicker:
			overlayHeight = m.filePicker.RenderHeight()
		case ModeSavePrompt:
			overlayHeight = 2 // heading + name input line
		}
	}

	attachHeight := 0
	if m.attachedImage != "" {
		attachHeight = 1
	}
	vpHeight := m.height - inputHeight - headerHeight - footerHeight - attachHeight - overlayHeight
	if vpHeight < 3 {
		vpHeight = 3
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
	m.textarea.SetWidth(m.width)
}

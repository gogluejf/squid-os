package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
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
		m = m.onModelsLoaded(msg)
		return m, nil

	case contextRefreshMsg:
		// Silent background refresh — just update context window, don't change mode
		if len(msg.models) > 0 {
			m.modelEntries = msg.models
			(&m).refreshContextWindow(msg.models)
		}
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
	switch m.mode {
	case ModeComponent:
		if m.activePrompt != nil {
			overlayHeight = m.activePrompt.RenderHeight()
			m.activePrompt.Init()
		} else {
			overlayHeight = m.activePicker.RenderHeight()
			m.activePicker.Init(m) // resolve DefaultValue, fires OnSelectionChange (idempotent)
		}
	case ModeHistorySearch:
		overlayHeight = m.historySearch.RenderHeight()
	}

	const statusLineHeight = 1
	vpHeight := m.height - inputHeight - headerHeight - footerHeight - statusLineHeight - overlayHeight
	if vpHeight < 6 {
		vpHeight = 6
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
	m.textarea.SetWidth(m.width)
}

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
		m.refreshViewportFollowing()
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

	case uiTickMsg:
		m.refreshViewportFollowing()
		return m, uiTickCmd(m.session.Stream.Active)

	case streamEventMsg:
		return m.handleStreamEvent(chat.StreamEvent(msg))

	case pendingToolResumeMsg:
		m.session.UIStream.MsgIdx = msg.msgIdx
		return (&m).resumeToolExecution()

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

	// Non-key messages: update active component if present (e.g., blink ticks)
	if m.mode == ModeComponent && m.activeComponent != nil {
		if cmd := m.activeComponent.Update(msg, &m); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// recalcLayout recomputes the viewport height based on current mode and terminal size.
// It syncs the textarea's cached Height with its actual content so multi-line
// input (Alt+Enter) and restored text grow/shrink the textarea naturally.
func (m *Model) recalcLayout() {
	// Set width first because visual row count depends on the content width.
	m.textarea.SetWidth(m.width)
	actualRows := textareaVisualRows(m.textarea.Value(), m.textarea.Width())
	if actualRows < 2 {
		actualRows = 2
	}
	if actualRows > m.textarea.MaxHeight {
		actualRows = m.textarea.MaxHeight
	}
	m.textarea.SetHeight(actualRows)

	inputHeight := 2 + m.textarea.Height() // textarea itself + padding/border around it
	const headerHeight = 1
	const footerHeight = 2

	overlayHeight := 0
	switch m.mode {
	case ModeComponent:
		if m.activeComponent != nil {
			overlayHeight = m.activeComponent.RenderHeight()
			m.activeComponent.Init(m)
		}
	case ModeHistorySearch:
		overlayHeight = m.historySearch.RenderHeight()
	case ModeChat:
		if _, ok := m.activeCapabilityCompletion(); ok {
			overlayHeight = 1
		}
	}

	const statusLineHeight = 1
	vpHeight := m.height - inputHeight - headerHeight - footerHeight - statusLineHeight - overlayHeight
	if vpHeight < 6 {
		vpHeight = 6
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
}

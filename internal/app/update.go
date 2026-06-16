package app

import (
	"strings"

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
	// Sync textarea height to actual content (bubbles doesn't auto-shrink after Reset)
	actualLines := countLines(m.textarea.Value())
	m.textarea.SetHeight(actualLines)

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

// countLines returns the number of logical lines in a string (at least 1).
func countLines(s string) int {
	n := 1 + strings.Count(s, "\n")
	if n > 20 { // matches MaxHeight
		n = 20
	}
	return n
}

package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"rig-chat/internal/ui"
)

// View is the top-level Bubble Tea render function — assembles all visible
// sections into a single string for the terminal.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.mode == ModeHelp {
		return m.renderHelp()
	}

	var sections []string
	sections = append(sections, m.renderTopHeader())

	// Viewport (messages)
	sections = append(sections, m.viewport.View())

	// Command palette overlay (between viewport and input)
	if m.cmdPalette.Visible {
		sections = append(sections, m.cmdPalette.Render(m.width))
	} else {
		switch m.mode {
		case ModeModelPicker:
			sections = append(sections, m.modelPicker.Render(m.width))
		case ModeSessionPicker:
			sections = append(sections, m.sessionPicker.Render(m.width))
		case ModeFilePicker:
			sections = append(sections, m.filePicker.Render(m.width))
		case ModeSavePrompt:
			sections = append(sections, m.savePrompt.Render(m.width))
		}
	}

	// Attachment chip
	if m.attachedImage != "" {
		sections = append(sections, ui.AttachmentStyle.Render("  attached: "+m.attachedImage))
	}

	// Textarea
	sections = append(sections, m.textarea.View())

	// Footer
	footerData := ui.FooterData{
		Model:       m.settings.Model,
		Provider:    m.settings.Provider,
		TotalTokens: m.totalTokens + m.tokenCount,
		Streaming:   m.streaming,
		InThinking:  m.inThinking,
		TokPerSec:   m.calcTokPerSec(),
	}
	sections = append(sections, ui.RenderFooter(footerData, m.width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// updateViewportContent rebuilds the viewport content from all current messages
// plus any active streaming text, and scrolls to the bottom.
func (m *Model) updateViewportContent() {
	var b strings.Builder

	for _, msg := range m.messages {
		b.WriteString(ui.RenderMessage(msg, m.width, msg.ThinkingExpanded))
	}

	if m.streaming {
		b.WriteString(ui.RenderStreamingMessage(
			m.streamText,
			m.streamThinking,
			m.inThinking,
			m.width,
			m.streamStart,
			m.tokenCount,
			m.calcTokPerSec(),
		))
	}

	if m.lastError != "" {
		b.WriteString(ui.ErrorStyle.Render("Error: " + m.lastError))
		b.WriteString("\n")
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

// renderHelp delegates to the ui package to produce the full help screen.
func (m Model) renderHelp() string {
	return ui.RenderHelp(m.width, m.height)
}

// renderTopHeader renders the top bar, including the incognito indicator when active.
func (m Model) renderTopHeader() string {
	if !m.incognito {
		return ui.TopHeaderStyle.Width(m.width).Render("rig-chat v0.1")
	}
	headerStyle := ui.IncognitoHeaderStyle.Width(m.width)
	title := "rig-chat v0.1"
	label := "👻 incognito"
	titleWidth := lipgloss.Width(ui.IncognitoHeaderStyle.Render(title))
	labelWidth := lipgloss.Width(ui.IncognitoHeaderStyle.Render(label))
	gap := m.width - titleWidth - labelWidth
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Render(title + strings.Repeat(" ", gap) + label)
}

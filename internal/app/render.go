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
	sections = append(sections, ui.RenderHeader(ui.HeaderData{Incognito: m.incognito}, m.width))

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
		TotalTokens: m.session.totalTokens() + m.stream.outputTokenCount + m.stream.thinkingTokenCount,
		Streaming:   m.stream.active,
		InThinking:  m.stream.inThinking,
		TokPerSec:   m.calcTokPerSec(),
	}
	sections = append(sections, ui.RenderFooter(footerData, m.width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// updateViewportContent rebuilds the viewport content from all current messages
// plus any active streaming text, and scrolls to the bottom.
func (m *Model) updateViewportContent() {
	var b strings.Builder

	// Invalidate cache on width change
	if m.session.renderedWidth != m.width {
		m.session.invalidateRenderAll()
		m.session.renderedWidth = m.width
	}
	// Render only new messages, reuse cache for existing ones
	for i := len(m.session.renderedMessages); i < len(m.session.file.Messages); i++ {
		msg := m.session.file.Messages[i]
		m.session.renderedMessages = append(m.session.renderedMessages, ui.RenderMessage(msg, m.width, m.thinkingExpanded))
	}
	for _, r := range m.session.renderedMessages {
		b.WriteString(r)
	}

	if m.stream.active {
		// Only re-run glamour when a new line has completed (lastNL changed).
		lastNL := strings.LastIndex(m.stream.text, "\n")
		if lastNL > m.stream.markdownEnd || (lastNL < 0 && m.stream.markdown != "") {
			if lastNL >= 0 {
				m.stream.markdown = strings.TrimRight(
					ui.RenderMarkdownOnBg(m.stream.text[:lastNL], "233"), "\n")
				m.stream.markdownEnd = lastNL
			} else {
				m.stream.markdown = ""
				m.stream.markdownEnd = -1
			}
		}
		partial := m.stream.text
		if lastNL >= 0 {
			partial = m.stream.text[lastNL+1:]
		}
		b.WriteString(ui.RenderStreamingMessage(
			m.stream.markdown,
			partial,
			m.stream.thinking,
			m.stream.inThinking,
			m.width,
			m.stream.start,
			m.stream.outputTokenCount,
			m.calcTokPerSec(),
			m.thinkingExpanded,
			m.stream.thinkingTokenCount,
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

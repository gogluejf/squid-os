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
		case ModeHistorySearch:
			sections = append(sections, m.historySearch.Render(m.width))
		}
	}

	// Status line: notification (left) + attachment chip (right)
	// Skip notification when in history search mode (the search overlay replaces it)
	if m.mode != ModeHistorySearch {
		attachChip := ""
		if m.attachedImage != "" {
			attachChip = ui.AttachmentStyle.Render("attached: " + m.attachedImage)
		}
		sections = append(sections, ui.RenderStatusLine(m.notification, attachChip, m.width))
	}

	// Textarea
	sections = append(sections, m.textarea.View())

	// Footer: context window = all saved messages + current inference
	sections = append(sections, ui.RenderFooter(m.buildFooterData(), m.width))

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
		m.session.renderedMessages = append(m.session.renderedMessages, ui.RenderMessage(msg, m.width, m.expanded))
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
		b.WriteString(ui.RenderStreamingMessage(ui.StreamingViewData{
			RenderedMarkdown: m.stream.markdown,
			Partial:          partial,
			ThinkingText:     m.stream.thinking,
			InThinking:       m.stream.inThinking,
			Width:            m.width,
			Expanded:         m.expanded,
			RequestStart:     m.stream.metrics.Start,
			ThinkingTokens:   m.stream.metrics.ThinkingTokens(),
			ThinkingDur:      m.stream.metrics.ThinkingDuration(),
			TextTokens:       m.stream.metrics.TextTokens(),
			TextDur:          m.stream.metrics.TextDuration(),
			TokPerSec:        m.stream.metrics.AvgTokenPerSec(),
			Waiting:          !m.stream.metrics.HasFirstToken(),
		}))
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

// buildFooterData assembles the dynamic footer data.
func (m Model) buildFooterData() ui.FooterData {
	return ui.FooterData{
		Model:       m.settings.Model,
		Provider:    m.settings.Provider,
		TotalTokens: m.session.totalTokens() + m.stream.metrics.TotalTokens(),
		Streaming:   m.stream.active,
		TokPerSec:   m.stream.metrics.AvgTokenPerSec(),
	}
}

// renderHelp delegates to the ui package to produce the full help screen.
func (m Model) renderHelp() string {
	return ui.RenderHelp(m.width, m.height)
}

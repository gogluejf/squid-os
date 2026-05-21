package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/config"
	"squid-os/internal/style"
	"squid-os/internal/ui"
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
			attachChip = style.AttachmentStyle.Render("attached: " + m.attachedImage)
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

	var liveSeqStat *config.SequenceStat
	var liveSeqStatID string
	if m.stream.active {
		liveSeqStat, liveSeqStatID = m.buildLiveSeqStat()
	}

	//this is needed to have a black first line before rendering message
	b.WriteString(style.StatusLineStyle.Width(m.width).Render(""))

	for i, rendered := range m.session.renderedMessages {
		msg := m.session.file.Messages[i]
		if msg.Role == "assistant" && msg.SequenceStat != nil {
			stat := msg.SequenceStat
			if msg.ID == liveSeqStatID {
				stat = liveSeqStat
				liveSeqStat = nil
				liveSeqStatID = ""
			}
			b.WriteString(ui.RenderAssistantHeader(msg.CreatedAt, stat, m.width))
		}
		b.WriteString(rendered)
	}

	if m.stream.active {
		if liveSeqStat != nil && liveSeqStatID == "" {
			// First of sequence — no saved assistant message yet
			b.WriteString(ui.RenderAssistantHeader(m.stream.metrics.Start, liveSeqStat, m.width))
		}
		// Only re-run glamour when a new line has completed (lastNL changed).
		lastNL := strings.LastIndex(m.stream.text, "\n")
		if lastNL > m.stream.markdownEnd || (lastNL < 0 && m.stream.markdown != "") {
			if lastNL >= 0 {
				m.stream.markdown = strings.TrimRight(
					ui.RenderMarkdownOnBg(m.stream.text[:lastNL], style.P.BgApp, style.CanvasContentWidth(m.width)), "\n")
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
			PendingTools:     m.stream.toStreamingToolCalls(),
		}))
	}

	// Show squid art when no user messages have been sent yet
	if !m.session.hasUserMessage() && !m.stream.active {
		existingRows := strings.Count(b.String(), "\n")
		b.WriteString(ui.RenderSquidArt(m.width, m.viewport.Height, existingRows))
	}

	// Ensure the content fills the entire viewport height with BgApp so
	// no terminal background bleeds through when there is little content.
	content := b.String()
	// Number of lines is newlines+1 (the last line doesn't end with \n)
	contentLines := strings.Count(content, "\n") + 1
	bgLine := lipgloss.NewStyle().
		Background(lipgloss.Color(style.P.BgApp)).
		Render(strings.Repeat(" ", m.width))
	for contentLines < m.viewport.Height {
		b.WriteString("\n" + bgLine)
		contentLines++
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

// buildFooterData assembles the dynamic footer data.
func (m Model) buildFooterData() ui.FooterData {
	return ui.FooterData{
		Model:            modelBasename(m.settings.Model),
		Provider:         m.settings.Provider,
		TotalTokens:      m.session.totalTokens() + m.stream.metrics.TotalOutputTokens(),
		TotalInputTokens: m.session.totalInputTokens(),
		TotalOutTokens:   m.session.totalOutputTokens() + m.stream.metrics.TotalOutputTokens(),
		Streaming:        m.stream.active,
		ThinkingOn:       m.settings.Thinking,
		TokPerSec:        m.stream.metrics.AvgTokenPerSec(),
		ContextWindow:    m.settings.ContextWindow,
		CurrentDir:       m.currentDir,
		IsGitRepo:        hasGit(m.currentDir),
	}
}

// renderHelp delegates to the ui package to produce the full help screen.
func (m Model) renderHelp() string {
	return ui.RenderHelp(m.width, m.height)
}

// buildLiveSeqStat returns a SequenceStat for the active stream and the ID of
// the message it belongs to. Returns ("", stat) when there is no saved message
// yet (the stream is the first assistant message of the sequence).
func (m *Model) buildLiveSeqStat() (*config.SequenceStat, string) {
	live := &config.SequenceStat{
		OutputTokens:         m.stream.metrics.TotalOutputTokens(),
		DurationMs:           m.stream.metrics.Duration().Milliseconds(),
		InferenceDuractionMs: (m.stream.metrics.TextDuration() + m.stream.metrics.ThinkingDuration() + m.stream.metrics.ToolCallDuration()).Milliseconds(),
		AvgTokensPerSec:      m.stream.metrics.AvgTokenPerSec(),
	}

	seqIdx := config.FindSequenceHeadIdx(m.session.file.Messages)
	if seqIdx == -1 {
		return live, ""
	}

	base := *m.session.file.Messages[seqIdx].SequenceStat
	base.Add(live)
	return &base, m.session.file.Messages[seqIdx].ID
}

func hasGit(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

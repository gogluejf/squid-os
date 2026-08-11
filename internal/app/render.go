package app

import (
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/config"
	"squid-os/internal/style"
	"squid-os/internal/ui"
)

// jitterTokenRate returns a slightly animated token rate during stalls.
// If tokens are flowing (last activity < 100ms ago), returns the real value.
// If stalled, randomizes the first decimal (±0.3 of the value, min ±0.1, max ±0.9) for visual liveliness.
// Only applied while stream is active.
func jitterTokenRate(tps float64, lastActivity time.Time, active bool) float64 {
	if tps <= 0 || !active {
		return tps
	}
	if time.Since(lastActivity) < 100*time.Millisecond {
		return tps
	}
	rounded := math.Floor(tps*10) / 10
	// Jitter range: 30% of the value, clamped between 0.1 and 0.9
	jitterRange := rounded * 0.3
	if jitterRange < 0.1 {
		jitterRange = 0.1
	}
	if jitterRange > 0.9 {
		jitterRange = 0.9
	}
	jitter := (rand.Float64()*2 - 1) * jitterRange
	return rounded + jitter
}

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
	sections = append(sections, ui.RenderHeader(ui.HeaderData{
		Incognito: m.incognito,
		Session:   m.session.Info,
		Agent:     m.session.Doc.Config.AgentName,
	}, m.width))

	// Viewport (messages)
	sections = append(sections, m.viewport.View())

	// Component overlay (between viewport and input)
	switch m.mode {
	case ModeComponent:
		if m.activeComponent != nil {
			sections = append(sections, m.activeComponent.Render(m.width))
		}
	case ModeHistorySearch:
		sections = append(sections, m.historySearch.Render(m.width))
	case ModeChat:
		if suggestion := m.renderCapabilitySuggestion(); suggestion != "" {
			sections = append(sections, suggestion)
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

	// Textarea (shown for all modes except component overlay which replaces it)
	sections = append(sections, m.textarea.View())

	// Footer: context window = all saved messages + current inference
	sections = append(sections, ui.RenderFooter(m.buildFooterData(), m.width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// updateViewportContent rebuilds the viewport content from all current messages
// plus any active streaming text, and scrolls to the bottom.
// Always syncs textarea height first so layout reflects actual content.
func (m *Model) updateViewportContent() {
	m.recalcLayout()

	var b strings.Builder

	// Invalidate cache on width change
	if m.session.renderedWidth != m.width {
		m.session.invalidateRenderAll()
		m.session.renderedWidth = m.width
	}
	// Render only new messages, reuse cache for existing ones
	for i := len(m.session.renderedMessages); i < len(m.session.Doc.Messages); i++ {
		msg := m.session.Doc.Messages[i]
		m.session.renderedMessages = append(m.session.renderedMessages, ui.RenderMessage(msg, m.width, m.expanded))
	}

	var liveSeqStat *config.SequenceStat
	var liveSeqStatID string
	if m.session.Stream.Active {
		liveSeqStat, liveSeqStatID = m.buildLiveSeqStat()
	}

	//this is needed to have a black first line before rendering message
	b.WriteString(style.StatusLineStyle.Width(m.width).Render(""))

	for i, rendered := range m.session.renderedMessages {
		msg := m.session.Doc.Messages[i]
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

	if m.session.Stream.Active {
		if liveSeqStat != nil && liveSeqStatID == "" {
			// First of sequence — no saved assistant message yet
			b.WriteString(ui.RenderAssistantHeader(m.session.Stream.Metrics.Start, liveSeqStat, m.width))
		}
		// Only re-run glamour when a new line has completed (lastNL changed).
		lastNL := strings.LastIndex(m.session.Stream.Text, "\n")
		if lastNL > m.session.UIStream.MarkdownEnd || (lastNL < 0 && m.session.UIStream.Markdown != "") {
			if lastNL >= 0 {
				m.session.UIStream.Markdown = strings.TrimRight(
					ui.RenderMarkdownOnBg(m.session.Stream.Text[:lastNL], style.P.BgApp, style.CanvasContentWidth(m.width)), "\n")
				m.session.UIStream.MarkdownEnd = lastNL
			} else {
				m.session.UIStream.Markdown = ""
				m.session.UIStream.MarkdownEnd = -1
			}
		}
		partial := m.session.Stream.Text
		if lastNL >= 0 {
			partial = m.session.Stream.Text[lastNL+1:]
		}

		b.WriteString(ui.RenderStreamingMessage(ui.StreamingViewData{
			RenderedMarkdown: m.session.UIStream.Markdown,
			Partial:          partial,
			ThinkingText:     m.session.Stream.Thinking,
			InThinking:       m.session.Stream.InThinking,
			Width:            m.width,
			Expanded:         m.expanded,
			RequestStart:     m.session.Stream.Metrics.Start,
			ThinkingTokens:   m.session.Stream.Metrics.ThinkingTokens(),
			ThinkingDur:      m.session.Stream.Metrics.ThinkingDuration(),
			TextTokens:       m.session.Stream.Metrics.TextTokens(),
			TextDur:          m.session.Stream.Metrics.TextDuration(),
			Waiting:          !m.session.Stream.Metrics.HasFirstToken(),
			PendingTools:     streamingToolCalls(m.session.Stream.PartialTools),
		}))
	}

	// Show squid art when no user messages have been sent yet.
	// Skip during component overlays — viewport is reduced, art would look wrong.
	if !m.session.HasUserMessage() && !m.session.Stream.Active && m.mode != ModeComponent {
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

// buildFooterData assembles footer data from the live TokenTally.
func (m Model) buildFooterData() ui.FooterData {
	streamOut := m.session.Stream.Metrics.TotalOutputTokens()

	tally := m.session.Doc.TokenTally
	if tally == nil {
		tally = &config.TokenTally{}
	}

	inference := m.session.EffectiveInference()
	return ui.FooterData{
		Model:                 modelBasename(inference.Model),
		Provider:              inference.Provider,
		ContextInputTokens:    tally.Context.CompactedInput,
		ContextOutputTokens:   tally.Context.CompactedOutput,
		ContextTotalTokens:    tally.Context.Compacted,
		SavedContextTokens:    tally.Context.Saved,
		ContextCompaction:     m.session.Doc.Config.ContextCompaction,
		StreamingOutputTokens: streamOut,
		Streaming:             m.session.Stream.Active,
		ThinkingOn:            inference.Thinking.Enabled,
		AuthorizationMode:     string(m.session.Doc.Config.AuthMode),
		TokPerSec:             jitterTokenRate(m.session.Stream.Metrics.AvgTokenPerSec(), m.session.Stream.Metrics.LastActivity(), m.session.Stream.Active),
		SeqDurMs: func() int64 {
			if m.session.Stream.Active {
				ss, _ := m.buildLiveSeqStat()
				return ss.DurationMs
			} else {
				return 0
			}
		}(),
		ContextWindow: m.settings.ContextWindow,
		WorkingDir:    m.session.Doc.Config.WorkingDir,
		Skill:         m.session.EffectiveSkill(),
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
		OutputTokens:         m.session.Stream.Metrics.TotalOutputTokens(),
		DurationMs:           m.session.UIStream.Stopwatch.Elapsed().Milliseconds(),
		InferenceDuractionMs: m.session.Stream.Metrics.InferenceDuration().Milliseconds(),
		AvgTokensPerSec:      jitterTokenRate(m.session.Stream.Metrics.AvgTokenPerSec(), m.session.Stream.Metrics.LastActivity(), m.session.Stream.Active),
	}

	seqIdx := config.FindSequenceHeadIdx(m.session.Doc.Messages)
	if seqIdx == -1 {
		return live, ""
	}

	base := *m.session.Doc.Messages[seqIdx].SequenceStat
	base.Add(live)
	return &base, m.session.Doc.Messages[seqIdx].ID
}

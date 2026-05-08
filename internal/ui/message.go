package ui

import (
	"fmt"
	"strings"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/tools"

	"github.com/charmbracelet/lipgloss"
)

// DrawCanvas renders a single styled block (canvas, toolbox, userbox).
//
//   - parts:  pre-styled title segments rendered as "↳ part0 · part1 · ..."
//     the "↳ " prefix and " · " separators carry the box bg color.
//     pass nil/empty for no title line.
//   - content: body blocks joined with "\n\n".  Each element can be
//     pre-styled (ANSI) or plain text.  Pass nil/empty for no body.
//   - fg / bg: foreground and background color names (lipgloss color spec).
//   - topGap:  number of leading blank rows (0, 1, or 2) before the first line.
//     Canvas blocks use 1, tool boxes use 2 to match legacy spacing.
//   - width:   total rendered width (includes margins + padding).
//
// Trailing spacing: one blank row after content, then MarginBottom=1 (bg-colored).
// Callers never concatenate newlines manually.
func DrawCanvas(parts []string, content []string, fg, bg string, topGap int, width int, marginBottom int) string {

	st := lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Margin(0, BoxMargin, marginBottom, BoxMargin).
		MarginBackground(lipgloss.Color(P.BgApp)).
		Padding(0, 2).
		Width(width)

	var b strings.Builder
	for i := 0; i < topGap; i++ {
		b.WriteByte('\n')
	}

	if len(parts) > 0 {

		sep := lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg)).Render(" · ")
		arrow := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render("↳ ")

		b.WriteString(arrow)
		b.WriteString(parts[0])
		for i := 1; i < len(parts); i++ {
			b.WriteString(sep)
			b.WriteString(parts[i])
		}
	}

	if len(content) > 0 {
		if len(parts) > 0 {
			b.WriteString("\n\n")
		} else if topGap < 1 {
			// No parts and no leading gap → add one blank row before content
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(content, "\n\n"))
	}

	b.WriteByte('\n')

	return st.Render(b.String())
}

// drawCanvasSpan is a convenience for full-canvas blocks (BgApp background).
// topGap=1 matches the legacy CanvasSpan one leading blank row.
func drawCanvasSpan(parts []string, content []string, fg string, width int) string {
	return DrawCanvas(parts, content, fg, P.BgApp, 1, width, 0)
}

// drawToolBox is a convenience for tool call blocks (BgCode background).
// topGap=2 matches the legacy ToolBox "\n\n" two leading blank rows.
func drawToolBox(parts []string, content []string, fg string, boxWidth int) string {
	return DrawCanvas(parts, content, fg, P.BgCode, 2, boxWidth, 1)
}

// drawUserBox is a convenience for user message blocks (BgUser background).
// topGap=1 matches the legacy UserBox MarginTop=1 (one BgApp row).
func drawUserBox(parts []string, content []string, fg string, boxWidth int) string {
	return DrawCanvas(parts, content, fg, P.BgUser, 1, boxWidth, 1)
}

// RenderMessage dispatches to the correct renderer by role.
func RenderMessage(msg config.Message, width int, expanded bool) string {
	switch msg.Role {
	case config.RoleSystem:
		return renderSystemMessage(msg, width, expanded)
	case config.RoleInternal:
		return renderInternalMessage(msg, width, expanded)
	case config.RoleSynthetic:
		return renderSyntheticMessage(msg, width, expanded)
	case config.RoleUser:
		return renderUserMessage(msg, width)
	case config.RoleAssistant:
		return renderAssistantMessage(msg, width, expanded)
	default:
		panic(fmt.Sprintf("unknown message role: %s", msg.Role))
	}
}

// renderSystemMessage renders a system prompt message (role = system).
// Expandable like thinking/tool. Label color 141, content muted.
func renderSystemMessage(msg config.Message, width int, expanded bool) string {
	parts := []string{
		SystemLabel.Render(msg.Label),
		CanvasStatInline.Render(tokenChipInput(msg.InputTokens, nil)),
	}

	var content []string
	if expanded && msg.Text != "" {
		content = []string{msg.Text}
	}
	return drawCanvasSpan(parts, content, P.TextMuted, width)
}

// renderInternalMessage renders an internal metadata message (role = internal).
// Expandable. Label color 39 (teal), content muted. No tokens (except tools def).
func renderInternalMessage(msg config.Message, width int, expanded bool) string {
	parts := []string{
		InternalMsgLabel.Render(msg.Label),
	}
	if msg.InputTokens > 0 {
		parts = append(parts, CanvasStatInline.Render(tokenChipInput(msg.InputTokens, nil)))
	}
	var content []string
	if expanded && msg.Text != "" {
		content = []string{msg.Text}
	}
	return drawCanvasSpan(parts, content, P.TextMuted, width)
}

// renderSyntheticMessage renders a synthetic message (e.g. stream aborted, error)
// as a canvas span. When collapsed, shows only the label; when expanded, shows the body too.
func renderSyntheticMessage(msg config.Message, width int, expanded bool) string {
	parts := []string{
		InternalLabel.Render(msg.Label),
		CanvasStatInline.Render(tokenChipOutput(msg.TextMetrics.Tokens, nil)),
	}
	var content []string
	if expanded && msg.Text != "" {
		content = []string{msg.Text}
	}
	return drawCanvasSpan(parts, content, P.TextMuted, width)
}

// renderUserMessage renders a user message as a single UserBox containing
// the header line + body text.  The header is content inside the box
// (not a DrawCanvas title part) since it has no ↳ prefix.
func renderUserMessage(msg config.Message, width int) string {
	boxWidth := BoxWidth(width)
	inner := ContentWidth(width)

	leftStr := UserHeaderDimStyle.Render(msg.CreatedAt.Format("15:04:05"))
	var right []string
	if msg.ImagePath != "" {
		right = append(right, UserHeaderAttStyle.Render(msg.ImagePath))
	}
	if msg.InputTokens > 0 {
		right = append(right, UserHeaderDimStyle.Render(tokenChipInput(msg.InputTokens, nil)))
	}
	rightStr := strings.Join(right, UserHeaderDimStyle.Render("  "))
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	headerLine := leftStr + UserHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr

	return drawUserBox(nil, []string{"\n" + headerLine, msg.Text}, P.TextPrimary, boxWidth)
}

// RenderAssistantHeader emits the assistant header as a bare canvas line
// (not a box).  Stays uncached: SequenceStat mutates while a stream is live.
func RenderAssistantHeader(start time.Time, stat *config.SequenceStat, width int) string {
	inner := CanvasContentWidth(width)
	leftStr := AssistantHeaderDimStyle.Render(start.Format("15:04:05"))
	rightStr := renderSeqStatRight(stat)
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	line := leftStr + AssistantHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr
	return CanvasSpan.Width(width).Render("\n" + line)
}

// renderAssistantMessage renders an assistant message as canvas spans
// (thinking, text body) followed by one ToolBox per tool call.
func renderAssistantMessage(msg config.Message, width int, expanded bool) string {
	var b strings.Builder
	boxWidth := BoxWidth(width)

	if msg.ThinkingText != "" {
		parts := []string{
			ThinkingLabel.Render("thinking"),
			CanvasStatInline.Render(tokenChipOutput(msg.ThinkingMetrics.Tokens, &msg.ThinkingMetrics.InferenceDuractionMs)),
		}
		var content []string
		if expanded {
			content = []string{msg.ThinkingText}
		}
		b.WriteString(drawCanvasSpan(parts, content, P.TextMuted, width))
	}

	if msg.Text != "" && msg.Text != "\n\n" {
		body := RenderMarkdownOnBg(msg.Text, P.BgApp, CanvasContentWidth(width)) + "\n"
		b.WriteString(drawCanvasSpan(nil, []string{body}, P.TextPrimary, width))
	}

	if len(msg.ToolCalls) > 0 {
		b.WriteString(renderToolCallsInline(msg.ToolCalls, boxWidth, expanded, tools.GetRegistry()))
	}

	return b.String()
}

// renderToolCallsInline renders one ToolBox per tool call. When expanded,
// the box contains the label line plus arguments and result/error stacked
// inside the same box (separated by "\n").
func renderToolCallsInline(toolCalls []config.ToolCallEntry, boxWidth int, expanded bool, reg *tools.Registry) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		namePart, paramPart := formatToolDisplay(tc.Instruction.Name, tc.Instruction.Arguments, reg)

		var parts []string
		parts = append(parts, ToolLabel.Render(namePart))
		if paramPart != "" {
			parts = append(parts, ToolParamOnTool.Render(paramPart))
		}

		// Status marker as its own part
		switch tc.Execution.Status {
		case "error":
			parts = append(parts, ToolErrOnTool.Render("[✗]"))
		case "success":
			parts = append(parts, ToolCheckOnTool.Render("[✓]"))
		}

		stats := tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs)
		if stats != "" {
			parts = append(parts, ToolStatOnTool.Render(stats))
		}

		// Build content blocks
		var content []string
		if expanded {
			if tc.Instruction.Arguments != "" {
				content = append(content, stripNewlines(tc.Instruction.Arguments))
			}
			switch tc.Execution.Status {
			case "error":
				if tc.Execution.Error != "" {
					content = append(content, ToolErrorOnTool.Render(tc.Execution.Error))
				}
				if tc.Execution.Result != "" {
					content = append(content, "Result:\n"+tc.Execution.Result)
				}
			case "success":
				if tc.Execution.Result != "" {
					content = append(content, tc.Execution.Result)
				}
			}
		}

		b.WriteString(drawToolBox(parts, content, P.TextDim, boxWidth))
	}
	return b.String()
}

// StreamingViewData holds all data needed to render a streaming message.
type StreamingViewData struct {
	RenderedMarkdown string
	Partial          string
	ThinkingText     string
	InThinking       bool
	Width            int
	Expanded         bool

	// Timing
	RequestStart   time.Time
	ThinkingTokens int
	ThinkingDur    time.Duration
	TextTokens     int
	TextDur        time.Duration
	TokPerSec      float64
	Waiting        bool // true when no first token has arrived yet

	// Pending tool calls (streaming, before execution)
	PendingTools []StreamingToolCall
}

// StreamingToolCall holds the display-relevant fields of a pending tool call.
type StreamingToolCall struct {
	Name      string
	Arguments string
	Tokens    int           // aggregate from metrics.ToolCallTokens()
	Duration  time.Duration // aggregate from metrics.ToolCallDuration()
}

// RenderStreamingMessage renders the in-progress streaming message.
func RenderStreamingMessage(data StreamingViewData) string {
	var b strings.Builder

	width := data.Width
	boxWidth := BoxWidth(width)

	if data.Waiting {
		elapsed := time.Since(data.RequestStart)
		parts := []string{
			ThinkingLabel.Render("waiting"),
			CanvasStatInline.Render(formatDuration(elapsed.Milliseconds())),
		}
		b.WriteString(drawCanvasSpan(parts, nil, P.TextMuted, width))
	}

	if data.ThinkingText != "" || data.InThinking {
		dur := data.ThinkingDur.Milliseconds()
		parts := []string{
			ThinkingLabel.Render("thinking"),
			CanvasStatInline.Render(tokenChipOutput(data.ThinkingTokens, &dur)),
		}
		var content []string
		if data.Expanded {
			if data.ThinkingText != "" {
				content = []string{data.ThinkingText}
			} else {
				content = []string{"..."}
			}
		}
		b.WriteString(drawCanvasSpan(parts, content, P.TextMuted, width))
	}

	if data.RenderedMarkdown != "" || data.Partial != "" {
		// RenderedMarkdown is already pre-wrapped and styled.
		// Partial is raw, unwrapped text — render it with wrapping.
		var body string
		if data.RenderedMarkdown != "" {
			body = data.RenderedMarkdown
		}
		if data.Partial != "" {
			wrappedPartial := RenderMarkdownOnBg(data.Partial, P.BgApp, CanvasContentWidth(data.Width))
			if body != "" {
				body = body + "\n" + wrappedPartial
			} else {
				body = wrappedPartial
			}
		}
		b.WriteString(drawCanvasSpan(nil, []string{body}, P.TextPrimary, data.Width))
	}

	if len(data.PendingTools) > 0 {
		b.WriteString(renderStreamingToolCalls(data.PendingTools, boxWidth, data.Expanded))
	}

	return b.String()
}

// renderSeqStatRight builds the right-side content of an assistant header from a SequenceStat.
func renderSeqStatRight(stat *config.SequenceStat) string {
	if stat == nil {
		return ""
	}
	var parts []string
	if stat.AvgTokensPerSec > 0 {
		parts = append(parts, AssistantHeaderDimStyle.Render(fmt.Sprintf("%.1f tok/s", stat.AvgTokensPerSec)))
	}
	var execDur *int64
	if stat.InputTokens > 0 {
		execDur = &stat.ExecDurMs
	}
	if chip := tokenChipBoth(stat.OutputTokens, stat.InputTokens, &stat.DurationMs, execDur); chip != "" {
		parts = append(parts, AssistantHeaderDimStyle.Render(chip))
	}
	return strings.Join(parts, AssistantHeaderDimStyle.Render("  "))
}

// renderStreamingToolCalls renders pending tool calls during streaming.
func renderStreamingToolCalls(pendingTools []StreamingToolCall, boxWidth int, expanded bool) string {
	var b strings.Builder
	reg := tools.GetRegistry()
	for _, tc := range pendingTools {
		namePart, paramPart := formatToolDisplay(tc.Name, tc.Arguments, reg)

		var parts []string
		parts = append(parts, ToolLabel.Render(namePart))
		if paramPart != "" {
			parts = append(parts, ToolParamOnTool.Render(paramPart))
		}
		if tc.Tokens > 0 || tc.Duration > 0 {
			dur := tc.Duration.Milliseconds()
			parts = append(parts, ToolStatOnTool.Render(tokenChipOutput(tc.Tokens, &dur)))
		}

		var content []string
		if expanded && tc.Arguments != "" {
			content = []string{tc.Arguments}
		}

		b.WriteString(drawToolBox(parts, content, P.TextDim, boxWidth))
	}
	return b.String()
}

func tokenChipOutput(n int, durMs *int64) string {
	s := "↓" + formatTokens(n)
	if durMs != nil {
		s += " " + formatDuration(*durMs)
	}
	return s
}

func tokenChipInput(n int, durMs *int64) string {
	s := "↑" + formatTokens(n)
	if durMs != nil {
		s += " " + formatDuration(*durMs)
	}
	return s
}

// tokenChipBoth builds ·↓downN[/↑upN][ downDur[/upDur]]·
// dur pointers: nil means "don't show", &val means "show val" (including 0).
func tokenChipBoth(downN, upN int, downDurMs *int64, execDurMs *int64) string {
	s := ""
	if downN > 0 {
		s += "↓" + formatTokens(downN)
	}
	if upN > 0 {
		if downN > 0 {
			s += ""
		}
		s += "↑" + formatTokens(upN)
	}
	showDur := downDurMs != nil || execDurMs != nil
	if showDur {
		s += " ↓"
		if downDurMs != nil {
			s += formatDuration(*downDurMs)
		}
		if execDurMs != nil {
			if downDurMs != nil {
				s += " ►"
			}
			s += formatDuration(*execDurMs)
		}
	}
	return s
}

// formatTokens formats a token count with k/M suffix above 1000.
func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatDuration(ms int64) string {
	if ms == 0 {
		return "<1ms"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fsec", d.Seconds())
	}
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// stripNewlines replaces newlines with spaces for clean single-line display.
func stripNewlines(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

// formatToolDisplay returns a display-friendly (namePart, paramPart).
// The "↳ " prefix is now added by DrawCanvas.
// When args is empty or the tool has no DisplayParam, paramPart is empty.
func formatToolDisplay(name, args string, reg *tools.Registry) (namePart string, paramPart string) {
	if args != "" && reg != nil {
		tool := reg.Get(name)
		if tool != nil {
			if display := tool.DisplayValue(args); display != "" {
				return name, truncate(display, 60)
			}
		}
	}
	return name, ""
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"rig-chat/internal/config"
	"rig-chat/internal/tools"

	"github.com/charmbracelet/lipgloss"
)

// RenderMessage dispatches to the correct renderer by role.
func RenderMessage(msg config.Message, width int, expanded bool) string {
	switch msg.Role {
	case "user":
		return renderUserMessage(msg, width)
	default:
		return renderAssistantMessage(msg, width, expanded)
	}
}

// renderUserMessage renders a user message as a single UserBox containing
// the header line + body text.
func renderUserMessage(msg config.Message, width int) string {
	boxWidth := width - 2*BoxMargin
	if boxWidth < 20 {
		boxWidth = 20
	}
	inner := boxWidth - 4 // minus left+right padding (2+2)

	leftStr := UserHeaderDimStyle.Render(msg.CreatedAt.Format("15:04:05"))
	var right []string
	if msg.ImagePath != "" {
		right = append(right, UserHeaderAttStyle.Render(msg.ImagePath))
	}
	if msg.UserTokens > 0 {
		right = append(right, UserHeaderDimStyle.Render(tokenChipInput(msg.UserTokens, nil)))
	}
	rightStr := strings.Join(right, UserHeaderDimStyle.Render("  "))
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	headerLine := leftStr + UserHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr

	return UserBox.Width(boxWidth).Render("\n" + headerLine + "\n\n" + msg.Text + "\n")
}

// RenderAssistantHeader emits the assistant header as a CanvasSpan.
// Stays uncached: SequenceStat mutates while a stream is live.
func RenderAssistantHeader(start time.Time, stat *config.SequenceStat, width int) string {
	inner := width - 2*(BoxMargin+2) // canvas horizontal padding (BoxMargin+2 each side)
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
	boxWidth := width - 2*BoxMargin
	if boxWidth < 20 {
		boxWidth = 20
	}

	if msg.ThinkingText != "" {
		thinkLabel := ThinkingLabel.Render("\n↳ thinking") +
			CanvasStatInline.Render(" · "+tokenChipOutput(msg.ThinkingMetrics.Tokens, &msg.ThinkingMetrics.InferenceDuractionMs))
		content := thinkLabel
		if expanded {
			content += "\n\n" + msg.ThinkingText
		}
		b.WriteString(CanvasThinkingSpan.Width(width).Render(content))
	}

	if msg.Text != "" && msg.Text != "\n\n" {
		body := RenderMarkdownOnBg(msg.Text, P.BgApp)
		b.WriteString(CanvasSpan.Width(width).Render(body))
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
		label := ToolLabel.Render(namePart)
		if paramPart != "" {
			label += ToolParamOnTool.Render(" · " + paramPart)
		}
		stats := ToolStatOnTool.Render(" · " + tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs))

		var marker string
		switch tc.Execution.Status {
		case "error":
			marker = ToolErrOnTool.Render(" [✗]")
		case "success":
			marker = ToolCheckOnTool.Render(" [✓]")
		}

		content := label + marker + stats
		if expanded {
			if tc.Instruction.Arguments != "" {
				content += "\n\n" + stripNewlines(tc.Instruction.Arguments)
			}
			switch tc.Execution.Status {
			case "error":
				content += "\n\n" + ToolErrorOnTool.Render(tc.Execution.Error)
				if tc.Execution.Result != "" {
					content += "\n\n" + "Result:\n" + tc.Execution.Result
				}
			case "success":
				if tc.Execution.Result != "" {
					content += "\n\n" + tc.Execution.Result
				}
			}
		}
		b.WriteString(ToolBox.Width(boxWidth).Render("\n\n" + content + "\n"))
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
	if width < 20 {
		width = 20
	}
	boxWidth := width - 2*BoxMargin
	if boxWidth < 20 {
		boxWidth = 20
	}

	if data.Waiting {
		elapsed := time.Since(data.RequestStart)
		waitLabel := ThinkingLabel.Render("\n↳ waiting") + CanvasStatInline.Render(" · "+formatDuration(elapsed.Milliseconds()))
		b.WriteString(CanvasThinkingSpan.Width(width).Render(waitLabel))
	}

	if data.ThinkingText != "" || data.InThinking {
		dur := data.ThinkingDur.Milliseconds()
		thinkLabel := ThinkingLabel.Render("\n↳ thinking") +
			CanvasStatInline.Render(" · "+tokenChipOutput(data.ThinkingTokens, &dur))
		content := thinkLabel
		if data.Expanded {
			if data.ThinkingText != "" {
				content += "\n\n" + data.ThinkingText
			} else {
				content += "\n\n ... "
			}
		}
		b.WriteString(CanvasThinkingSpan.Width(width).Render(content))
	}

	if data.RenderedMarkdown != "" || data.Partial != "" {
		body := data.RenderedMarkdown + data.Partial
		b.WriteString(CanvasSpan.Width(width).Render(body))
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
		label := ToolLabel.Render(namePart)
		if paramPart != "" {
			label += ToolParamOnTool.Render(" · " + paramPart)
		}
		var statPart string
		if tc.Tokens > 0 || tc.Duration > 0 {
			dur := tc.Duration.Milliseconds()
			statPart = ToolStatOnTool.Render("  " + tokenChipOutput(tc.Tokens, &dur))
		}
		content := label + statPart
		if expanded && tc.Arguments != "" {
			content += "\n\n" + tc.Arguments
		}
		b.WriteString(ToolBox.Width(boxWidth).Render("\n\n" + content + "\n"))
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

// formatToolDisplay returns a display-friendly label: ↳ name or ↳ name(display_value).
// When args is empty or the tool has no DisplayParam, it shows just ↳ name.
// Once args arrive, it returns (namePart, paramPart) where paramPart may be empty.
func formatToolDisplay(name, args string, reg *tools.Registry) (namePart string, paramPart string) {
	if args != "" && reg != nil {
		tool := reg.Get(name)
		if tool != nil {
			if display := tool.DisplayValue(args); display != "" {
				return "↳ " + name, truncate(display, 50)
			}
		}
	}
	return "↳ " + name, ""
}

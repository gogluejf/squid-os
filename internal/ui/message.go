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

// renderUserMessage renders a user message: just text.
func renderUserMessage(msg config.Message, width int) string {
	bodyWidth := width
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	return UserMsgStyle.Width(bodyWidth).Render("\n" + msg.Text + "\n")
}

// renderAssistantMessage renders an assistant message: thinking, text, tool calls.
func renderAssistantMessage(msg config.Message, width int, expanded bool) string {
	var b strings.Builder
	bodyWidth := width
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	body := msg.Text
	if body != "" {
		body = RenderMarkdownOnBg(body, "233")
	}

	// Thinking block
	if msg.ThinkingText != "" {
		thinkLabel := ThinkingStyle.Render("↳ thinking ") + ToolStatInline.Render(" "+tokenChipOutput(msg.ThinkingMetrics.Tokens, &msg.ThinkingMetrics.InferenceDuractionMs))
		if expanded {
			b.WriteString(toolLineBg.Width(bodyWidth).Render("\n" + thinkLabel + "\n"))
			b.WriteString(ToolCallResultStyle.Width(bodyWidth).Render(msg.ThinkingText + "\n"))
		} else {
			b.WriteString(toolLineBg.Width(bodyWidth).Render("\n" + thinkLabel + "\n"))
		}
	}

	// Text body
	if body != "" {
		b.WriteString(AssistantMsgStyle.Width(bodyWidth).Render(body + "\n"))
	}

	// Tool calls
	if len(msg.ToolCalls) > 0 {
		b.WriteString(renderToolCallsInline(msg.ToolCalls, bodyWidth, expanded, tools.GetRegistry()))
	}

	return b.String()
}

// RenderUserHeader builds the header line for a user message.
func RenderUserHeader(msg config.Message, width int) string {
	inner := width - 2
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
	return UserHeaderStyle.Width(width).Render(
		"\n\n" + leftStr + UserHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr + "\n",
	)
}

// RenderAssistantHeader builds the header line for an assistant message.
func RenderAssistantHeader(start time.Time, stat *config.SequenceStat, width int) string {
	inner := width - 2
	leftStr := AssistantHeaderDimStyle.Render(start.Format("15:04:05"))
	rightStr := renderSeqStatRight(stat)
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	return AssistantHeaderStyle.Width(width).Render(
		"\n\n" + leftStr + AssistantHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr + "\n",
	)
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

	bubbleWidth := data.Width
	if bubbleWidth < 20 {
		bubbleWidth = 20
	}
	bodyWidth := bubbleWidth

	// Waiting state
	if data.Waiting {
		elapsed := time.Since(data.RequestStart)
		waitLabel := ThinkingStyle.Render("↳ waiting ") + ToolStatInline.Render(" "+formatDuration(elapsed.Milliseconds()))
		b.WriteString(toolLineBg.Width(bodyWidth).Render("\n" + waitLabel + "\n"))
	}

	// Thinking block
	if data.ThinkingText != "" || data.InThinking {
		dur := data.ThinkingDur.Milliseconds()
		thinkLabel := ThinkingStyle.Render("↳ thinking ") + ToolStatInline.Render(" "+tokenChipOutput(data.ThinkingTokens, &dur))
		if data.Expanded {
			b.WriteString(toolLineBg.Width(bodyWidth).Render("\n" + thinkLabel + "\n"))
			if data.ThinkingText != "" {
				b.WriteString(ToolCallResultStyle.Width(bodyWidth).Render(data.ThinkingText + "\n"))
			} else {
				b.WriteString(ToolCallResultStyle.Width(bodyWidth).Render(" ... \n"))
			}
		} else {
			b.WriteString(toolLineBg.Width(bodyWidth).Render("\n" + thinkLabel + "\n"))
		}
	}

	// Text content
	if data.RenderedMarkdown != "" || data.Partial != "" {
		style := AssistantMsgStyle.Width(bodyWidth)
		body := data.RenderedMarkdown
		if data.Partial != "" {
			body += data.Partial
		}
		b.WriteString(style.Render("\n" + body + "\n"))
	}

	// Pending tool calls
	if len(data.PendingTools) > 0 {
		b.WriteString(renderStreamingToolCalls(data.PendingTools, bodyWidth, data.Expanded))
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

// toolLineBg is a plain background style used as a width wrapper for composed tool lines.
// Inline segments only set foreground — the wrapper provides background and padding.
var toolLineBg = lipgloss.NewStyle().Background(lipgloss.Color(P.BgApp)).Padding(0, 1)

// renderToolCallsInline renders tool call lines with timing/token stats.
func renderToolCallsInline(toolCalls []config.ToolCallEntry, width int, expanded bool, reg *tools.Registry) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		namePart, paramPart := formatToolDisplay(tc.Instruction.Name, tc.Instruction.Arguments, reg)
		label := ToolCallInline.Render(namePart)
		if paramPart != "" {
			label += ToolParamInline.Render(paramPart)
		}

		switch tc.Execution.Status {
		case "error":
			checkAndErr := ToolErrInline.Render(" [✗]")
			stats := ToolStatInline.Render(" " + tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs))
			b.WriteString(toolLineBg.Width(width).Render("\n" + label + checkAndErr + stats + "\n"))
			if expanded {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Instruction.Arguments) + "\n"))
				b.WriteString(ToolCallErrorStyle.Width(width).Render("\n" + tc.Execution.Error + "\n"))
				if tc.Execution.Result != "" {
					b.WriteString(ToolCallResultStyle.Width(width).Render("\nResult:\n" + tc.Execution.Result + "\n"))
				}
			}
		case "success":
			checkAndResult := ToolCheckInline.Render(" [✓]")
			stats := ToolStatInline.Render(" " + tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs))
			b.WriteString(toolLineBg.Width(width).Render("\n" + label + checkAndResult + stats + "\n"))
			if expanded {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Instruction.Arguments) + "\n"))
				if tc.Execution.Result != "" {
					b.WriteString(ToolCallResultStyle.Width(width).Render("\n" + tc.Execution.Result + "\n"))
				}
			}
		default:
			b.WriteString(toolLineBg.Width(width).Render("\n" + label + "\n"))
			if expanded && tc.Instruction.Arguments != "" {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Instruction.Arguments) + "\n"))
			}
		}
	}
	return b.String()
}

// renderStreamingToolCalls renders pending tool calls during streaming.
func renderStreamingToolCalls(pendingTools []StreamingToolCall, width int, expanded bool) string {
	var b strings.Builder
	reg := tools.GetRegistry()
	for _, tc := range pendingTools {
		namePart, paramPart := formatToolDisplay(tc.Name, tc.Arguments, reg)
		label := ToolCallInline.Render(namePart)
		if paramPart != "" {
			label += ToolParamInline.Render(paramPart)
		}
		var statPart string
		if tc.Tokens > 0 || tc.Duration > 0 {
			dur := tc.Duration.Milliseconds()
			statPart = ToolStatInline.Render("  " + tokenChipOutput(tc.Tokens, &dur))
		}
		b.WriteString(toolLineBg.Width(width).Render("\n" + label + statPart + "\n"))
		if expanded && tc.Arguments != "" {
			b.WriteString(ToolCallResultStyle.Width(width).Render("\n" + tc.Arguments + "\n"))
		}
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
				return "↳ " + name, " " + truncate(display, 50)
			}
		}
	}
	return "↳ " + name, ""
}

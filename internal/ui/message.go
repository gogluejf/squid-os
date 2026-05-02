package ui

import (
	"fmt"
	"strings"
	"time"

	"rig-chat/internal/config"

	"github.com/charmbracelet/lipgloss"
)

// RenderMessage renders a single chat message for the viewport
func RenderMessage(msg config.Message, width int, expanded bool) string {
	var b strings.Builder

	bubbleWidth := width
	if bubbleWidth < 20 {
		bubbleWidth = 20
	}

	bodyWidth := bubbleWidth
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	// Message body & thinking

	style := AssistantMsgStyle
	if msg.Role == "user" {
		style = UserMsgStyle
	}
	style = style.Width(bodyWidth)

	body := msg.Text

	if body != "" && msg.Role == "assistant" {
		body = RenderMarkdownOnBg(body, "233")
	}

	// Thinking block (collapsed/expanded) — must come BEFORE text
	if msg.ThinkingText != "" {
		thinkStyle := ThinkingStyle.Width(bodyWidth)
		b.WriteString("\n")
		thinkLabel := " thinking " + tokenChipDown(msg.ThinkingMetrics.Tokens, &msg.ThinkingMetrics.InferenceDuractionMs)
		if expanded {
			b.WriteString(thinkStyle.Render("\n" + thinkLabel + "\n"))
			b.WriteString(thinkStyle.Render("\n" + msg.ThinkingText))
		} else {
			b.WriteString(thinkStyle.Render("\n" + thinkLabel))
		}
	}

	if body != "" {
		b.WriteString(style.Render("\n" + body + "\n"))
	}

	// Tool calls: render as inline lines with results
	if len(msg.ToolCalls) > 0 {
		b.WriteString(renderToolCallsInline(msg.ToolCalls, bubbleWidth, expanded))
	}

	// One trailing spacer line after each message block.
	b.WriteString("\n")
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
		right = append(right, UserHeaderDimStyle.Render(tokenChipUp(msg.UserTokens, nil)))
	}

	rightStr := strings.Join(right, UserHeaderDimStyle.Render("  "))
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}

	return UserHeaderStyle.Width(width).Render(
		"\n" + leftStr + UserHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr + "\n",
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
		"\n" + leftStr + AssistantHeaderDimStyle.Render(strings.Repeat(" ", gap)) + rightStr + "\n",
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

	// Waiting state: show "waiting..." with live elapsed before first token
	if data.Waiting {
		elapsed := time.Since(data.RequestStart)
		b.WriteString(ThinkingStyle.Width(bodyWidth).Render("\n  waiting...  " + formatDuration(elapsed.Milliseconds()) + "\n"))
	}

	// Thinking block — shown when thinking text exists or we're in thinking mode
	if data.ThinkingText != "" || data.InThinking {
		thinkStyle := ThinkingStyle.Width(bodyWidth)
		dur := data.ThinkingDur.Milliseconds()
		thinkLabel := " thinking " + tokenChipDown(data.ThinkingTokens, &dur)
		if data.Expanded {
			b.WriteString(thinkStyle.Render("\n" + thinkLabel + "\n"))
			if data.ThinkingText != "" {
				b.WriteString(thinkStyle.Render(data.ThinkingText))
			} else {
				b.WriteString(thinkStyle.Render("\n thinking...\n"))
			}
		} else {
			// Collapsed: only show the label, NOT the thinking text
			b.WriteString(thinkStyle.Render("\n" + thinkLabel))
		}
	}

	// Text content
	if data.RenderedMarkdown != "" || data.Partial != "" {
		style := AssistantMsgStyle.Width(bodyWidth)
		body := data.RenderedMarkdown
		if data.Partial != "" {
			if body != "" {
				body += "\n"
			}
			body += data.Partial
		}
		b.WriteString(style.Render("\n" + body + "\n"))
		b.WriteString("\n")
	}

	// Pending tool calls — shown during streaming before execution
	if len(data.PendingTools) > 0 {
		for _, tc := range data.PendingTools {
			argsDisplay := stripNewlines(truncate(tc.Arguments, 50))
			namePart := ToolCallInline.Render(" ↳ " + tc.Name + "(" + argsDisplay + ")")
			var statPart string
			if tc.Tokens > 0 || tc.Duration > 0 {
				dur := tc.Duration.Milliseconds()
				statPart = ToolStatInline.Render("  " + tokenChipDown(tc.Tokens, &dur))
			}
			b.WriteString(toolLineBg.Width(bodyWidth).Render("\n" + namePart + statPart + "\n"))
			if data.Expanded && tc.Arguments != "" {
				b.WriteString(ToolCallResultStyle.Width(bodyWidth).Render("\n" + tc.Arguments + "\n"))
			}
		}
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
	if chip := tokenChipBoth(stat.OutputTokens, stat.InputTokens, &stat.InferenceDuractionMs, execDur); chip != "" {
		parts = append(parts, AssistantHeaderDimStyle.Render(chip))
	}
	return strings.Join(parts, AssistantHeaderDimStyle.Render("  "))
}

// toolLineBg is a plain background style used as a width wrapper for composed tool lines.
// Inline segments only set foreground — the wrapper provides background and padding.
var toolLineBg = lipgloss.NewStyle().Background(lipgloss.Color("233")).Padding(0, 1)

// renderToolCallsInline renders tool call lines with timing/token stats.
func renderToolCallsInline(toolCalls []config.ToolCallEntry, width int, expanded bool) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		argsDisplay := stripNewlines(truncate(tc.Instruction.Arguments, 50))
		namePart := ToolCallInline.Render(" ↳ " + tc.Instruction.Name + "(" + argsDisplay + ")")

		switch tc.Execution.Status {
		case "error":
			checkAndErr := ToolErrInline.Render(" ✗ " + stripNewlines(truncate(tc.Execution.Error, 30)))
			stats := ToolStatInline.Render(" " + tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs))
			b.WriteString(toolLineBg.Width(width).Render("\n" + namePart + checkAndErr + stats + "\n"))
			if expanded {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Instruction.Arguments) + "\n"))
				b.WriteString(ToolCallErrorStyle.Width(width).Render("\n" + tc.Execution.Error + "\n"))
				if tc.Execution.Result != "" {
					b.WriteString(ToolCallResultStyle.Width(width).Render("\nResult:\n" + tc.Execution.Result + "\n"))
				}
			}
		case "success":
			checkAndResult := ToolCheckInline.Render(" ✓ " + stripNewlines(truncate(tc.Execution.Result, 30)))
			stats := ToolStatInline.Render(" " + tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs))
			b.WriteString(toolLineBg.Width(width).Render("\n" + namePart + checkAndResult + stats + "\n"))
			if expanded {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Instruction.Arguments) + "\n"))
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n" + tc.Execution.Result + "\n"))
			}
		default:
			// No execution yet (streaming, before tool execution) — allow expand
			b.WriteString(toolLineBg.Width(width).Render("\n" + namePart + "\n"))
			if expanded && tc.Instruction.Arguments != "" {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Instruction.Arguments) + "\n"))
			}
		}
	}
	return b.String()
}

func tokenChipDown(n int, durMs *int64) string {
	s := "↓" + formatTokens(n)
	if durMs != nil {
		s += " " + formatDuration(*durMs)
	}
	return s
}

func tokenChipUp(n int, durMs *int64) string {
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

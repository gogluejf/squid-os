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

	// Skip tool result messages — they are rendered inline with the assistant's tool calls
	if msg.Role == "tool" {
		return ""
	}

	hasTools := len(msg.ToolCalls) > 0

	// Determine if we should skip header/body: no text AND no thinking but has tools.
	skipBody := msg.Role == "assistant" && msg.Text == "" && msg.ThinkingText == "" && hasTools

	// Header line: date left, metadata right
	if !skipBody {
		header := renderHeader(msg, bubbleWidth)
		b.WriteString(header)
	}

	// Message body & thinking
	if !skipBody {
		style := AssistantMsgStyle
		if msg.Role == "user" {
			style = UserMsgStyle
		}
		style = style.Width(bodyWidth)

		body := msg.Text
		if body == "" && msg.Role == "assistant" {
			body = "..."
		}

		if msg.Role == "assistant" {
			body = RenderMarkdownOnBg(body, "233")
		}

		// Thinking block (collapsed/expanded) — must come BEFORE text
		if msg.ThinkingText != "" {
			thinkStyle := ThinkingStyle.Width(bodyWidth)
			b.WriteString("\n")
			thinkLabel := " thinking " + tokenChipDown(msg.ThinkingTokens, &msg.ThinkingDurationMs)
			if expanded {
				b.WriteString(thinkStyle.Render("\n" + thinkLabel + "\n"))
				b.WriteString(thinkStyle.Render("\n" + msg.ThinkingText))
			} else {
				b.WriteString(thinkStyle.Render("\n" + thinkLabel))
			}
		}

		b.WriteString(style.Render("\n" + body + "\n"))
	}

	// Tool calls: render as inline lines with results
	if hasTools {
		b.WriteString(renderToolCallsInline(msg.ToolCalls, bubbleWidth, expanded))
	}

	// One trailing spacer line after each message block.
	b.WriteString("\n")
	return b.String()
}

func renderHeader(msg config.Message, width int) string {
	dim := AssistantHeaderDimStyle
	att := AssistantHeaderAttStyle
	lineStyle := AssistantHeaderStyle
	if msg.Role == "user" {
		dim = UserHeaderDimStyle
		att = UserHeaderAttStyle
		lineStyle = UserHeaderStyle
	}
	inner := width - 2 // Padding(0,1) is outer, inner content area = width-2

	leftStr := dim.Render(msg.CreatedAt.Format("15:04:05"))

	var right []string
	if msg.ImagePath != "" {
		right = append(right, att.Render(msg.ImagePath))
	}
	if msg.Role == "user" {
		if msg.UserTokens > 0 {
			right = append(right, dim.Render(tokenChipUp(msg.UserTokens, nil)))
		}
	} else {
		if msg.TokensPerSecond > 0 {
			right = append(right, dim.Render(fmt.Sprintf("%.1f tok/s", msg.TokensPerSecond)))
		}
		if msg.TextDurationMs > 0 {
			right = append(right, dim.Render(formatDuration(msg.TextDurationMs)))
		}
		if msg.TextTokens > 0 {
			right = append(right, dim.Render(fmt.Sprintf("%d tokens", msg.TextTokens)))
		}
	}

	rightStr := strings.Join(right, dim.Render("  "))
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}

	return lineStyle.Width(width).Render(
		"\n" + leftStr + dim.Render(strings.Repeat(" ", gap)) + rightStr + "\n",
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

	streamHeader := renderStreamingHeader(data)
	b.WriteString(streamHeader)
	b.WriteString("\n")

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

// renderStreamingHeader mirrors renderHeader visually:
// timestamp on the left, [tok/s  elapsed  N tokens] on the right.
func renderStreamingHeader(data StreamingViewData) string {
	leftStr := data.RequestStart.Format("15:04:05")

	var right []string
	if data.TokPerSec > 0 {
		right = append(right, fmt.Sprintf("%.1f tok/s", data.TokPerSec))
	}
	if data.TextDur > 0 {
		right = append(right, formatDuration(data.TextDur.Milliseconds()))
	}
	if data.TextTokens > 0 {
		right = append(right, fmt.Sprintf("%d tokens", data.TextTokens))
	}

	rightStr := strings.Join(right, "  ")
	gap := data.Width - lipgloss.Width(leftStr) - lipgloss.Width(rightStr) - 2
	if gap < 1 {
		gap = 1
	}

	header := leftStr + strings.Repeat(" ", gap) + rightStr
	return AssistantHeaderStyle.Width(data.Width).Render("\n" + header + "\n")
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
func tokenChipBoth(downN, upN int, downDurMs *int64, upDurMs *int64) string {
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
	showDur := downDurMs != nil || upDurMs != nil
	if showDur {
		s += " ↓"
		if downDurMs != nil {
			s += formatDuration(*downDurMs)
		}
		if upDurMs != nil {
			if downDurMs != nil {
				s += "↑"
			}
			s += formatDuration(*upDurMs)
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
		return "1ms"
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

// toolLineBg is a plain background style used as a width wrapper for composed tool lines.
// Inline segments only set foreground — the wrapper provides background and padding.
var toolLineBg = lipgloss.NewStyle().Background(lipgloss.Color("233")).Padding(0, 1)

// renderToolCallsInline renders tool call lines with timing/token stats.
func renderToolCallsInline(toolCalls []config.ToolCallEntry, width int, expanded bool) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		argsDisplay := stripNewlines(truncate(tc.Arguments, 50))
		namePart := ToolCallInline.Render(" ↳ " + tc.Name + "(" + argsDisplay + ")")

		if tc.Error != "" {
			checkAndErr := ToolErrInline.Render(" ✗ " + stripNewlines(truncate(tc.Error, 30)))
			stats := ToolStatInline.Render(" " + tokenChipBoth(tc.CallTokens, tc.ResultTokens, &tc.CallDurationMs, &tc.ResultDurationMs))
			b.WriteString(toolLineBg.Width(width).Render("\n" + namePart + checkAndErr + stats + "\n"))
			if expanded {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Arguments) + "\n"))
				b.WriteString(ToolCallErrorStyle.Width(width).Render("\n" + tc.Error + "\n"))
				if tc.Result != "" && tc.Result != tc.Error {
					b.WriteString(ToolCallResultStyle.Width(width).Render("\nResult:\n" + tc.Result + "\n"))
				}

			}
		} else if tc.Result != "" {
			checkAndResult := ToolCheckInline.Render(" ✓ " + stripNewlines(truncate(tc.Result, 30)))
			stats := ToolStatInline.Render(" " + tokenChipBoth(tc.CallTokens, tc.ResultTokens, &tc.CallDurationMs, &tc.ResultDurationMs))
			b.WriteString(toolLineBg.Width(width).Render("\n" + namePart + checkAndResult + stats + "\n"))
			if expanded {
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n  " + stripNewlines(tc.Arguments) + "\n"))
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n" + tc.Result + "\n"))
			}
		} else {
			// No result yet (streaming, before tool execution)
			b.WriteString(toolLineBg.Width(width).Render("\n" + namePart + "\n"))
		}
	}
	return b.String()
}

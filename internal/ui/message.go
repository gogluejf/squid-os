package ui

import (
	"fmt"
	"strings"
	"time"

	"rig-chat/internal/config"

	"github.com/charmbracelet/lipgloss"
)

// RenderMessage renders a single chat message for the viewport
func RenderMessage(msg config.Message, width int, thinkingExpanded bool) string {
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

	// Header line: date left, metadata right
	header := renderHeader(msg, bubbleWidth)
	b.WriteString(header)

	// Message body
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
		var thinkLabel string
		if msg.ThinkingDurationMs > 0 {
			thinkLabel = fmt.Sprintf(" thinking (%d tokens, %s)", msg.ThinkingTokens, formatDuration(msg.ThinkingDurationMs))
		} else {
			thinkLabel = fmt.Sprintf(" thinking (%d tokens)", msg.ThinkingTokens)
		}
		if thinkingExpanded {
			b.WriteString(thinkStyle.Render("\n" + thinkLabel + "\n"))
			b.WriteString(thinkStyle.Render(msg.ThinkingText + "\n"))
		} else {
			b.WriteString(thinkStyle.Render("\n" + thinkLabel + ", ctrl+e to expand" + "\n"))
		}
	}

	b.WriteString(style.Render("\n" + body + "\n"))

	// Tool calls: render as inline lines with results
	if msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
		b.WriteString(renderToolCallsInline(msg.ToolCalls, bubbleWidth))
	}

	// One trailing spacer line after each message block.
	b.WriteString("\n")
	return b.String()
}

// renderToolCallsInline renders tool call lines, showing result on the same line if available.
func renderToolCallsInline(toolCalls []config.ToolCallEntry, width int) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		args := tc.Arguments
		if len(args) > 50 {
			args = args[:50] + "..."
		}
		line := fmt.Sprintf(" ↳ %s(%s)", tc.Name, args)

		if tc.Error != "" {
			err := tc.Error
			if len(err) > 60 {
				err = err[:60] + "..."
			}
			line += " ✗ " + err
			b.WriteString(ToolCallErrorStyle.Width(width).Render("\n" + line + "\n"))
		} else if tc.Result != "" {
			if len(tc.Result) > 60 {
				line += " ✓"
				b.WriteString(ToolCallStyle.Width(width).Render("\n" + line + "\n"))
				b.WriteString(ToolCallResultStyle.Width(width).Render("\n" + tc.Result + "\n"))
			} else {
				line += " ✓ " + tc.Result
				b.WriteString(ToolCallStyle.Width(width).Render("\n" + line + "\n"))
			}
		} else {
			// No result yet (streaming, before tool execution)
			b.WriteString(ToolCallStyle.Width(width).Render("\n" + line + "\n"))
		}
	}
	return b.String()
}

// not used in the moment, but could be used to render tool results that come in as separate messages (e.g. from streaming updates or if the provider doesn't support inline tool call results)
func renderToolResults(results []config.ToolResultEntry, width int) string {
	var b strings.Builder
	for _, r := range results {
		line := fmt.Sprintf("[%s: %s]", r.Name, r.ToolCallID)
		if r.Error != "" {
			line += " ✗ " + r.Error
			b.WriteString(ToolCallErrorStyle.Width(width).Render("\n" + line + "\n"))
		} else {
			result := r.Result
			if len(result) > 80 {
				result = result[:80] + "..."
			}
			line += " ✓ " + result
			b.WriteString(ToolCallStyle.Width(width).Render("\n" + line + "\n"))
		}
	}
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
			right = append(right, dim.Render(fmt.Sprintf("%d tokens", msg.UserTokens)))
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
	ThinkingExpanded bool

	// Timing
	RequestStart   time.Time
	ThinkingTokens int
	ThinkingDur    time.Duration
	TextTokens     int
	TextDur        time.Duration
	TokPerSec      float64
	Waiting        bool // true when no first token has arrived yet
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
		var thinkLabel string
		if data.ThinkingDur > 0 {
			thinkLabel = fmt.Sprintf(" thinking (%d tokens, %s)", data.ThinkingTokens, formatDuration(data.ThinkingDur.Milliseconds()))
		} else {
			thinkLabel = fmt.Sprintf(" thinking (%d tokens)", data.ThinkingTokens)
		}
		if data.ThinkingExpanded {
			b.WriteString(thinkStyle.Render("\n" + thinkLabel + "\n"))
			if data.ThinkingText != "" {
				b.WriteString(thinkStyle.Render(data.ThinkingText))
			} else {
				b.WriteString(thinkStyle.Render("\n thinking...\n"))
			}
		} else {
			// Collapsed: only show the label, NOT the thinking text
			b.WriteString(thinkStyle.Render("\n" + thinkLabel + ", ctrl+e to expand" + "\n"))
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

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1f sec", d.Seconds())
	}
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

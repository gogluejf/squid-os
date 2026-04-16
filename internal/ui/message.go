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

	// Left-margin concept removed (requested): use full available width.
	bubbleWidth := width
	if bubbleWidth < 20 {
		bubbleWidth = 20
	}

	// Header line: date left, metadata right
	header := renderHeader(msg, bubbleWidth)
	b.WriteString(header)

	// Message body
	style := AssistantMsgStyle
	if msg.Role == "user" {
		style = UserMsgStyle
	}

	bodyWidth := bubbleWidth
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	style = style.Width(bodyWidth)

	body := msg.Text
	if body == "" && msg.Role == "assistant" {
		body = "..."
	}

	// For assistant messages, render markdown and restore the background colour
	// after every glamour reset sequence — same technique used for the header
	// inline styles — so the lipgloss block background stays solid throughout.
	if msg.Role == "assistant" {
		body = RenderMarkdownOnBg(body, "233")
	}

	b.WriteString(style.Render("\n" + body + "\n"))

	// Thinking block (collapsed/expanded)
	if msg.ThinkingText != "" {
		b.WriteString("\n")
		if thinkingExpanded {
			thinkStyle := ThinkingStyle.Width(bodyWidth)
			b.WriteString(ThinkingLabelStyle.Render("\n" + fmt.Sprintf(" thinking (%d tokens)", msg.ThinkingTokens) + "\n"))
			b.WriteString("\n")
			b.WriteString(thinkStyle.Render(msg.ThinkingText))

		} else {
			// Show token count instead of line count
			tokens := msg.ThinkingTokens
			label := fmt.Sprintf(" thinking (%d tokens, ctrl+e to expand)", tokens)
			b.WriteString(ThinkingLabelStyle.Render("\n" + label + "\n"))
		}
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
		if msg.InputTokens > 0 {
			right = append(right, dim.Render(fmt.Sprintf("%d tokens", msg.InputTokens)))
		}
	} else {
		if msg.TokensPerSecond > 0 {
			right = append(right, dim.Render(fmt.Sprintf("%.1f tok/s", msg.TokensPerSecond)))
		}
		if msg.ResponseTimeMs > 0 {
			right = append(right, dim.Render(formatDuration(msg.ResponseTimeMs)))
		}
		if msg.OutputTokens > 0 {
			right = append(right, dim.Render(fmt.Sprintf("%d tokens", msg.OutputTokens)))
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

// RenderStreamingMessage renders the in-progress streaming message.
// tokenCount and tokPerSec are live values; tokPerSec should be 0 until the
// first token arrives so the latency (pre-token wait) is excluded from the
// speed calculation.
// renderedMarkdown is the pre-cached glamour output for completed lines;
// partial is the current line still being typed (plain text).
func RenderStreamingMessage(renderedMarkdown, partial, thinkingText string, inThinking bool, width int, createdAt time.Time, tokenCount int, tokPerSec float64, thinkingExpanded bool, thinkingTokenCount int) string {
	var b strings.Builder

	bubbleWidth := width
	if bubbleWidth < 20 {
		bubbleWidth = 20
	}
	bodyWidth := bubbleWidth

	streamHeader := renderStreamingHeader(createdAt, tokenCount, tokPerSec, bubbleWidth)
	b.WriteString(streamHeader)
	b.WriteString("\n")

	// Thinking block — mirrors RenderMessage logic
	if thinkingText != "" || inThinking {
		if thinkingExpanded {
			thinkStyle := ThinkingStyle.Width(bodyWidth)
			b.WriteString(ThinkingLabelStyle.Render(fmt.Sprintf("\n"+" thinking (%d tokens)", thinkingTokenCount) + "\n"))
			if thinkingText != "" {
				b.WriteString("\n")
				b.WriteString(thinkStyle.Render(thinkingText))
			} else {
				b.WriteString(ThinkingLabelStyle.Render("\n thinking...\n"))
			}
		} else {
			if thinkingTokenCount > 0 {
				b.WriteString(ThinkingLabelStyle.Render("\n" + fmt.Sprintf(" thinking (%d tokens, ctrl+e to expand)", thinkingTokenCount) + "\n"))
			} else {
				b.WriteString(ThinkingLabelStyle.Render("\n thinking...\n"))
			}
		}
	}

	if renderedMarkdown != "" || partial != "" {
		style := AssistantMsgStyle.Width(bodyWidth)
		body := renderedMarkdown
		if partial != "" {
			if body != "" {
				body += "\n"
			}
			body += partial
		}
		b.WriteString(style.Render("\n" + body + "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// renderStreamingHeader mirrors renderHeader visually:
// timestamp on the left, [elapsed  tok/s  N tokens] on the right.
// The elapsed timer starts from createdAt (before the first token) so the
// pre-token latency is visible. tok/s is only shown once > 0 (caller ensures
// it stays 0 until the first token arrives).
func renderStreamingHeader(createdAt time.Time, tokenCount int, tokPerSec float64, width int) string {
	leftStr := createdAt.Format("15:04:05")

	// Order matches the completed-message header: tok/s → elapsed → tokens.
	// tok/s is suppressed until > 0 so the latency phase shows only the timer.
	var right []string
	if tokPerSec > 0 {
		right = append(right, fmt.Sprintf("%.1f tok/s", tokPerSec))
	}
	elapsed := time.Since(createdAt)
	right = append(right, formatDuration(elapsed.Milliseconds()))
	if tokenCount > 0 {
		right = append(right, fmt.Sprintf("%d tokens", tokenCount))
	}

	rightStr := strings.Join(right, "  ")
	gap := width - lipgloss.Width(leftStr) - lipgloss.Width(rightStr) - 2
	if gap < 1 {
		gap = 1
	}

	header := leftStr + strings.Repeat(" ", gap) + rightStr
	return AssistantHeaderStyle.Width(width).Render("\n" + header + "\n")
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

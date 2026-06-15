package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/style"
	"squid-os/internal/tools"
	"squid-os/internal/util"

	"github.com/charmbracelet/lipgloss"
)

// orderedParams returns the keys of msg.Params sorted alphabetically.
func orderedParams(msg config.Message) []string {
	keys := make([]string, 0, len(msg.Params))
	for k := range msg.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// renderPerLine applies a lipgloss style to each line independently, joining with \n.
// Passing multi-line input to Style.Render triggers alignTextHorizontal's pad-to-widest
// behavior, which inflates short lines to thousands of trailing spaces when the block
// contains one disproportionately long line; per-line rendering avoids that.
func renderPerLine(s string, st lipgloss.Style) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return strings.Join(lines, "\n")
}

// renderStyledContent styles plain-text content for system/internal/synthetic messages.
//   - Lines starting with "## " get the role's label color (heading emphasis).
//   - Tool names appearing as whole words get their tool's label color with
//     the content's background preserved so no transparent hole is punched.
//   - All non-heading lines are wrapped in contentStyle to maintain bg/fg.
func renderStyledContent(content string, labelStyle lipgloss.Style, contentStyle lipgloss.Style) string {
	lines := strings.Split(content, "\n")
	var styled []string

	for _, line := range lines {
		styledLine := styleContentLine(line, labelStyle, contentStyle)
		styled = append(styled, styledLine)
	}
	return strings.Join(styled, "\n")
}

// renderSystemMessage renders a system prompt message (role = system).
// Expandable like thinking/tool. Label color 141, params muted, content muted.
func renderSystemMessage(msg config.Message, width int, expanded bool) string {
	s := style.SystemStyleLabel()
	parts := []string{
		s.Label.Render(msg.Label),
	}
	if msg.Params != nil {
		for _, k := range orderedParams(msg) {
			v := styleParamValue(msg.Params[k], s.Param)
			parts = append(parts, s.Param.Render(fmt.Sprintf("%s=%s", k, v)))
		}
	}
	parts = append(parts, s.Dim.Render(tokenChipInput(msg.InputTokens, nil)))

	var content []string
	if expanded && msg.Text != "" {
		content = []string{renderStyledContent(msg.Text, s.Param, s.Content)}
	}
	return drawCanvasSpan(parts, content, s, width)
}

// renderInternalMessage renders an internal metadata message (role = internal).
// Expandable. Label color 39 (teal), params muted, content muted. No tokens (except tools def).
func renderInternalMessage(msg config.Message, width int, expanded bool) string {
	s := style.InternalStyleLabel()
	parts := []string{
		s.Label.Render(msg.Label),
	}
	if msg.Params != nil {
		for _, k := range orderedParams(msg) {
			v := styleParamValue(msg.Params[k], s.Param)
			parts = append(parts, s.Param.Render(fmt.Sprintf("%s=%s", k, v)))
		}
	}
	if msg.InputTokens > 0 {
		parts = append(parts, s.Dim.Render(tokenChipInput(msg.InputTokens, nil)))
	}
	var content []string
	if expanded && msg.Text != "" {
		content = []string{renderStyledContent(msg.Text, s.Param, s.Content)}
	}
	return drawCanvasSpan(parts, content, s, width)
}

// renderSyntheticMessage renders a synthetic message (e.g. stream aborted, error)
// as a canvas span. When collapsed, shows only the label; when expanded, shows the body too.
func renderSyntheticMessage(msg config.Message, width int, expanded bool) string {
	s := style.SyntheticStyleLabel()
	parts := []string{
		s.Label.Render(msg.Label),
		s.Dim.Render(tokenChipOutput(msg.TextMetrics.Tokens, nil)),
	}

	if msg.Params != nil {
		for _, k := range orderedParams(msg) {
			v := styleParamValue(msg.Params[k], s.Param)
			parts = append(parts, s.Param.Render(fmt.Sprintf("%s=%s", k, v)))
		}
	}

	var content []string
	if expanded && msg.Text != "" {
		content = []string{renderStyledContent(msg.Text, s.Param, s.Content)}
	}
	return drawCanvasSpan(parts, content, s, width)
}

// renderUserMessage renders a user message as a single UserBox containing
// the header line + body text.  The header is content inside the box
// (not a DrawCanvas title part) since it has no ↳ prefix.
func renderUserMessage(msg config.Message, width int) string {
	s := style.UserStyleLabel()
	boxWidth := style.BoxWidth(width)
	inner := style.ContentWidth(width)

	leftStr := s.Dim.Render(msg.CreatedAt.Format("15:04:05"))
	var right []string
	if msg.ImagePath != "" {
		right = append(right, style.UserHeaderAttStyle.Render(msg.ImagePath))
	}
	if msg.InputTokens > 0 {
		right = append(right, s.Dim.Render(tokenChipInput(msg.InputTokens, nil)))
	}
	rightStr := strings.Join(right, s.Dim.Render("  "))
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	headerLine := leftStr + s.Dim.Render(strings.Repeat(" ", gap)) + rightStr

	return drawUserBox(nil, []string{"\n" + headerLine, msg.Text}, s, boxWidth)
}

// RenderAssistantHeader emits the assistant header as a bare canvas line
// (not a box).  Stays uncached: SequenceStat mutates while a stream is live.
func RenderAssistantHeader(start time.Time, stat *config.SequenceStat, width int) string {
	s := style.AssistantStyleLabel()
	inner := style.CanvasContentWidth(width)
	leftStr := s.Dim.Render(start.Format("15:04:05"))
	rightStr := renderSeqStatRight(stat)
	gap := inner - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
	if gap < 1 {
		gap = 1
	}
	line := leftStr + s.Dim.Render(strings.Repeat(" ", gap)) + rightStr
	return style.CanvasSpan.Width(width).Render("\n" + line)
}

// renderAssistantMessage renders an assistant message as canvas spans
// (thinking, text body) followed by one ToolBox per tool call.
func renderAssistantMessage(msg config.Message, width int, expanded bool) string {
	var b strings.Builder
	boxWidth := style.BoxWidth(width)

	if msg.ThinkingText != "" {
		s := style.ThinkingStyleLabel()
		parts := []string{
			s.Label.Render("thinking"),
			s.Dim.Render(tokenChipOutput(msg.ThinkingMetrics.Tokens, &msg.ThinkingMetrics.InferenceDuractionMs)),
		}
		var content []string
		if expanded {
			content = []string{msg.ThinkingText}
		}
		b.WriteString(drawCanvasSpan(parts, content, s, width))
	}

	if msg.Text != "" && msg.Text != "\n\n" {
		body := RenderMarkdownOnBg(msg.Text, style.P.BgApp, style.CanvasContentWidth(width)) + "\n"
		s := style.AssistantStyleLabel()
		b.WriteString(drawCanvasSpan(nil, []string{body}, s, width))
	}

	if len(msg.ToolCalls) > 0 {
		b.WriteString(renderToolCallsInline(msg.ToolCalls, boxWidth, expanded, tools.GetRegistry()))
	}

	return b.String()
}

// renderToolCallsInline renders one ToolBox per tool call. When expanded,
// the box contains the label line plus arguments, result/error, and any file diffs stacked
// inside the same box (separated by "\n").
func renderToolCallsInline(toolCalls []config.ToolCallEntry, boxWidth int, expanded bool, reg *tools.Registry) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		t := reg.Get(tc.Instruction.Name)

		var parts []string

		// Status indicator + tool name as first part, no separator between them
		var prefix string
		switch tc.Execution.Status {
		case "error":
			prefix = style.CheckError.Render("[✗] ")
		case "success":
			prefix = style.CheckSuccess.Render("[✓] ")
		case "pending":
			prefix = style.CheckWarning.Render("[?] ")
		}
		parts = append(parts, prefix+t.Style.Label.Render(tc.Instruction.Name))

		var fixedParts []string
		stats := tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs)
		if stats != "" {
			fixedParts = append(fixedParts, t.Style.Dim.Render(stats))
		}

		if display := t.DisplayValue(tc.Instruction.Arguments); display != "" {
			if idx := strings.Index(display, "\n"); idx >= 0 {
				display = display[:idx]
			}
			allOthers := append([]string{}, parts...)
			allOthers = append(allOthers, fixedParts...)
			parts = append(parts, t.Style.Param.Render(util.Truncate(display, availablePartWidth(boxWidth, allOthers))))
		}
		parts = append(parts, fixedParts...)

		var content []string
		if expanded {
			if tc.Instruction.Arguments != "" {
				content = append(content, formatArgs(tc.Instruction.Arguments, t.Style.Bg, boxWidth))
			}
			switch tc.Execution.Status {
			case "error":
				if tc.Execution.Error != "" {
					content = append(content, renderPerLine(tc.Execution.Error, t.Style.Error))
				}
				if tc.Execution.Result != "" {
					content = append(content, "Result:\n"+tc.Execution.Result)
				}
			case "success":
				if tc.Execution.Result != "" {
					content = append(content, tc.Execution.Result)
				}
			case "pending":
				if tc.Execution.Result != "" {
					content = append(content, tc.Execution.Result)
				}
			}
		}

		// Diff visible for both success and pending (with preview data)
		if (tc.Execution.Status == "success" || tc.Execution.Status == "pending") && len(tc.Execution.Files) > 0 {
			if d := renderToolFilesDiff(tc.Execution.Files, boxWidth, t.Style); d != "" {
				content = append(content, d)
			}
		}

		b.WriteString(drawToolBox(parts, content, t.Style, boxWidth))

		// Stop rendering after the first pending tool — it's the one being authorized.
		if tc.Execution.Status == "pending" {
			break
		}
	}
	return b.String()
}

// renderToolFilesDiff renders file diffs inside a tool box.
// Just the diff directly — no file name header.
func renderToolFilesDiff(files []config.FileEntry, boxWidth int, s style.StyleLabel) string {
	contentW := boxWidth - 4

	var b strings.Builder
	for i, f := range files {
		if f.Diff != "" {
			diffLines := parseDiffLines(f.Diff)
			for _, sbLine := range renderSideBySideDiffContent(diffLines, contentW, s) {
				b.WriteString(sbLine + "\n")
			}
		}

		if i < len(files)-1 {
			sepStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(style.P.TextDim)).
				Background(lipgloss.Color(s.Bg)).
				Width(contentW).Align(lipgloss.Left)
			b.WriteString(sepStyle.Render("────────") + "\n")
		}
	}
	return b.String()
}

// diffLineType represents a single diff line category.
type diffLineType int

const (
	diffContext diffLineType = iota
	diffRemove
	diffAdd
)

// diffLine holds a parsed diff line with real source line numbers (1-based).
type diffLine struct {
	typ   diffLineType
	text  string
	oldLn int
	newLn int
}

// parseDiffLines parses unified diff text into structured diffLine objects.
func parseDiffLines(diffText string) []diffLine {
	rawLines := strings.Split(diffText, "\n")
	var result []diffLine
	for _, raw := range rawLines {
		if raw == "" && len(result) > 0 {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "  - "):
			after := strings.TrimPrefix(raw, "  - ")
			parts := strings.SplitN(after, "|", 3)
			if len(parts) == 3 {
				oldNum := 0
				fmt.Sscanf(parts[0], "%d", &oldNum)
				result = append(result, diffLine{diffRemove, parts[2], oldNum, 0})
			} else {
				result = append(result, diffLine{diffRemove, after, 0, 0})
			}
		case strings.HasPrefix(raw, "  + "):
			after := strings.TrimPrefix(raw, "  + ")
			parts := strings.SplitN(after, "|", 3)
			if len(parts) == 3 {
				newNum := 0
				fmt.Sscanf(parts[1], "%d", &newNum)
				result = append(result, diffLine{diffAdd, parts[2], 0, newNum})
			} else {
				result = append(result, diffLine{diffAdd, after, 0, 0})
			}
		default:
			text := raw
			for i := 0; i < 4 && i < len(text) && text[i] == ' '; i++ {
				text = text[1:]
			}
			parts := strings.SplitN(text, "|", 3)
			if len(parts) == 3 {
				oldNum, newNum := 0, 0
				fmt.Sscanf(parts[0], "%d", &oldNum)
				fmt.Sscanf(parts[1], "%d", &newNum)
				result = append(result, diffLine{diffContext, parts[2], oldNum, newNum})
			} else {
				result = append(result, diffLine{diffContext, text, 0, 0})
			}
		}
	}
	return result
}

type sideBySidePair struct {
	left   string
	leftT  diffLineType
	right  string
	rightT diffLineType
	oldLn  int
	newLn  int
}

func pairDiffLines(lines []diffLine) []sideBySidePair {
	var pairs []sideBySidePair
	i := 0
	for i < len(lines) {
		l := lines[i]
		switch l.typ {
		case diffContext:
			pairs = append(pairs, sideBySidePair{l.text, diffContext, l.text, diffContext, l.oldLn, l.newLn})
			i++
		case diffRemove, diffAdd:
			var removes []diffLine
			var adds []diffLine
			for i < len(lines) {
				ll := lines[i]
				if ll.typ == diffRemove {
					removes = append(removes, ll)
					i++
				} else if ll.typ == diffAdd {
					adds = append(adds, ll)
					i++
				} else {
					break
				}
			}
			maxLen := len(removes)
			if len(adds) > maxLen {
				maxLen = len(adds)
			}
			for j := 0; j < maxLen; j++ {
				p := sideBySidePair{}
				if j < len(removes) {
					p.left = removes[j].text
					p.leftT = diffRemove
					p.oldLn = removes[j].oldLn
				}
				if j < len(adds) {
					p.right = adds[j].text
					p.rightT = diffAdd
					p.newLn = adds[j].newLn
				}
				pairs = append(pairs, p)
			}
		}
	}
	return pairs
}

// renderSideBySideDiffContent renders diff lines with a given content width (no margin offsets).
// Returns one styled string per pair, without newlines. bg overrides context background.
func renderSideBySideDiffContent(lines []diffLine, contentW int, s style.StyleLabel) []string {
	if len(lines) == 0 {
		return nil
	}
	pairs := pairDiffLines(lines)

	// Determine line number column width based on max real line numbers
	maxLn := 0
	for _, p := range pairs {
		if p.oldLn > maxLn {
			maxLn = p.oldLn
		}
		if p.newLn > maxLn {
			maxLn = p.newLn
		}
	}
	numWidth := 2
	if maxLn >= 100 {
		numWidth = 3
	}
	if maxLn >= 1000 {
		numWidth = 4
	}

	// contentW = numL + leftText + divider(1) + numR + rightText
	textTotal := contentW - 2*numWidth - 1
	if textTotal < 4 {
		textTotal = 4
	}
	leftW := textTotal / 2
	rightW := textTotal - leftW
	if leftW < 2 {
		leftW = 2
		rightW = 2
	}

	removeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("174")).Background(lipgloss.Color("52")).
		Width(leftW).Align(lipgloss.Left)
	addStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("144")).Background(lipgloss.Color("22")).
		Width(rightW).Align(lipgloss.Left)
	contextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextDim)).Background(lipgloss.Color(s.Bg)).
		Width(leftW).Align(lipgloss.Left)
	contextRightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextDim)).Background(lipgloss.Color(s.Bg)).
		Width(rightW).Align(lipgloss.Left)

	numStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextMuted)).Background(lipgloss.Color(s.Bg)).
		Width(numWidth).Align(lipgloss.Right)

	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextDim)).Background(lipgloss.Color(s.Bg))

	var result []string
	for _, p := range pairs {
		lText := util.Truncate(p.left, leftW-numWidth-6)
		rText := util.Truncate(p.right, rightW-numWidth-6)

		oldStr := "-"
		if p.oldLn > 0 {
			oldStr = fmt.Sprintf("%d", p.oldLn)
		}
		newStr := "-"
		if p.newLn > 0 {
			newStr = fmt.Sprintf("%d", p.newLn)
		}
		oldNum := numStyle.Render(oldStr)
		newNum := numStyle.Render(newStr)

		var leftStr string
		switch p.leftT {
		case diffRemove:
			leftStr = removeStyle.Render(lText)
		default:
			leftStr = contextStyle.Render(lText)
		}

		var rightStr string
		switch p.rightT {
		case diffAdd:
			rightStr = addStyle.Render(rText)
		default:
			rightStr = contextRightStyle.Render(rText)
		}

		// Wrap in bg-only style with full content width so trailing space gets background
		bgWrapper := lipgloss.NewStyle().Background(lipgloss.Color(s.Bg)).Width(contentW)
		result = append(result, bgWrapper.Render(oldNum+leftStr+dividerStyle.Render("│")+newNum+rightStr))
	}
	return result
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
	boxWidth := style.BoxWidth(width)

	if data.Waiting {
		elapsed := time.Since(data.RequestStart)
		s := style.ThinkingStyleLabel()
		parts := []string{
			s.Label.Render("waiting"),
			s.Dim.Render(formatDuration(elapsed.Milliseconds())),
		}
		b.WriteString(drawCanvasSpan(parts, nil, s, width))
	}

	if data.ThinkingText != "" || data.InThinking {
		dur := data.ThinkingDur.Milliseconds()
		s := style.ThinkingStyleLabel()
		parts := []string{
			s.Label.Render("thinking"),
			s.Dim.Render(tokenChipOutput(data.ThinkingTokens, &dur)),
		}
		var content []string
		if data.Expanded {
			if data.ThinkingText != "" {
				content = []string{data.ThinkingText}
			} else {
				content = []string{"..."}
			}
		}
		b.WriteString(drawCanvasSpan(parts, content, s, width))
	}

	if data.RenderedMarkdown != "" || data.Partial != "" {
		var body string
		if data.RenderedMarkdown != "" {
			body = data.RenderedMarkdown
		}
		if data.Partial != "" {
			wrappedPartial := RenderMarkdownOnBg(data.Partial, style.P.BgApp, style.CanvasContentWidth(data.Width))
			if body != "" {
				body = body + "\n" + wrappedPartial
			} else {
				body = wrappedPartial
			}
		}
		s := style.AssistantStyleLabel()
		b.WriteString(drawCanvasSpan(nil, []string{body}, s, width))
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
	s := style.AssistantStyleLabel()
	var parts []string
	if stat.AvgTokensPerSec > 0 {
		parts = append(parts, s.Dim.Render(fmt.Sprintf("%.1f tok/s", stat.AvgTokensPerSec)))
	}
	var execDur *int64
	if stat.InputTokens > 0 {
		execDur = &stat.ExecDurMs
	}
	if chip := tokenChipBoth(stat.OutputTokens, stat.InputTokens, &stat.DurationMs, execDur); chip != "" {
		parts = append(parts, s.Dim.Render(chip))
	}
	return strings.Join(parts, s.Dim.Render("  "))
}

// renderStreamingToolCalls renders pending tool calls during streaming.
func renderStreamingToolCalls(pendingTools []StreamingToolCall, boxWidth int, expanded bool) string {
	var b strings.Builder
	reg := tools.GetRegistry()
	for _, tc := range pendingTools {
		t := reg.Get(tc.Name)

		var parts []string

		// Streaming indicator + tool name as first part, no separator
		parts = append(parts, t.Style.Dim.Render("[ ] ")+t.Style.Label.Render(tc.Name))

		var fixedParts []string
		if tc.Tokens > 0 || tc.Duration > 0 {
			dur := tc.Duration.Milliseconds()
			fixedParts = append(fixedParts, t.Style.Dim.Render(tokenChipOutput(tc.Tokens, &dur)))
		}

		if display := t.DisplayValue(tc.Arguments); display != "" {
			if idx := strings.Index(display, "\n"); idx >= 0 {
				display = display[:idx]
			}
			allOthers := append([]string{}, parts...)
			allOthers = append(allOthers, fixedParts...)
			parts = append(parts, t.Style.Param.Render(util.Truncate(display, availablePartWidth(boxWidth, allOthers))))
		}
		parts = append(parts, fixedParts...)

		var content []string
		if expanded && tc.Arguments != "" {
			content = []string{tc.Arguments}
		}

		b.WriteString(drawToolBox(parts, content, t.Style, boxWidth))
	}
	return b.String()
}

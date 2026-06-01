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
// the box contains the label line plus arguments and result/error stacked
// inside the same box (separated by "\n"). File tracking renders as a
// separate green canvas span after each tool box.
func renderToolCallsInline(toolCalls []config.ToolCallEntry, boxWidth int, expanded bool, reg *tools.Registry) string {
	var b strings.Builder
	for _, tc := range toolCalls {
		t := reg.Get(tc.Instruction.Name)

		var parts []string
		parts = append(parts, t.Style.Label.Render(tc.Instruction.Name))
		if display := t.DisplayValue(tc.Instruction.Arguments); display != "" {
			parts = append(parts, t.Style.Param.Render(util.Truncate(display, 60)))
		}

		switch tc.Execution.Status {
		case "error":
			parts = append(parts, style.CheckError.Render("[✗]"))
		case "success":
			parts = append(parts, style.CheckSuccess.Render("[✓]"))
		}

		stats := tokenChipBoth(tc.Instruction.Tokens, tc.Execution.Tokens, &tc.Instruction.DurationMs, &tc.Execution.DurationMs)
		if stats != "" {
			parts = append(parts, t.Style.Dim.Render(stats))
		}

		// File tracking renders as a separate green canvas span after the tool box.
		if len(tc.Execution.Files) > 0 {
			chip := fmt.Sprintf("%d file(s)", len(tc.Execution.Files))
			parts = append(parts, t.Style.Dim.Render(chip))
		}

		var content []string
		if expanded {
			if tc.Instruction.Arguments != "" {
				content = append(content, util.StripNewlines(tc.Instruction.Arguments))
			}
			switch tc.Execution.Status {
			case "error":
				if tc.Execution.Error != "" {
					content = append(content, t.Style.Error.Render(tc.Execution.Error))
				}
				if tc.Execution.Result != "" {
					content = append(content, "Result:\n"+tc.Execution.Result)
				}
			case "success":
				if tc.Execution.Result != "" {
					content = append(content, t.Style.Content.Render(tc.Execution.Result))
				}
			}
		}

		b.WriteString(drawToolBox(parts, content, t.Style, boxWidth))

		// File tracking renders as a separate green canvas span after the tool box.
		if len(tc.Execution.Files) > 0 {
			b.WriteString(renderFilesBox(tc.Execution.Files, boxWidth))
		}
	}
	return b.String()
}

// filesStyle returns a green-tinted style label for file tracking display.
var filesStyle = func() style.StyleLabel {
	bg := lipgloss.Color(style.P.BgApp)
	return style.StyleLabel{
		Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextSuccess)),
		Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextSuccess)),
		Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextDim)),
		Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextSuccess)),
		Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextError)),
		Bg:      style.P.BgApp,
		Fg:      style.P.TextSuccess,
	}
}()

// renderFilesBox renders a green-tinted box showing affected files and their diffs.
func renderFilesBox(files []config.FileEntry, boxWidth int) string {
	s := filesStyle

	var parts []string
	parts = append(parts, s.Label.Render("files"))
	parts = append(parts, s.Dim.Render(fmt.Sprintf("%d file(s)", len(files))))
	header := drawCanvasSpan(parts, nil, s, boxWidth)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")

	for i, f := range files {
		checksum := f.Checksum
		if len(checksum) > 12 {
			checksum = checksum[:12] + "…"
		}
		line := fmt.Sprintf("  %s (%s) %s", f.Path, f.Trace, checksum)
		// Apply arch pattern: style with full background width so no transparency after checksum
		fullLineW := boxWidth + 2*style.BoxMargin
		lineStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(style.P.TextSuccess)).
			Background(lipgloss.Color(style.P.BgApp)).
			Width(fullLineW).Align(lipgloss.Left)
		b.WriteString(lineStyle.Render(line) + "\n")

		if f.Diff != "" {
			diffLines := parseDiffLines(f.Diff)
			for _, sbLine := range renderSideBySideDiff(diffLines, boxWidth, s) {
				b.WriteString(sbLine + "\n")
			}
		}

		if i < len(files)-1 {
			fullLineW := boxWidth + 2*style.BoxMargin
			sepStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(style.P.TextDim)).
				Background(lipgloss.Color(style.P.BgApp)).
				Width(fullLineW).Align(lipgloss.Left)
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
	oldLn int // 1-based line number in old file (0 if not applicable)
	newLn int // 1-based line number in new file (0 if not applicable)
}

// parseDiffLines parses unified diff text into structured diffLine objects.
// Diff lines embed real line numbers as "OLD|NEW|text" (context), "OLD|-|text" (remove),
// or "-|NEW|text" (add). Line numbers are 1-based.
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

// sideBySidePair holds a matched left/right line for side-by-side rendering.
// oldLn and newLn are the real 1-based source line numbers.
type sideBySidePair struct {
	left   string
	leftT  diffLineType
	right  string
	rightT diffLineType
	oldLn  int // 1-based line number in old file (left column)
	newLn  int // 1-based line number in new file (right column)
}

// pairDiffLines groups diff lines into left/right pairs for side-by-side display.
// Context lines appear on both sides. Consecutive removes and adds are paired.
// Real source line numbers (1-based) are preserved from the diff text.
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

// renderSideBySideDiff renders diff lines as two columns (left=red, right=green) with a gray divider.
// Returns one styled string per pair, without newlines.
func renderSideBySideDiff(lines []diffLine, boxWidth int, s style.StyleLabel) []string {
	if len(lines) == 0 {
		return nil
	}
	pairs := pairDiffLines(lines)

	// Match the header's full rendered width: DrawCanvas adds Margin(BoxMargin) outside Width
	fullLineW := boxWidth + 2*style.BoxMargin - 2

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

	// fullLineW = numL + leftText + divider(1) + numR + rightText
	textTotal := fullLineW - 2*numWidth - 1
	if textTotal < 4 {
		textTotal = 4
	}
	leftW := textTotal / 2
	rightW := textTotal - leftW
	if leftW < 2 {
		leftW = 2
		rightW = 2
	}

	// Styles — colors the user set
	removeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("174")).Background(lipgloss.Color("52")).
		Width(leftW).Align(lipgloss.Left)
	addStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("144")).Background(lipgloss.Color("22")).
		Width(rightW).Align(lipgloss.Left)
	contextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextDim)).Background(lipgloss.Color(style.P.BgApp)).
		Width(leftW).Align(lipgloss.Left)
	contextRightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextDim)).Background(lipgloss.Color(style.P.BgApp)).
		Width(rightW).Align(lipgloss.Left)

	// Line number style — right-aligned, visible bg/fg
	numStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextMuted)).Background(lipgloss.Color(style.P.BgApp)).
		Width(numWidth).Align(lipgloss.Right)

	// Divider always uses context bg
	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(style.P.TextDim)).Background(lipgloss.Color(style.P.BgApp))

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

		// numL + leftText + divider + numR + rightText
		// Wrap in bg-only style with full width so trailing space gets background (arch pattern)
		fullLineW := boxWidth + 2*style.BoxMargin
		bgWrapper := lipgloss.NewStyle().Background(lipgloss.Color(style.P.BgApp)).Width(fullLineW)
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
		parts = append(parts, t.Style.Label.Render(tc.Name))
		if display := t.DisplayValue(tc.Arguments); display != "" {
			parts = append(parts, t.Style.Param.Render(util.Truncate(display, 60)))
		}
		if tc.Tokens > 0 || tc.Duration > 0 {
			dur := tc.Duration.Milliseconds()
			parts = append(parts, t.Style.Dim.Render(tokenChipOutput(tc.Tokens, &dur)))
		}

		var content []string
		if expanded && tc.Arguments != "" {
			content = []string{t.Style.Content.Render(tc.Arguments)}
		}

		b.WriteString(drawToolBox(parts, content, t.Style, boxWidth))
	}
	return b.String()
}

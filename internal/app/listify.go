package app

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// numberRegex matches lines starting with "N>" followed by optional space.
var numberRegex = regexp.MustCompile(`^(\d+)>\s?`)

// getPrefixLen returns the rune length of the number prefix on a line.
func getPrefixLen(line string) int {
	content := stripNumber(line)
	return len([]rune(line)) - len([]rune(content))
}

// logicalCursorColumn returns the cursor's rune offset in its full logical
// line. LineInfo.CharOffset is only the terminal-cell offset within the current
// soft-wrapped visual row, so it must not be used to split textarea content.
func logicalCursorColumn(ta textarea.Model) int {
	info := ta.LineInfo()
	return info.StartColumn + info.ColumnOffset
}

// listify starts or continues a local numbered block.
//
// Plain text never becomes part of a list implicitly:
//   - On a non-empty plain line, Tab inserts "1> " on the following line.
//   - On an empty plain line, Tab turns that line into "1> ".
//
// On a numbered line, Tab keeps the existing split/append behavior and
// renumbers only the contiguous numbered block containing that line. Separate
// lists therefore restart at 1 and prose between lists remains untouched.
func listify(ta textarea.Model) textarea.Model {
	value := ta.Value()
	lines := splitLines(value)
	cursorLine := clampLine(ta.Line(), len(lines))
	cursorCol := logicalCursorColumn(ta)

	if !numberRegex.MatchString(lines[cursorLine]) {
		return startLocalList(ta, lines, cursorLine)
	}
	return continueLocalList(ta, lines, cursorLine, cursorCol)
}

func startLocalList(ta textarea.Model, lines []string, cursorLine int) textarea.Model {
	if strings.TrimSpace(lines[cursorLine]) == "" {
		if cursorLine > 0 && numberRegex.MatchString(lines[cursorLine-1]) {
			blockStart, _ := numberedBlock(lines, cursorLine-1)
			for i := blockStart; i < cursorLine; i++ {
				lines[i] = fmtNumbered(i-blockStart+1, stripNumber(lines[i]))
			}
			lines[cursorLine] = fmtNumbered(cursorLine-blockStart+1, "")
		} else {
			lines[cursorLine] = fmtNumbered(1, "")
		}
		return setTextareaValueAndCursor(ta, lines, cursorLine, getPrefixLen(lines[cursorLine]))
	}

	insertAt := cursorLine + 1
	lines = insertLine(lines, insertAt, fmtNumbered(1, ""))
	return setTextareaValueAndCursor(ta, lines, insertAt, getPrefixLen(lines[insertAt]))
}

func continueLocalList(ta textarea.Model, lines []string, cursorLine, rawCursorCol int) textarea.Model {
	blockStart, blockEnd := numberedBlock(lines, cursorLine)
	line := lines[cursorLine]
	prefixLen := getPrefixLen(line)
	content := stripNumber(line)
	contentRunes := []rune(content)
	cursorCol := rawCursorCol - prefixLen
	if cursorCol < 0 {
		cursorCol = 0
	}
	if cursorCol > len(contentRunes) {
		cursorCol = len(contentRunes)
	}

	// Normalize the existing block to raw content before inserting. This avoids
	// confusing literal item content such as "2> example" with another prefix.
	for i := blockStart; i <= blockEnd; i++ {
		lines[i] = stripNumber(lines[i])
	}

	insertAt := cursorLine + 1
	if cursorCol == 0 && strings.TrimSpace(content) != "" {
		// At the content start, insert a blank item before this item.
		insertAt = cursorLine
		lines = insertLine(lines, insertAt, "")
		blockEnd++
	} else if cursorCol < len(contentRunes) {
		// In the middle, keep the prefix on this item and move the suffix to
		// the next item.
		lines[cursorLine] = string(contentRunes[:cursorCol])
		lines = insertLine(lines, insertAt, string(contentRunes[cursorCol:]))
		blockEnd++
	} else {
		// At the end (including an empty item), append a blank item.
		lines[cursorLine] = content
		lines = insertLine(lines, insertAt, "")
		blockEnd++
	}

	// Renumber only this contiguous block.
	for i := blockStart; i <= blockEnd; i++ {
		lines[i] = fmtNumbered(i-blockStart+1, lines[i])
	}

	return setTextareaValueAndCursor(ta, lines, insertAt, getPrefixLen(lines[insertAt]))
}

func numberedBlock(lines []string, at int) (start, end int) {
	start, end = at, at
	for start > 0 && numberRegex.MatchString(lines[start-1]) {
		start--
	}
	for end+1 < len(lines) && numberRegex.MatchString(lines[end+1]) {
		end++
	}
	return start, end
}

func insertLine(lines []string, at int, line string) []string {
	lines = append(lines, "")
	copy(lines[at+1:], lines[at:])
	lines[at] = line
	return lines
}

func setTextareaValueAndCursor(ta textarea.Model, lines []string, line, col int) textarea.Model {
	ta.SetValue(strings.Join(lines, "\n"))
	for ta.Line() < line {
		ta.CursorDown()
	}
	for ta.Line() > line {
		ta.CursorUp()
	}
	ta.SetCursor(col)
	return ta
}

func clampLine(line, count int) int {
	if line < 0 {
		return 0
	}
	if line >= count {
		return count - 1
	}
	return line
}

// stripNumber removes the "N> " prefix from a line, returning the content after it.
func stripNumber(line string) string {
	matches := numberRegex.FindStringSubmatch(line)
	if matches != nil {
		return line[len(matches[0]):]
	}
	return line
}

// fmtNumbered formats a line with an "N> " prefix.
func fmtNumbered(num int, content string) string {
	return strconv.Itoa(num) + "> " + content
}

// applyListify applies local list behavior to the model's textarea.
func (m *Model) applyListify() (tea.Model, tea.Cmd) {
	m.textarea = listify(m.textarea)
	m.autoSizeTextarea()
	m.recalcLayout()
	return m, nil
}

// splitLines preserves all logical lines, including a trailing empty line.
func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

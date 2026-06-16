package app

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// numberRegex matches lines starting with "N>" followed by optional space
var numberRegex = regexp.MustCompile(`^(\d+)>\s?`)

// listify applies sequential numbering to the textarea content.
//
// If all lines are already properly numbered in sequence (1>, 2>, 3>...)
// with no missing prefixes, just renumber — no new lines inserted.
// Otherwise, insert a new numbered line based on cursor position:
//
//   - Cursor on empty line (or line with only whitespace):
//     If it's the last line → append a new numbered item below.
//     Otherwise → insert a numbered line here, renumber everything below.
//
//   - Cursor on a line with content:
//     If cursor is at column 0 → insert new numbered line before this one, push content down.
//     If cursor is at end of line → keep this line numbered, append new numbered line below.
//     If cursor is mid-line → split at cursor, prefix stays on current number,
//     suffix moves to next number.
//
//   - All lines are renumbered sequentially 1>, 2>, 3> regardless of prior state.
//   - If a line lost its number prefix (user deleted it), it gets a new one in sequence.
//
// Returns the new textarea model with updated value and cursor position.
func listify(ta textarea.Model) textarea.Model {
	value := ta.Value()
	cursorLine := ta.Line()               // 0-based logical line index
	cursorCol := ta.LineInfo().CharOffset // cursor position within the (possibly wrapped) line

	lines := splitLines(value)
	totalLines := len(lines)

	// Clamp cursor
	if cursorLine < 0 {
		cursorLine = 0
	}
	if cursorLine >= totalLines {
		cursorLine = totalLines - 1
	}

	// Check if all lines are already properly numbered in sequence.
	// If so, just renumber without inserting anything.
	allNumbered := len(lines) > 0
	if allNumbered {
		for i, line := range lines {
			expected := strconv.Itoa(i+1) + "> "
			if !strings.HasPrefix(line, expected) && stripNumber(line) != line {
				// Has some number prefix but wrong — needs fix
				allNumbered = false
				break
			}
			if strings.HasPrefix(line, expected) {
				continue
			}
			// Line doesn't have a "N> " prefix at all
			if numberRegex.MatchString(line) {
				// Has a number prefix but not the expected one
				allNumbered = false
				break
			}
			// No number prefix — could be a plain content line or empty
			// If there's actual content without a prefix, it's not properly numbered
			if strings.TrimSpace(line) != "" {
				allNumbered = false
				break
			}
		}
	}

	if allNumbered {
		// Just renumber — no new lines
		var result []string
		for i, line := range lines {
			result = append(result, fmtNumbered(i+1, stripNumber(line)))
		}
		newValue := strings.Join(result, "\n")
		ta.SetValue(newValue)
		// Restore cursor position
		placeCursor(ta, result, cursorLine, cursorCol)
		return ta
	}

	// Get the raw content of the cursor line (strip number prefix if present)
	cursorText := lines[cursorLine]
	cursorContent := stripNumber(cursorText)
	contentLen := len([]rune(cursorContent))

	// Clamp column to content length
	if cursorCol < 0 {
		cursorCol = 0
	}
	if cursorCol > contentLen {
		cursorCol = contentLen
	}

	var result []string
	nextNum := 1
	newCursorLine := cursorLine // default: cursor stays on same line
	newCursorCol := 0           // cursor column within the new target line

	for i, line := range lines {
		content := stripNumber(line)

		if i == cursorLine {
			if cursorCol == 0 {
				// Cursor at beginning of line
				if strings.TrimSpace(content) == "" {
					// Empty line: insert a new empty numbered line here
					result = append(result, fmtNumbered(nextNum, ""))
					nextNum++
					newCursorLine = i
					newCursorCol = 0 // cursor at end of empty content
				} else {
					// Content exists: insert empty numbered line BEFORE this content
					result = append(result, fmtNumbered(nextNum, ""))
					nextNum++
					// Original content becomes next line
					result = append(result, fmtNumbered(nextNum, content))
					nextNum++
					newCursorLine = i // cursor on the empty line we just inserted
					newCursorCol = 0
				}
			} else if cursorCol >= contentLen {
				// Cursor at end of line content
				result = append(result, fmtNumbered(nextNum, content))
				nextNum++
				// Append new empty numbered line below
				result = append(result, fmtNumbered(nextNum, ""))
				nextNum++
				newCursorLine = i + 1
				newCursorCol = 0
			} else {
				// Mid-line split
				runes := []rune(content)
				before := string(runes[:cursorCol])
				after := string(runes[cursorCol:])

				result = append(result, fmtNumbered(nextNum, before))
				nextNum++
				newCursorLine = i // cursor stays on the "before" part
				newCursorCol = len([]rune(before))

				if after != "" {
					result = append(result, fmtNumbered(nextNum, after))
				} else {
					result = append(result, fmtNumbered(nextNum, ""))
				}
				nextNum++
			}
		} else {
			// Renumber every other line sequentially
			result = append(result, fmtNumbered(nextNum, content))
			nextNum++
		}
	}

	// Build new value
	newValue := strings.Join(result, "\n")

	// Apply to textarea
	ta.SetValue(newValue)

	// Move cursor to the target line
	placeCursor(ta, result, newCursorLine, newCursorCol)

	return ta
}

// placeCursor positions the cursor on the given line at the given content column
// (after the "N> " prefix).
func placeCursor(ta textarea.Model, result []string, targetLine int, cursorCol int) {
	if targetLine < 0 {
		targetLine = 0
	}
	if targetLine >= len(result) {
		targetLine = len(result) - 1
	}
	for ta.Line() < targetLine {
		ta.CursorDown()
	}
	for ta.Line() > targetLine {
		ta.CursorUp()
	}
	targetLineRaw := result[targetLine]
	targetContent := stripNumber(targetLineRaw)
	prefixLen := len([]rune(targetLineRaw)) - len([]rune(targetContent))
	ta.SetCursor(prefixLen + cursorCol)
}

// stripNumber removes the "N> " prefix from a line, returning the content after it.
func stripNumber(line string) string {
	matches := numberRegex.FindStringSubmatch(line)
	if matches != nil {
		return line[len(matches[0]):]
	}
	return line
}

// fmtNumbered formats a line with "N> content" prefix.
func fmtNumbered(num int, content string) string {
	return strconv.Itoa(num) + "> " + content
}

// applyListify applies listify to the model's textarea.
func (m *Model) applyListify() (tea.Model, tea.Cmd) {
	m.textarea = listify(m.textarea)
	m.autoSizeTextarea()
	m.recalcLayout()
	return m, nil
}

// splitLines splits a string into lines without a trailing empty element.
func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty element from a trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

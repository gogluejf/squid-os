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

// getPrefixLen returns the rune length of the number prefix on a line.
func getPrefixLen(line string) int {
	content := stripNumber(line)
	return len([]rune(line)) - len([]rune(content))
}

// listify applies sequential numbering to the textarea content.
//
// Behavior depends on cursor position:
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
//
//   - If a line lost its number prefix (user deleted it), it gets a new one in sequence.
//
// Returns the new textarea model with updated value and cursor position.
func listify(ta textarea.Model) textarea.Model {
	value := ta.Value()
	cursorLine := ta.Line()
	rawCursorCol := ta.LineInfo().CharOffset // raw column including prefix

	lines := splitLines(value)
	totalLines := len(lines)

	// Clamp cursor line
	if cursorLine < 0 {
		cursorLine = 0
	}
	if cursorLine >= totalLines {
		cursorLine = totalLines - 1
	}

	// Convert raw cursor column to content-relative column by subtracting prefix length
	cursorText := lines[cursorLine]
	prefixLen := getPrefixLen(cursorText)
	cursorContent := stripNumber(cursorText)
	contentLen := len([]rune(cursorContent))

	cursorCol := rawCursorCol - prefixLen
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
	newCursorCol := 0           // cursor column within the new target line (content-relative)

	for i, line := range lines {
		content := stripNumber(line)

		if i == cursorLine {
			if cursorCol == 0 {
				// Cursor at beginning of line content
				if strings.TrimSpace(content) == "" {
					// Empty line: if the textarea was completely empty (single line,
					// no content anywhere), just produce one numbered line.
					// Otherwise, keep this line and append a new numbered line below.
					if totalLines == 1 && strings.TrimSpace(value) == "" {
						result = append(result, fmtNumbered(nextNum, ""))
						nextNum++
						newCursorLine = i
						newCursorCol = 0
					} else {
						result = append(result, fmtNumbered(nextNum, ""))
						nextNum++
						result = append(result, fmtNumbered(nextNum, ""))
						nextNum++
						newCursorLine = i + 1
						newCursorCol = 0
					}
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
				// Mid-line split: cursor moves to beginning of the "after" part on next line
				runes := []rune(content)
				before := string(runes[:cursorCol])
				after := string(runes[cursorCol:])

				result = append(result, fmtNumbered(nextNum, before))
				nextNum++

				if after != "" {
					result = append(result, fmtNumbered(nextNum, after))
				} else {
					result = append(result, fmtNumbered(nextNum, ""))
				}
				nextNum++
				newCursorLine = i + 1 // cursor on the new "after" line
				newCursorCol = 0      // at the beginning of the content
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
	// After SetValue, the textarea resets to row 0, col 0.
	if newCursorLine < 0 {
		newCursorLine = 0
	}
	if newCursorLine >= len(result) {
		newCursorLine = len(result) - 1
	}

	// Move to target line by calling CursorDown/Up
	for ta.Line() < newCursorLine {
		ta.CursorDown()
	}
	for ta.Line() > newCursorLine {
		ta.CursorUp()
	}

	// Set cursor column: prefix length + content-relative offset
	targetLine := result[newCursorLine]
	targetPrefixLen := getPrefixLen(targetLine)
	ta.SetCursor(targetPrefixLen + newCursorCol)

	return ta
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

// isNumberingBroken checks if the textarea has a broken numbering sequence —
// meaning some lines have number prefixes and some don't, or the sequence is
// out of order (e.g. 1>, 3>, or a line with its prefix deleted).
//
// Returns false (not broken) when:
//   - No lines have any number prefix at all (plain text, normal listify applies).
//   - All lines are already correctly numbered sequentially.
//
// Returns true (broken) when:
//   - At least one line has a prefix and at least one doesn't (mixed).
//   - The prefixes exist but are out of sequence.
func isNumberingBroken(ta textarea.Model) bool {
	value := ta.Value()
	if value == "" {
		return false
	}
	lines := splitLines(value)

	hasPrefix := 0
	noPrefix := 0
	outOfOrder := false

	for i, line := range lines {
		if numberRegex.MatchString(line) {
			hasPrefix++
			content := stripNumber(line)
			reconstructed := fmtNumbered(i+1, content)
			if line != reconstructed {
				outOfOrder = true
			}
		} else {
			noPrefix++
		}
	}

	// All lines lack prefixes → not broken, just plain text.
	if hasPrefix == 0 {
		return false
	}

	// All lines have correct sequential prefixes → not broken.
	if noPrefix == 0 && !outOfOrder {
		return false
	}

	// Mixed: some have prefixes, some don't → broken.
	// Or out of order → broken.
	return true
}

// renumberInPlace renumbers all lines sequentially without adding or removing lines,
// and preserves the cursor position exactly.
func renumberInPlace(ta textarea.Model) textarea.Model {
	value := ta.Value()
	cursorLine := ta.Line()
	rawCursorCol := ta.LineInfo().CharOffset

	lines := splitLines(value)

	// Clamp cursor
	if cursorLine < 0 {
		cursorLine = 0
	}
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}

	// Convert raw cursor column to content-relative
	cursorPrefixLen := getPrefixLen(lines[cursorLine])
	cursorCol := rawCursorCol - cursorPrefixLen
	cursorContent := stripNumber(lines[cursorLine])
	contentLen := len([]rune(cursorContent))
	if cursorCol < 0 {
		cursorCol = 0
	}
	if cursorCol > contentLen {
		cursorCol = contentLen
	}

	// Renumber every line in place
	var result []string
	for i, line := range lines {
		content := stripNumber(line)
		result = append(result, fmtNumbered(i+1, content))
	}

	newValue := strings.Join(result, "\n")
	ta.SetValue(newValue)

	// Restore cursor to the same line
	if cursorLine >= len(result) {
		cursorLine = len(result) - 1
	}

	for ta.Line() < cursorLine {
		ta.CursorDown()
	}
	for ta.Line() > cursorLine {
		ta.CursorUp()
	}

	// Set cursor: new prefix length + content-relative offset
	targetPrefixLen := getPrefixLen(result[cursorLine])
	ta.SetCursor(targetPrefixLen + cursorCol)

	return ta
}

// applyListify applies listify to the model's textarea.
// If numbering is broken (out of order or missing prefixes), Tab simply
// fixes the numbering in place — no new lines, no cursor movement.
// On the next Tab press, numbering will be correct and normal behavior resumes.
// Otherwise, it runs full listify for normal insert/split behavior.
func (m *Model) applyListify() (tea.Model, tea.Cmd) {
	ta := m.textarea

	if isNumberingBroken(ta) {
		// Just fix the numbering in place, preserve cursor exactly
		ta = renumberInPlace(ta)
	} else {
		ta = listify(ta)
	}

	m.textarea = ta
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

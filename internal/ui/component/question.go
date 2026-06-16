package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

const questionPlaceholder = "add instructions to your answer..."

type Question struct {
	Title       string
	Description string // dim text shown under title (optional)
	Options     []string
	Selection   int
	ShowInput   bool
	TextInput   string
	TextMode    bool // true = typing instructions
	cur         cursor.Model
	initialized bool
	OnConfirm   func(int, string, any) tea.Cmd
	OnCancel    func(any) tea.Cmd
}

func (q *Question) Init(any) {
	if q.initialized {
		return
	}
	q.initialized = true

	if q.Selection < 0 {
		q.Selection = 0
	}
	if q.Selection >= len(q.Options) {
		q.Selection = len(q.Options) - 1
	}
	q.cur = cursor.New()
	q.cur.Blink = false
	q.cur.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextAccent)).Background(lipgloss.Color(style.P.BgFooter))
	q.cur.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextMuted)).Background(lipgloss.Color(style.P.BgFooter))
	q.cur.Focus()
	q.syncCursorChar()
}

func (q *Question) BlinkCmd() tea.Cmd {
	return q.cur.BlinkCmd()
}

func (q *Question) RenderHeight() int {
	return PickerMaxItems + 4
}

// Update handles tick messages for cursor blinking.
func (q *Question) Update(msg tea.Msg, ctx any) tea.Cmd {
	if _, ok := msg.(tea.KeyMsg); ok {
		return nil
	}
	newCur, cmd := q.cur.Update(msg)
	q.cur = newCur
	return cmd
}

func (q *Question) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	s := msg.String()

	if q.TextMode {
		// --- TEXT MODE ---
		switch {
		case s == "shift+tab":
			q.TextMode = false
		case s == "esc":
			q.TextMode = false
		case s == "ctrl+c":
			q.TextMode = false
		case s == "tab":
			q.Selection = (q.Selection + 1) % len(q.Options)
		case s == "up":
			if q.Selection > 0 {
				q.Selection--
			}
		case s == "down":
			if q.Selection < len(q.Options)-1 {
				q.Selection++
			}
		case s == "enter":
			if q.OnConfirm != nil {
				return q.OnConfirm(q.Selection, q.TextInput, ctx)
			}
		case s == "backspace":
			if len(q.TextInput) > 0 {
				runes := []rune(q.TextInput)
				q.TextInput = string(runes[:len(runes)-1])
			}
			return q.resetBlink()
		case s == "space":
			q.TextInput += " "
			return q.resetBlink()
		case msg.Type == tea.KeyRunes && len(msg.Runes) > 1:
			// Paste event — insert all runes at once
			q.TextInput += string(msg.Runes)
			return q.resetBlink()
		case len(s) == 1 && isPrintable(s):
			q.TextInput += s
			return q.resetBlink()
		}
		return nil
	}

	// --- SELECTION MODE ---
	switch {
	case s == "tab":
		q.TextMode = true
	case s == "up":
		if q.Selection > 0 {
			q.Selection--
		}
	case s == "down":
		if q.Selection < len(q.Options)-1 {
			q.Selection++
		}
	case s == "left":
		if q.Selection > 0 {
			q.Selection--
		}
	case s == "right":
		if q.Selection < len(q.Options)-1 {
			q.Selection++
		}
	case s == "enter":
		if q.OnConfirm != nil {
			// Confirm from selection mode — send empty instructions
			return q.OnConfirm(q.Selection, "", ctx)
		}
	case s == "esc":
		if q.OnCancel != nil {
			return q.OnCancel(ctx)
		}
	case s == "ctrl+c":
		if q.OnCancel != nil {
			return q.OnCancel(ctx)
		}
	case len(s) == 1 && isPrintable(s) && '1' <= s[0] && s[0] <= '9':
		idx := int(s[0] - '1')
		if idx < len(q.Options) {
			q.Selection = idx
		}
	}

	return nil
}

func (q *Question) syncCursorChar() {
	if q.TextInput == "" {
		q.cur.SetChar(string([]rune(questionPlaceholder)[:1]))
	} else {
		q.cur.SetChar(" ")
	}
}

func (q *Question) resetBlink() tea.Cmd {
	q.syncCursorChar()
	q.cur.Blink = false
	return q.cur.BlinkCmd()
}

// truncateDescription truncates the description to fit within the terminal width,
// stopping at the first newline in the source text.
// Returns the full line: "   ↳ " + truncated text.
func (q *Question) truncateDescription(text string, width int) string {
	// Stop at first newline
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = text[:idx] + "\\n" //so it is clear to users that there was a newline and the text is truncated, instead of just cut off mid-line
	}
	prefix := "   ↳ "
	prefixW := lipgloss.Width(prefix)
	// Available display width for the description text (leave room for "..." and 3-char right margin)
	avail := width - prefixW - 3 - 3
	if avail < 4 {
		avail = 4
	}

	// Truncate by display width using lipgloss.Width
	if lipgloss.Width(text) <= avail {
		return prefix + text
	}

	// Walk runes, tracking display width, until we hit the limit
	runes := []rune(text)
	var result []rune
	totalW := 0
	for _, r := range runes {
		chW := lipgloss.Width(string(r))
		if totalW+chW > avail {
			break
		}
		totalW += chW
		result = append(result, r)
	}
	result = append(result, '.', '.', '.')
	return prefix + string(result)
}

// Render draws the question overlay.
func (q *Question) Render(width int) string {
	bg := lipgloss.Color(style.P.BgFooter)

	var lines []string

	// Leading blank line
	lines = append(lines, " ")

	// Title
	lines = append(lines, lipgloss.NewStyle().Background(bg).Render(style.HeadingStyle.Render("   "+q.Title)))

	// Description (dim, optional) — separated from title by a blank line
	if q.Description != "" {
		lines = append(lines, " ")
		lines = append(lines, lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render(q.truncateDescription(q.Description, width)))
	}

	// Separator blank line
	lines = append(lines, " ")

	// Option rows
	for i, opt := range q.Options {
		num := i + 1
		label := fmt.Sprintf("%d. %s", num, opt)
		if i == q.Selection {
			lines = append(lines, lipgloss.NewStyle().
				Background(lipgloss.Color(style.P.BgSelected)).
				Foreground(lipgloss.Color(style.P.TextAccent)).
				Bold(true).
				Width(width).
				Render(" \u25B6 "+label))
		} else {
			lines = append(lines, lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color(style.P.TextMuted)).
				Render("   "+label))
		}
	}

	// Pad options
	for i := len(q.Options); i < PickerMaxItems/2; i++ {
		lines = append(lines, " ")
	}

	// Blank separator before instruction area
	lines = append(lines, " ")

	// Pad to push instruction to the very bottom (before trailing blank)
	remaining := (PickerMaxItems + 1) - len(lines) - 1 // -1 for instruction line
	for i := 0; i < remaining; i++ {
		lines = append(lines, " ")
	}

	// Instruction line — always at the bottom
	if q.ShowInput {
		label := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("  Instruction: ")
		if q.TextMode {
			// Show cursor + typed text (or placeholder)
			if q.TextInput != "" {
				lines = append(lines, label+
					lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextAccent)).Render(q.TextInput)+
					q.cur.View())
			} else {
				lines = append(lines, label+
					q.cur.View()+
					lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render(questionPlaceholder[1:]))
			}
		} else {
			// Selection mode — show hint
			lines = append(lines, lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("  [Tab] to add note or instruction to your answer"))
		}
	}

	// Trailing blank line
	lines = append(lines, " ")

	return lipgloss.NewStyle().
		Background(bg).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

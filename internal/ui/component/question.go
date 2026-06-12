package component

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

// Question is a multi-option selection overlay with a simple instruction input line.
// Options render like Picker rows (with arrow indicator).
//
// Navigation: Up/Down/Left/Right for options, Tab cycles forward, Enter confirms.
// Ctrl+C or Esc clears instruction text (or cancels if empty).
// Any other key types into the instruction field.
type Question struct {
	Title       string
	Options     []string
	Selection   int
	ShowInput   bool
	TextInput   string
	OnConfirm   func(int, string, any) tea.Cmd
	OnCancel    func(any) tea.Cmd
}

// Init validates selection index.
func (q *Question) Init(any) {
	if q.Selection < 0 {
		q.Selection = 0
	}
	if q.Selection >= len(q.Options) {
		q.Selection = len(q.Options) - 1
	}
}

// RenderHeight returns the same height as Picker.
func (q *Question) RenderHeight() int {
	return PickerMaxItems + 4
}

// Update is a no-op for Question (no blink, no state).
func (q *Question) Update(tea.Msg, any) tea.Cmd {
	return nil
}

// HandleKey processes key events for the question.
func (q *Question) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	s := msg.String()

	switch {
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
	case s == "tab":
		q.Selection = (q.Selection + 1) % len(q.Options)
	case s == "enter":
		if q.OnConfirm != nil {
			return q.OnConfirm(q.Selection, q.TextInput, ctx)
		}
	case s == "esc":
		if q.TextInput != "" {
			q.TextInput = ""
		} else if q.OnCancel != nil {
			return q.OnCancel(ctx)
		}
	case s == "ctrl+c":
		if q.TextInput != "" {
			q.TextInput = ""
		} else if q.OnCancel != nil {
			return q.OnCancel(ctx)
		}
	case s == "backspace":
		if len(q.TextInput) > 0 {
			runes := []rune(q.TextInput)
			q.TextInput = string(runes[:len(runes)-1])
		}
	case s == "space":
		q.TextInput += " "
	case len(s) == 1 && isPrintable(s):
		q.TextInput += s
	}

	return nil
}

// Render draws the question overlay using picker-style rows.
func (q *Question) Render(width int) string {
	bg := lipgloss.Color(style.P.BgFooter)

	var lines []string

	// Leading blank line
	lines = append(lines, " ")

	// Title
	lines = append(lines, lipgloss.NewStyle().Background(bg).Render(style.HeadingStyle.Render("   "+q.Title)))

	// Separator blank line
	lines = append(lines, " ")

	// Option rows — picker-style with arrow indicator
	for i, opt := range q.Options {
		if i == q.Selection {
			lines = append(lines, lipgloss.NewStyle().
				Background(lipgloss.Color(style.P.BgSelected)).
				Foreground(lipgloss.Color(style.P.TextAccent)).
				Bold(true).
				Width(width).
				Render(" \u25B6 "+opt))
		} else {
			lines = append(lines, lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color(style.P.TextMuted)).
				Render("   "+opt))
		}
	}

	// Blank separator + instruction line (right after options)
	if q.ShowInput {
		lines = append(lines, " ")
		if q.TextInput != "" {
			lines = append(lines, lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("  Instruction: ") +
				lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextAccent)).Render(q.TextInput))
		} else {
			lines = append(lines, lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("  Instruction: add instructions to your answer..."))
		}
	}

	// Pad remaining to fill height
	remaining := (PickerMaxItems + 1) - len(lines)
	for i := 0; i < remaining; i++ {
		lines = append(lines, " ")
	}

	// Trailing blank line
	lines = append(lines, " ")

	return lipgloss.NewStyle().
		Background(bg).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

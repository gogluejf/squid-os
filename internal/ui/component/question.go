package component

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

// Question is a multi-option selection overlay with optional text input.
// Options render like Picker rows (with arrow indicator).
// An input line always appears at the bottom for additional instructions.
//
// Example: authorization = Options:["Yes","No"], ShowInput:true
type Question struct {
	Title       string
	Options     []string      // e.g. ["Yes", "No"] or ["Approve", "Reject", "Modify"]
	Selection   int           // current option index
	ShowInput   bool          // show text input field at bottom
	TextInput   string
	TextMode    bool          // false = arrow nav, true = typing in input
	OnConfirm   func(int, string) tea.Cmd // selectedIndex, textInput
	OnCancel    func() tea.Cmd
}

// Init validates selection index. Context is ignored by Question.
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

// HandleKey processes key events for the question. Context is ignored.
func (q *Question) HandleKey(msg tea.KeyMsg, _ any) tea.Cmd {
	s := msg.String()

	if q.TextMode {
		switch {
		case s == "esc":
			q.TextMode = false
		case s == "enter":
			if q.OnConfirm != nil {
				return q.OnConfirm(q.Selection, q.TextInput)
			}
		case s == "backspace":
			if len(q.TextInput) > 0 {
				runes := []rune(q.TextInput)
				q.TextInput = string(runes[:len(runes)-1])
			}
		case len(s) == 1 && isPrintable(s):
			q.TextInput += s
		}
		return nil
	}

	switch {
	case s == "tab" || s == "shift+tab":
		if q.ShowInput {
			q.TextMode = true
		}
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
			return q.OnConfirm(q.Selection, q.TextInput)
		}
	case s == "esc" || s == "ctrl+c":
		if q.OnCancel != nil {
			return q.OnCancel()
		}
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
				Background(bg).
				Render(" \u25B6 "+opt))
		} else {
			lines = append(lines, lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color(style.P.TextMuted)).
				Render("   "+opt))
		}
	}

	// Pad option rows to fill some space
	for i := len(q.Options); i < PickerMaxItems/2; i++ {
		lines = append(lines, " ")
	}

	// Text input (always shown if ShowInput)
	if q.ShowInput {
		if q.TextMode {
			var b strings.Builder
			b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("   Instructions: "))
			b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true).Render(q.TextInput+"█"))
			lines = append(lines, b.String())
		} else {
			lines = append(lines, lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("   Instructions: [Tab] to type"))
		}
	}

	// Pad remaining slots
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

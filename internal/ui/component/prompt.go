package component

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/style"
)

// Prompt is a single-line text input overlay (e.g. save session name).
// RenderHeight matches Picker so viewport layout stays stable.
type Prompt struct {
	Title        string
	Label        string   // e.g. "Name:"
	Value        string
	DefaultValue string
	OnConfirm    func(string) tea.Cmd
	OnCancel     func()   tea.Cmd
}

// Init resolves the default value. Context is ignored by Prompt.
func (p *Prompt) Init(any) {
	if p.Value == "" && p.DefaultValue != "" {
		p.Value = p.DefaultValue
		p.DefaultValue = ""
	}
}

// RenderHeight returns the same height as Picker.
func (p *Prompt) RenderHeight() int {
	return PickerMaxItems + 4 // 3 + PickerMaxItems + 1, same as Picker
}

// HandleKey processes key events for the prompt. Context is ignored.
func (p *Prompt) HandleKey(msg tea.KeyMsg, _ any) tea.Cmd {
	s := msg.String()

	switch {
	case s == "enter":
		if p.OnConfirm != nil {
			return p.OnConfirm(p.Value)
		}
	case s == "esc" || s == "ctrl+c":
		if p.OnCancel != nil {
			return p.OnCancel()
		}
	case s == "backspace":
		if len(p.Value) > 0 {
			runes := []rune(p.Value)
			p.Value = string(runes[:len(runes)-1])
		}
	case len(s) == 1 && isPrintable(s):
		p.Value += s
	}

	return nil
}

// Render draws the prompt overlay.
func (p *Prompt) Render(width int) string {
	bg := lipgloss.Color(style.P.BgFooter)

	var lines []string

	// Leading blank line
	lines = append(lines, " ")

	// Title
	lines = append(lines, lipgloss.NewStyle().Background(bg).Render(style.HeadingStyle.Render("   "+p.Title)))

	// Separator blank line
	lines = append(lines, " ")

	// Label + value with cursor
	var inputLine strings.Builder
	inputLine.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("   "+p.Label+" "))
	inputLine.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true).Render(p.Value+"█"))
	lines = append(lines, inputLine.String())

	// Pad remaining slots to match PickerMaxItems
	for i := 0; i < PickerMaxItems-1; i++ {
		lines = append(lines, " ")
	}

	// Trailing blank line
	lines = append(lines, " ")

	return lipgloss.NewStyle().
		Background(bg).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

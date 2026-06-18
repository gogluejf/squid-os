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
	Description  string   // dim text shown under title (optional)
	Label        string   // e.g. "Name:"
	Value        string
	DefaultValue string
	OnConfirm    func(string, any) tea.Cmd
	OnCancel     func(any) tea.Cmd
}

// Init resolves the default value. Context is ignored by Prompt.
func (p *Prompt) Init(any) {
	if p.Value == "" && p.DefaultValue != "" {
		p.Value = p.DefaultValue
		p.DefaultValue = ""
	}
}

// Update handles non-key messages for Prompt (no-op, kept for interface compliance).
func (p *Prompt) Update(tea.Msg, any) tea.Cmd {
	return nil
}

// RenderHeight returns the same height as Picker.
func (p *Prompt) RenderHeight() int {
	return PickerMaxItems + 4 // 3 + PickerMaxItems + 1, same as Picker
}

// HandleKey processes key events for the prompt.
func (p *Prompt) HandleKey(msg tea.KeyMsg, ctx any) tea.Cmd {
	s := msg.String()

	switch {
	case s == "enter":
		if p.OnConfirm != nil {
			return p.OnConfirm(p.Value, ctx)
		}
	case s == "esc" || s == "ctrl+c":
		if p.OnCancel != nil {
			return p.OnCancel(ctx)
		}
	case s == "backspace":
		if len(p.Value) > 0 {
			runes := []rune(p.Value)
			p.Value = string(runes[:len(runes)-1])
		}
	case len(s) == 1 && isPrintable(s):
		p.Value += s
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 1:
		// Paste event — insert all runes
		p.Value += string(msg.Runes)
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

	// Description (dim, optional) — separated from title by a blank line
	var descLines []string
	var descCount int
	if p.Description != "" {
		lines = append(lines, " ")
		descLines, descCount = formatDescription(p.Description, width, DescriptionMaxLines)
		lines = append(lines, strings.Split(renderDescriptionLines(descLines, width), "\n")...)
	}

	// Separator blank line
	lines = append(lines, " ")

	// Label + value with cursor
	var inputLine strings.Builder
	inputLine.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("   "+p.Label+" "))
	inputLine.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true).Render(p.Value+"█"))
	lines = append(lines, inputLine.String())

	// Pad remaining slots to match PickerMaxItems
	// Account for: blank before desc, desc lines, blank after desc, blank separator, input line
	usedExtra := descCount + (1 + 1) // blank before desc + blank after desc (separator)
	if descCount == 0 {
		usedExtra = 0 // no description, no extra blanks
	}
	// Fixed lines: leading blank (1) + title (1) + separator blank (1) + input (1) + trailing blank (1) = 5
	fixedLines := 5
	pad := PickerMaxItems + 4 - fixedLines - usedExtra
	if pad < 0 {
		pad = 0
	}
	for i := 0; i < pad; i++ {
		lines = append(lines, " ")
	}

	// Trailing blank line
	lines = append(lines, " ")

	return lipgloss.NewStyle().
		Background(bg).
		Width(width).
		Render(strings.Join(lines, "\n"))
}

package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// AuthorizationPrompt renders the authorization prompt UI for tool execution confirmation.
type AuthorizationPrompt struct {
	ToolName      string
	DisplayValue  string
	IsDestructive bool
	PreviewDiff   string     // from tool.Preview — empty if no preview available
	Selection     int        // 0=yes, 1=no
	TextMode      bool       // true = typing instructions
	TextInput     string
	Width         int
}

var (
	authYesStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render
	authNoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render
	authDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render
)

func (p AuthorizationPrompt) Render() string {
	var sb strings.Builder

	// Tool label + destructive icon
	label := p.ToolName
	if p.DisplayValue != "" {
		display := p.DisplayValue
		if len(display) > 40 {
			display = display[:37] + "..."
		}
		label = fmt.Sprintf("%s(%s)", p.ToolName, display)
	}
	icon := ""
	if p.IsDestructive {
		icon = "⚠ "
	}

	// Diff preview indicator
	var diffLine string
	if p.PreviewDiff != "" {
		lines := strings.Count(p.PreviewDiff, "\n")
		diffLine = fmt.Sprintf(" (%d lines changed)", lines)
	}

	sb.WriteString(fmt.Sprintf("\n  %s%s%s\n", icon, label, diffLine))

	if p.TextMode {
		sb.WriteString(fmt.Sprintf("  Instructions: %s█\n", p.TextInput))
	} else {
		yesLabel, noLabel := authDimStyle("Yes"), authDimStyle("No")
		if p.Selection == 0 {
			yesLabel = authYesStyle("Yes")
		}
		if p.Selection == 1 {
			noLabel = authYesStyle("No")
		}
		sb.WriteString(fmt.Sprintf("  %s / %s  [←/→] select  [Tab] instructions  [Enter] confirm  [Esc] reject\n", yesLabel, noLabel))
	}

	return sb.String()
}

package style

import "github.com/charmbracelet/lipgloss"

// StyleLabel holds pre-built lipgloss styles for a tool/skill.
type StyleLabel struct {
	Label lipgloss.Style // label style (e.g. tool name)
	Param lipgloss.Style // param style (e.g. display value)
	Dim   lipgloss.Style // dim style (e.g. separators, muted text)
}

// ToolStyle returns the style for core tools (cyan label, dark gray-blue param).
func ToolStyle() StyleLabel {
	bg := lipgloss.Color("234") // BgCode
	return StyleLabel{
		Label: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("110")),
		Param: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("67")),
		Dim:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("243")),
	}
}

// SkillStyle returns the style for skill tools (yellow label, darker yellow param).
func SkillStyle() StyleLabel {
	bg := lipgloss.Color("234") // BgCode
	return StyleLabel{
		Label: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("178")),
		Param: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("180")),
		Dim:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("243")),
	}
}

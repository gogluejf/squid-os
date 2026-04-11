package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CommandInfo describes a slash command for the palette
type CommandInfo struct {
	Name        string
	Description string
}

// AllCommands is the full command list
var AllCommands = []CommandInfo{
	{Name: "model", Description: "Select inference model"},
	{Name: "thinking", Description: "Toggle thinking mode (on/off)"},
	{Name: "image", Description: "Attach image to next message"},
	{Name: "save", Description: "Save current session"},
	{Name: "load", Description: "Load a saved session"},
	{Name: "clear", Description: "Clear chat and start fresh"},
	{Name: "system", Description: "Load system prompt"},
	{Name: "exit", Description: "Exit rig-chat"},
	{Name: "help", Description: "Show help"},
}

// CommandPalette holds the state for the slash command overlay
type CommandPalette struct {
	Filter   string
	Selected int
	Visible  bool
	Items    []CommandInfo
}

func NewCommandPalette() CommandPalette {
	return CommandPalette{
		Items: AllCommands,
	}
}

// FilteredItems returns commands matching the current filter
func (cp *CommandPalette) FilteredItems() []CommandInfo {
	if cp.Filter == "" {
		return cp.Items
	}
	f := strings.ToLower(cp.Filter)
	var result []CommandInfo
	for _, item := range cp.Items {
		if strings.HasPrefix(strings.ToLower(item.Name), f) {
			result = append(result, item)
		}
	}
	return result
}

// MoveUp moves selection up
func (cp *CommandPalette) MoveUp() {
	if cp.Selected > 0 {
		cp.Selected--
	}
}

// MoveDown moves selection down
func (cp *CommandPalette) MoveDown() {
	items := cp.FilteredItems()
	if cp.Selected < len(items)-1 {
		cp.Selected++
	}
}

// SelectedCommand returns the currently selected command name, or empty
func (cp *CommandPalette) SelectedCommand() string {
	items := cp.FilteredItems()
	if cp.Selected >= 0 && cp.Selected < len(items) {
		return items[cp.Selected].Name
	}
	return ""
}

// Reset clears the palette state
func (cp *CommandPalette) Reset() {
	cp.Filter = ""
	cp.Selected = 0
	cp.Visible = false
}

// maxCmdItems is the maximum number of command palette rows ever rendered.
const maxCmdItems = 10

// RenderHeight returns the exact number of terminal lines that Render() will output.
func (cp *CommandPalette) RenderHeight() int {
	n := len(cp.FilteredItems())
	if n == 0 {
		return 1 // "No matching commands" line
	}
	if n > maxCmdItems {
		n = maxCmdItems
	}
	return n
}

// palette background colours
const paletteBg = lipgloss.Color("235")
const paletteSelectedBg = lipgloss.Color("237")

// Render renders the command palette
func (cp *CommandPalette) Render(width int) string {
	items := cp.FilteredItems()
	if len(items) == 0 {
		return lipgloss.NewStyle().
			Background(paletteBg).
			Width(width).
			Render(lipgloss.NewStyle().Background(paletteBg).Foreground(lipgloss.Color("243")).Render("  No matching commands"))
	}
	// Cap rendered items to match RenderHeight.
	if len(items) > maxCmdItems {
		items = items[:maxCmdItems]
	}

	// Inline styles that carry the background so ANSI resets don't punch holes.
	normalNameStyle := lipgloss.NewStyle().Background(paletteBg).Foreground(lipgloss.Color("110")).Bold(true)
	normalDescStyle := lipgloss.NewStyle().Background(paletteBg).Foreground(lipgloss.Color("243"))
	normalRowStyle := lipgloss.NewStyle().Background(paletteBg).Width(width)

	selNameStyle := lipgloss.NewStyle().Background(paletteSelectedBg).Foreground(lipgloss.Color("252")).Bold(true)
	selDescStyle := lipgloss.NewStyle().Background(paletteSelectedBg).Foreground(lipgloss.Color("243"))
	selRowStyle := lipgloss.NewStyle().Background(paletteSelectedBg).Width(width)

	var rows []string
	for i, item := range items {
		name := "  /" + item.Name
		desc := "  " + item.Description
		if i == cp.Selected {
			rows = append(rows, selRowStyle.Render(selNameStyle.Render(name)+selDescStyle.Render(desc)))
		} else {
			rows = append(rows, normalRowStyle.Render(normalNameStyle.Render(name)+normalDescStyle.Render(desc)))
		}
	}

	return strings.Join(rows, "\n")
}

// PickerList is a generic filtered list for model picker, session picker, etc.
type PickerList struct {
	Title    string
	Items    []string
	Filter   string
	Selected int
}

func NewPickerList(title string, items []string) PickerList {
	return PickerList{
		Title: title,
		Items: items,
	}
}

func (pl *PickerList) FilteredItems() []string {
	if pl.Filter == "" {
		return pl.Items
	}
	f := strings.ToLower(pl.Filter)
	var result []string
	for _, item := range pl.Items {
		if strings.Contains(strings.ToLower(item), f) {
			result = append(result, item)
		}
	}
	return result
}

func (pl *PickerList) MoveUp() {
	if pl.Selected > 0 {
		pl.Selected--
	}
}

func (pl *PickerList) MoveDown() {
	items := pl.FilteredItems()
	if pl.Selected < len(items)-1 {
		pl.Selected++
	}
}

func (pl *PickerList) SelectedItem() string {
	items := pl.FilteredItems()
	if pl.Selected >= 0 && pl.Selected < len(items) {
		return items[pl.Selected]
	}
	return ""
}

// maxPickerItems is the maximum number of list rows ever rendered (excluding heading/filter).
const maxPickerItems = 15

// RenderHeight returns the exact number of terminal lines that Render() will output.
func (pl *PickerList) RenderHeight() int {
	h := 1 // heading line
	if pl.Filter != "" {
		h++ // filter hint line
	}
	items := pl.FilteredItems()
	if len(items) == 0 {
		h++ // "No matches" line
	} else {
		count := len(items)
		if count > maxPickerItems {
			count = maxPickerItems
		}
		h += count
	}
	return h
}

func (pl *PickerList) Render(width int) string {
	items := pl.FilteredItems()

	var b strings.Builder
	b.WriteString(HeadingStyle.Render("  "+pl.Title) + "\n")

	if pl.Filter != "" {
		b.WriteString(CommandDescStyle.Render("  filter: "+pl.Filter) + "\n")
	}

	if len(items) == 0 {
		b.WriteString(CommandDescStyle.Render("  No matches"))
		return lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Width(width).
			Render(strings.TrimRight(b.String(), "\n"))
	}

	// Show max maxPickerItems items around selection (must match RenderHeight).
	start := pl.Selected - 7
	if start < 0 {
		start = 0
	}
	end := start + maxPickerItems
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		if i == pl.Selected {
			b.WriteString(CommandSelectedStyle.Width(width).Render("  "+items[i]) + "\n")
		} else {
			b.WriteString(CommandDescStyle.Render("  "+items[i]) + "\n")
		}
	}

	return lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Width(width).
		Render(strings.TrimRight(b.String(), "\n"))
}

// ThinkingToggle for the /thinking command
type ThinkingToggle struct {
	Value    bool
	Selected int // 0 = on, 1 = off
}

func NewThinkingToggle(current bool) ThinkingToggle {
	sel := 1
	if current {
		sel = 0
	}
	return ThinkingToggle{Value: current, Selected: sel}
}

func (tt *ThinkingToggle) Toggle() {
	if tt.Selected == 0 {
		tt.Selected = 1
	} else {
		tt.Selected = 0
	}
}

func (tt *ThinkingToggle) Result() bool {
	return tt.Selected == 0
}

func (tt *ThinkingToggle) Render(width int) string {
	var b strings.Builder
	b.WriteString(HeadingStyle.Render("  Thinking Mode") + "\n")

	options := []string{"on", "off"}
	for i, opt := range options {
		if i == tt.Selected {
			b.WriteString(CommandSelectedStyle.Width(width).Render("  "+opt) + "\n")
		} else {
			b.WriteString(CommandDescStyle.Render("  "+opt) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// SavePrompt for the /save command
type SavePrompt struct {
	Name    string
	Editing bool
}

func NewSavePrompt(lastName string) SavePrompt {
	return SavePrompt{Name: lastName, Editing: true}
}

func (sp *SavePrompt) Render(width int) string {
	var b strings.Builder
	b.WriteString(HeadingStyle.Render("  Save Session") + "\n")
	b.WriteString(CommandDescStyle.Render("  Name: "))
	b.WriteString(CommandStyle.Render(sp.Name + "_"))
	return b.String()
}

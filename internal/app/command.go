package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

// SlashCommand defines a slash command with optional key shortcut.
type SlashCommand struct {
	Name              string
	Description       string
	Key               key.Binding // keyboard shortcut that triggers this command
	BlockDuringStream bool        // if true, show notification and skip execution while streaming
	OnExecute         func(Model) (tea.Model, tea.Cmd)
}

// AllCommands is the full slash command list
var AllCommands = []SlashCommand{
	{
		Name:              "model",
		Description:       "Select inference model",
		Key:               keys.Model,
		BlockDuringStream: true,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.openModelPicker()
		},
	},
	{
		Name:        "skill",
		Description: "Select active skill",
		Key:         keys.Skill,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.openSkillPicker()
		},
	},
	{
		Name:        "thinking",
		Description: "Toggle thinking mode (on/off)",
		Key:         keys.Thinking,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.toggleThinking()
		},
	},
	{
		Name:        "auth-mode",
		Description: "Cycle authorization mode (auto/ask-on-write/ask-for-all)",
		Key:         keys.AuthMode,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.cycleAuthorization(true)
		},
	},
	{
		Name:              "save",
		Description:       "Save current session",
		Key:               keys.Save,
		BlockDuringStream: true,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.openSaveSessionPrompt()
		},
	},
	{
		Name:              "load",
		Description:       "Load a saved session",
		Key:               keys.Load,
		BlockDuringStream: true,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.openSessionPicker()
		},
	},
	{
		Name:              "clear",
		Description:       "Clear chat and start fresh",
		Key:               keys.NewSession,
		BlockDuringStream: true,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.clearSession()
		},
	},
	{
		Name:        "system",
		Description: "Load system prompt",
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m.openSystemPicker()
		},
	},
	{
		Name:        "exit",
		Description: "Exit squid-os",
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			return m, tea.Quit
		},
	},
	{
		Name:        "help",
		Description: "Show help",
		Key:         keys.Help,
		OnExecute: func(m Model) (tea.Model, tea.Cmd) {
			m.mode = ModeHelp
			return m, nil
		},
	},
}

// buildCommandPickerItems builds the standard command list as PickerItems for the command palette.
func buildCommandPickerItems() []component.PickerItem {
	items := make([]component.PickerItem, len(AllCommands))
	for i, c := range AllCommands {
		meta := []string{c.Description}
		if len(c.Key.Keys()) > 0 {
			meta = append(meta, c.Key.Help().Key)
		}
		items[i] = component.PickerItem{
			Label: "/" + c.Name,
			Meta:  meta,
			Value: c.Name,
		}
	}
	return items
}

// matchCommandKey checks if any command's key binding matches the given key message.
// Returns the command name if matched, or empty string.
func matchCommandKey(msg tea.KeyMsg) string {
	for _, c := range AllCommands {
		if len(c.Key.Keys()) > 0 && key.Matches(msg, c.Key) {
			return c.Name
		}
	}
	return ""
}

// findCommand returns the SlashCommand with the given name, or nil.
func findCommand(name string) *SlashCommand {
	for i := range AllCommands {
		if AllCommands[i].Name == name {
			return &AllCommands[i]
		}
	}
	return nil
}

// executeCommandByName dispatches to the appropriate handler for a command name.
func (m Model) executeCommandByName(name string) (tea.Model, tea.Cmd) {
	cmd := findCommand(name)
	if cmd == nil || cmd.OnExecute == nil {
		return m, m.setChatMode()
	}
	if cmd.BlockDuringStream && m.mode == ModeStreaming {
		m.setNotification(ui.NotificationInfo, fmt.Sprintf("/%s will be available when assistant finishes", name))
		return m, nil
	}
	return cmd.OnExecute(m)
}

// openCommandPicker opens the slash command palette.
func (m *Model) openCommandPicker() {
	picker := component.Picker{
		Title:     "Commands",
		Items:     m.allCommands,
		MatchMode: component.MatchPrefix,
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			m.textarea.SetValue("")
			mm, cmd := m.executeCommandByName(item.Value)
			*m = mm.(Model)
			return cmd
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			m.textarea.SetValue("")
			return m.setChatMode()
		},
	}
	m.setComponent(&picker)
}

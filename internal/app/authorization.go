package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

type AuthResult struct {
	Approved     bool
	Instructions string // empty = plain yes/no
}

func (r AuthResult) HasInstructions() bool { return r.Instructions != "" }

type AuthorizationContext struct {
	ToolName      string
	Args          map[string]interface{}
	ArgsJSON      string
	DisplayValue  string
	IsDestructive bool
	Result        AuthResult
}

func (c *AuthorizationContext) IsActionable() bool {
	return c.Result.Approved || c.Result.Instructions != ""
}

// cycleAuthorization cycles through authorization modes: auto → ask-on-write → ask-for-all → auto.
// Persists like thinking. Does not change UI mode or disturb an active stream.
func (m Model) cycleAuthorization() (Model, tea.Cmd) {
	modes := []config.AuthorizationMode{config.AuthorizationAuto, config.AuthorizationAskOnWrite, config.AuthorizationAskForAll}
	idx := 0
	for i, mode := range modes {
		if string(mode) == m.settings.Authorization {
			idx = i
			break
		}
	}
	next := modes[(idx+1)%len(modes)]
	m.settings.Authorization = string(next)
	m.session.Doc.Config.AuthMode = next
	if err := config.SaveSettings(m.paths, m.settings); err != nil {
		m.setNotification(ui.NotificationError, "could not save authorization settings: "+err.Error())
		return m, nil
	}
	labels := map[config.AuthorizationMode]string{
		config.AuthorizationAuto:       "auto — execute all tools without asking",
		config.AuthorizationAskOnWrite: "ask-on-write — confirm before destructive commands",
		config.AuthorizationAskForAll:  "ask-for-all — confirm every tool call",
	}
	(&m).setNotification(ui.NotificationInfo, "authorization: "+labels[next])
	(&m).recalcLayout()
	return m, nil
}

// openAuthorizationModePicker shows a picker to explicitly select an authorization mode.
func (m Model) openAuthorizationModePicker() (Model, tea.Cmd) {
	items := []component.PickerItem{
		{Label: "auto", Meta: []string{"execute all tools without asking"}, Value: string(config.AuthorizationAuto)},
		{Label: "ask-on-write", Meta: []string{"confirm before destructive commands"}, Value: string(config.AuthorizationAskOnWrite)},
		{Label: "ask-for-all", Meta: []string{"confirm every tool call"}, Value: string(config.AuthorizationAskForAll)},
	}
	picker := component.Picker{
		Title:        "Authorization Mode",
		Items:        items,
		DefaultValue: m.settings.Authorization,
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			mode := config.AuthorizationMode(item.Value)
			m.settings.Authorization = string(mode)
			m.session.Doc.Config.AuthMode = mode
			if err := config.SaveSettings(m.paths, m.settings); err != nil {
				m.setNotification(ui.NotificationError, "could not save authorization settings: "+err.Error())
				return m.dismissCommandPicker()
			}
			labels := map[config.AuthorizationMode]string{
				config.AuthorizationAuto:       "auto — execute all tools without asking",
				config.AuthorizationAskOnWrite: "ask-on-write — confirm before destructive commands",
				config.AuthorizationAskForAll:  "ask-for-all — confirm every tool call",
			}
			m.setNotification(ui.NotificationInfo, "authorization: "+labels[mode])
			return m.dismissCommandPicker()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.dismissCommandPicker()
		},
	}
	m.setComponent(&picker)
	return m, nil
}

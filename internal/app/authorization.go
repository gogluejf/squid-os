package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui"
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

// cycleAuthorization cycles through authorization modes: auto -> ask-on-write -> ask-for-all -> auto.
// If dismiss is true, the component is closed (useful when cycling to "auto" mid-prompt).
func (m Model) cycleAuthorization(dismiss bool) (Model, tea.Cmd) {
	modes := []string{config.AuthorizationAuto, config.AuthorizationAskOnWrite, config.AuthorizationAskForAll}
	idx := 0
	for i, mode := range modes {
		if mode == m.settings.Authorization {
			idx = i
			break
		}
	}
	next := modes[(idx+1)%len(modes)]
	m.settings.Authorization = next
	_ = config.SaveSettings(m.paths, m.settings)
	labels := map[string]string{
		config.AuthorizationAuto:       "auto — execute all tools without asking",
		config.AuthorizationAskOnWrite: "ask-on-write — confirm before destructive commands",
		config.AuthorizationAskForAll:  "ask-for-all — confirm every tool call",
	}
	(&m).setNotification(ui.NotificationInfo, "authorization: "+labels[next])

	if dismiss {
		return m, m.setChatMode()
	}
	// When not dismissing, just re-render the current view with updated state.
	(&m).recalcLayout()
	return m, nil
}

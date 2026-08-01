package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/skills"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

// setSkill sets the pending skill change and shows a notification.
func (m *Model) setSkill(name string) {
	m.session.SetPendingSkill(name)
	if name == "" {
		m.setNotification(ui.NotificationInfo, "skill: (will unload at next user turn)")
	} else {
		m.setNotification(ui.NotificationInfo, "skill: "+name+" (will load at next user turn)")
	}
}

// openSkillPicker opens the skill picker overlay, building items from the registry.
func (m Model) openSkillPicker() (Model, tea.Cmd) {
	items := make([]component.PickerItem, 0, 16)
	items = append(items, component.PickerItem{
		Label: "(none)",
		Meta:  []string{"No skill active"},
		Value: "(none)",
	})
	if reg := skills.GetRegistry(); reg != nil {
		allowed := make(map[string]bool, len(m.session.Doc.Config.Skills))
		for _, name := range m.session.Doc.Config.Skills {
			allowed[name] = true
		}
		for _, e := range reg.List() {
			if !allowed[e.Name] {
				continue
			}
			items = append(items, component.PickerItem{
				Label: e.Name,
				Meta:  []string{e.Description},
				Value: e.Name,
			})
		}
	}

	// Pre-select current skill if any
	current := m.session.EffectiveSkill()

	picker := component.Picker{
		Title:        "Select Skill",
		Items:        items,
		DefaultValue: current,
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			skillName := strings.TrimSpace(item.Label)
			if skillName == "(none)" {
				skillName = ""
			}
			if skillName != m.session.EffectiveSkill() {
				m.setSkill(skillName)
			}
			return m.setChatMode()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.setChatMode()
		},
	}

	(&m).setComponent(&picker)
	return m, nil
}

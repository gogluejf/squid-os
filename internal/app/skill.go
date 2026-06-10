package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/skills"
	"squid-os/internal/ui"
	"squid-os/internal/util"
)

func (m Model) cycleSkill() (Model, tea.Cmd) {
	options := []string{""}
	if reg := skills.GetRegistry(); reg != nil {
		for _, e := range reg.List() {
			options = append(options, e.Name)
		}
	}

	// Determine current position: prefer Next if set (pending), else Current
	current := m.session.file.Session.Skill.Current
	if m.session.file.Session.Skill.Next != nil {
		current = *m.session.file.Session.Skill.Next
	}

	idx := 0
	for i, s := range options {
		if s == current {
			idx = i
			break
		}
	}

	next := options[(idx+1)%len(options)]
	(&m).setSkill(next)

	m.updateViewportContent()
	return m.autoSave()
}

// setSkill sets the pending skill change and shows a notification.
func (m *Model) setSkill(name string) {
	m.session.file.Session.Skill.Next = &name
	if name == "" {
		m.setNotification(ui.NotificationInfo, "skill: (will unload at next user turn)")
	} else {
		m.setNotification(ui.NotificationInfo, "skill: "+name+" (will inject at next user turn)")
	}
}

func (m *Model) injectSkillChangeSynthetic(old string, nxt string) {
	var text string
	var label string
	var params map[string]string

	if nxt == "" {
		text = fmt.Sprintf("Skill %q has been unloaded by the user. Don't use the previously loaded skill anymore.", old)
		label = "skill-unload"
		params = nil
	} else if old == "" {
		text = fmt.Sprintf("Skill %q has been loaded by the user.\n\n", nxt)
		text += m.getSkillText(nxt)
		label = "skill-load"
		params = map[string]string{"name": nxt}
	} else {
		text = fmt.Sprintf("Skill changed from %q to %q by the user. Stop using the previous skill and use the new one instead.\n\n", old, nxt)
		text += m.getSkillText(nxt)
		label = "skill-load"
		params = map[string]string{"name": nxt}
	}

	m.session.appendMsg(config.Message{
		ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:        config.RoleSynthetic,
		CreatedAt:   time.Now(),
		Text:        text,
		Label:       label,
		Params:      params,
		InputTokens: countTokensApprox(text),
	})
}

func (m *Model) getSkillText(name string) string {
	reg := skills.GetRegistry()
	if reg == nil {
		return fmt.Sprintf("Loaded skill: %s", name)
	}
	sk, err := reg.Load(name)
	if err != nil {
		return fmt.Sprintf("Loaded skill: %s", name)
	}
	text := fmt.Sprintf("Loaded skill: %s\n\n", name)
	if sk.Body != "" {
		text += sk.Body
	}
	return text
}

// openSkillPicker opens the skill picker overlay, building items from the registry.
func (m Model) openSkillPicker() (Model, tea.Cmd) {
	// Collect entries for column width calculation
	type skillItem struct {
		name        string
		description string
	}
	var entries []skillItem
	entries = append(entries, skillItem{name: "(none)", description: "No skill active"})
	if reg := skills.GetRegistry(); reg != nil {
		for _, e := range reg.List() {
			entries = append(entries, skillItem{name: e.Name, description: e.Description})
		}
	}

	// Find longest name for column alignment
	maxName := 0
	for _, e := range entries {
		if len(e.name) > maxName {
			maxName = len(e.name)
		}
	}
	if maxName < 8 {
		maxName = 8
	}

	// Description gets remaining width (name col + 2-space gap + truncation "..." margin)
	descMax := m.width - maxName - 2
	if descMax > 5 {
		descMax -= 3 // leave room for "..."
	}
	if descMax < 1 {
		descMax = 1
	}

	fmtStr := fmt.Sprintf("%%-%ds  %%s", maxName)
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.description != "" {
			items = append(items, fmt.Sprintf(fmtStr, e.name, util.Truncate(e.description, descMax)))
		} else {
			items = append(items, e.name)
		}
	}

	m.skillPicker = ui.NewPickerList("Select Skill", items)

	// Pre-select current skill if any
	current := m.session.file.Session.Skill.Current
	if m.session.file.Session.Skill.Next != nil {
		current = *m.session.file.Session.Skill.Next
	}
	if current != "" {
		for i, e := range entries {
			if e.name == current {
				m.skillPicker.Selected = i
				break
			}
		}
	}

	m.mode = ModeSkillPicker
	(&m).recalcLayout()
	return m, nil
}

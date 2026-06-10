package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/skills"
	"squid-os/internal/ui"
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
	m.session.file.Session.Skill.Next = &next

	if next == "" {
		m.setNotification(ui.NotificationInfo, "skill: (will unload at next user turn)")
	} else {
		m.setNotification(ui.NotificationInfo, "skill: "+next+" (will inject at next user turn)")
	}

	m.updateViewportContent()
	return m.autoSave()
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

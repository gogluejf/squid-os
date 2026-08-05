package chat

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"squid-os/internal/config"
)

// PrepareTurn applies pending changes to the session.
// It compares Config vs Pending, injects transition messages for diffs, and commits.
func PrepareTurn(s *Session) error {
	if len(s.Doc.Messages) == 0 || s.Doc.Messages[len(s.Doc.Messages)-1].Role != config.RoleUser {
		return fmt.Errorf("prepare turn requires the latest message to be user input")
	}

	pending := s.Doc.Pending
	if pending == nil {
		// Nothing pending — commit as-is
		return nil
	}

	// Handle inference
	if pending.Inference != nil {
		current := s.CurrentInference()
		desired := *pending.Inference
		if current.Provider != desired.Provider || current.Model != desired.Model {
			s.PushModelSwitch(current.Provider+"/"+current.Model, desired.Provider+"/"+desired.Model)
		}
		if current.Thinking != desired.Thinking {
			s.PushThinkingSwitch(desired.Thinking)
		}
		s.SetInference(desired)
	}

	// Handle target mode
	if pending.Target != nil {
		oldTarget := s.Doc.Config.Target
		desiredTarget := *pending.Target
		if desiredTarget != oldTarget {
			s.Doc.Config.Target = desiredTarget
			s.Append(BuildModeSwitchMsg(oldTarget, desiredTarget))
		}
	}

	// Handle skill
	applyPendingActiveSkill(s, pending)

	// Handle tools
	if pending.Tools != nil && !reflect.DeepEqual(*pending.Tools, s.Doc.Config.Tools) {
		s.SetTools(*pending.Tools)
		s.Append(transition("Tools Available Changed", nil))
	}

	// Handle skills and agents.
	applyPendingCapabilities(s, pending)

	// Commit: clear pending
	s.Doc.Pending = nil
	return nil
}

// ApplyPendingCapabilities commits workspace-driven capability changes before
// an immediate same-turn model restart after set_working_dir.
func ApplyPendingCapabilities(s *Session) {
	pending := s.Doc.Pending
	if pending == nil {
		return
	}
	applyPendingActiveSkill(s, pending)
	applyPendingCapabilities(s, pending)
	if pending.Target == nil && pending.Inference == nil && pending.ActiveSkill == nil && pending.Tools == nil && pending.Skills == nil && pending.Agents == nil {
		s.Doc.Pending = nil
	}
}

func applyPendingActiveSkill(s *Session, pending *config.PendingConfig) {
	if pending.ActiveSkill == nil {
		return
	}
	oldSkill := s.CurrentSkill()
	desiredSkill := *pending.ActiveSkill
	if desiredSkill != oldSkill {
		s.SetCurrentSkill(desiredSkill)
		message := s.BuildSkillChangeMsg(oldSkill, desiredSkill)
		message.ID = fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1)
		s.Append(message)
	}
	pending.ActiveSkill = nil
}

func applyPendingCapabilities(s *Session, pending *config.PendingConfig) {
	if pending.Skills != nil && (!reflect.DeepEqual(*pending.Skills, s.Doc.Config.Skills) || len(pending.SkillsMissing) > 0) {
		old := s.GetSkills()
		s.SetSkills(*pending.Skills)
		s.Append(BuildCapabilitiesChangedMsg("skills", old, *pending.Skills, pending.SkillsMissing))
	}
	if pending.Agents != nil && (!reflect.DeepEqual(*pending.Agents, s.Doc.Config.Agents) || len(pending.AgentsMissing) > 0) {
		old := s.GetAgents()
		s.SetAgents(*pending.Agents)
		s.Append(BuildCapabilitiesChangedMsg("agents", old, *pending.Agents, pending.AgentsMissing))
	}
	pending.Skills = nil
	pending.Agents = nil
	pending.SkillsMissing = nil
	pending.AgentsMissing = nil
}

func (s *Session) BuildSkillChangeMsg(oldSkill, nextSkill string) config.Message {
	var text string
	var label string
	var params map[string]string
	if nextSkill == "" {
		text = fmt.Sprintf("Skill %q has been unloaded by the user. Don't use the previously loaded skill anymore.", oldSkill)
		label = "skill_unload"
	} else if oldSkill == "" {
		text = fmt.Sprintf("Skill %q has been loaded by the user.\n\n", nextSkill)
		text += s.skillText(nextSkill)
		label = "skill_load"
		params = map[string]string{"name": nextSkill}
	} else {
		text = fmt.Sprintf("Skill changed from %q to %q by the user. Stop using the previous skill and use the new one instead.\n\n", oldSkill, nextSkill)
		text += s.skillText(nextSkill)
		label = "skill_load"
		params = map[string]string{"name": nextSkill}
	}
	return config.Message{Role: config.RoleSynthetic, CreatedAt: time.Now(), Text: text, Label: label, Params: params, InputTokens: CountTokensApproxString(text)}
}

func (s *Session) skillText(name string) string {
	registry := s.Catalog.Skills
	if registry == nil {
		return fmt.Sprintf("Loaded skill: %s", name)
	}
	skill, err := registry.Load(name)
	if err != nil {
		return fmt.Sprintf("Loaded skill: %s", name)
	}
	text := fmt.Sprintf("Loaded skill: %s\n\n", name)
	if skill.Body != "" {
		text += skill.Body
	}
	return text
}

func BuildCapabilitiesChangedMsg(kind string, old, next []config.CapabilityRef, missing []string) config.Message {
	added, removed := capabilityDiff(old, next)
	available := formatCapabilityRefs(next)
	text := fmt.Sprintf("Available %s changed. Use only this effective list: %s.", kind, available)
	if len(added) > 0 {
		text += "\nAdded: " + formatCapabilityRefs(added) + "."
	}
	if len(removed) > 0 {
		text += "\nRemoved: " + formatCapabilityRefs(removed) + "."
	}
	if len(missing) > 0 {
		text += "\nRequested but unavailable: " + strings.Join(missing, ", ") + "."
	}
	params := map[string]string{kind: available}
	if len(missing) > 0 {
		params["missing"] = strings.Join(missing, ", ")
	}
	label := strings.ToUpper(kind[:1]) + kind[1:] + " Available Changed"
	return config.Message{
		Role:        config.RoleSynthetic,
		CreatedAt:   time.Now(),
		Text:        text,
		Label:       label,
		Params:      params,
		InputTokens: CountTokensApproxString(text),
	}
}

func capabilityDiff(old, next []config.CapabilityRef) (added, removed []config.CapabilityRef) {
	oldSet := make(map[config.CapabilityRef]bool, len(old))
	nextSet := make(map[config.CapabilityRef]bool, len(next))
	for _, ref := range old {
		oldSet[ref] = true
	}
	for _, ref := range next {
		nextSet[ref] = true
		if !oldSet[ref] {
			added = append(added, ref)
		}
	}
	for _, ref := range old {
		if !nextSet[ref] {
			removed = append(removed, ref)
		}
	}
	return added, removed
}

func formatCapabilityRefs(refs []config.CapabilityRef) string {
	if len(refs) == 0 {
		return "none"
	}
	values := make([]string, len(refs))
	for i, ref := range refs {
		values[i] = fmt.Sprintf("%s [%s]", ref.Name, ref.Scope)
	}
	return strings.Join(values, ", ")
}

func transition(label string, params map[string]string) config.Message {
	return config.Message{Role: config.RoleInternal, Label: label, Params: params}
}

func BuildModeSwitchMsg(oldTarget, newTarget string) config.Message {
	text := fmt.Sprintf("Session mode switched from %q to %q. Adjust your behavior for the new session mode.", oldTarget, newTarget)
	return config.Message{
		Role:        config.RoleSynthetic,
		CreatedAt:   time.Now(),
		Text:        text,
		Label:       "session_mode_switch",
		Params:      map[string]string{"from": oldTarget, "to": newTarget},
		InputTokens: CountTokensApproxString(text),
	}
}

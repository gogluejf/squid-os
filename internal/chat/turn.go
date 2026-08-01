package chat

import (
	"fmt"
	"reflect"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/skills"
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

	// Handle skill
	if pending.ActiveSkill != nil {
		oldSkill := s.CurrentSkill()
		desiredSkill := *pending.ActiveSkill
		if desiredSkill != oldSkill {
			s.SetCurrentSkill(desiredSkill)
			message := BuildSkillChangeMsg(oldSkill, desiredSkill)
			message.ID = fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1)
			s.Append(message)
		}
	}

	// Handle tools
	if pending.Tools != nil && !reflect.DeepEqual(*pending.Tools, s.Doc.Config.Tools) {
		s.SetTools(*pending.Tools)
		s.Append(transition("Tools Available Changed", nil))
	}

	// Handle skills
	if pending.Skills != nil && !reflect.DeepEqual(*pending.Skills, s.Doc.Config.Skills) {
		s.SetSkills(*pending.Skills)
		s.Append(transition("Skills Available Changed", nil))
	}

	// Handle agents
	if pending.Agents != nil && !reflect.DeepEqual(*pending.Agents, s.Doc.Config.Agents) {
		s.Doc.Config.Agents = append([]string(nil), *pending.Agents...)
		s.Append(transition("Agents Available Changed", nil))
	}

	// Commit: clear pending
	s.Doc.Pending = nil
	return nil
}

func BuildSkillChangeMsg(oldSkill, nextSkill string) config.Message {
	var text string
	var label string
	var params map[string]string
	if nextSkill == "" {
		text = fmt.Sprintf("Skill %q has been unloaded by the user. Don't use the previously loaded skill anymore.", oldSkill)
		label = "skill_unload"
	} else if oldSkill == "" {
		text = fmt.Sprintf("Skill %q has been loaded by the user.\n\n", nextSkill)
		text += skillText(nextSkill)
		label = "skill_load"
		params = map[string]string{"name": nextSkill}
	} else {
		text = fmt.Sprintf("Skill changed from %q to %q by the user. Stop using the previous skill and use the new one instead.\n\n", oldSkill, nextSkill)
		text += skillText(nextSkill)
		label = "skill_load"
		params = map[string]string{"name": nextSkill}
	}
	return config.Message{Role: config.RoleSynthetic, CreatedAt: time.Now(), Text: text, Label: label, Params: params, InputTokens: CountTokensApproxString(text)}
}

func skillText(name string) string {
	registry := skills.GetRegistry()
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

func transition(label string, params map[string]string) config.Message {
	return config.Message{Role: config.RoleInternal, Label: label, Params: params}
}

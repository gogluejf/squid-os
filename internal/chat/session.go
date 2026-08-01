package chat

import (
	"fmt"
	"strings"
	"time"

	goai_provider "github.com/zendev-sh/goai/provider"

	"squid-os/internal/config"
	"squid-os/internal/environment"
	"squid-os/internal/tools"
)

// Session is the pure runtime session: persisted document + pure stream state.
type Session struct {
	Doc    config.SessionDoc
	Stream StreamState
	Info   config.SessionInfo
}

// NewSession creates a pure session with initial system/environment/config/tool messages.
func NewSession(cfg config.SessionConfig, paths config.Paths) *Session {
	doc := config.NewSessionDoc(cfg)
	s := &Session{Doc: doc}

	sysContent := config.LoadSystemPrompt(paths, cfg.SystemPromptFile)
	s.Append(config.Message{
		ID:          "sys0",
		Role:        config.RoleSystem,
		Text:        sysContent,
		Label:       "System Prompt",
		Params:      map[string]string{"file": cfg.SystemPromptFile},
		InputTokens: CountTokensApproxString(sysContent),
	})

	env := environment.LoadEnvironment(paths, cfg)
	envContent := environment.FormatEnvironment(env)
	envSections := environment.ExtractSectionNames(envContent)
	s.Append(config.Message{
		ID:          "env0",
		Role:        config.RoleSystem,
		Text:        envContent,
		Label:       "Environment",
		Params:      map[string]string{"sections": strings.Join(envSections, ", ")},
		InputTokens: CountTokensApproxString(envContent),
	})

	if cfg.AgentSystem != "" {
		s.Append(config.Message{
			ID:          "agent0",
			Role:        config.RoleSystem,
			Text:        cfg.AgentSystem,
			Label:       "Agent System",
			InputTokens: CountTokensApproxString(cfg.AgentSystem),
		})
	}

	s.Append(BuildConfigMsg(cfg.Inference))
	if toolsMsg := BuildToolsEnabledMsg(s.GetTools()); toolsMsg.ID != "" {
		s.Append(toolsMsg)
	}

	return s
}

// LoadSession wraps an existing session document.
func LoadSession(sd config.SessionDoc, name string) *Session {
	info := config.SessionInfo{Name: name}
	if sd.Meta.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, sd.Meta.UpdatedAt); err == nil {
			info.ModTime = t
		}
	}
	return &Session{Doc: sd, Info: info}
}

func (s *Session) Append(msg config.Message) { s.Doc.Messages = append(s.Doc.Messages, msg) }

func (s *Session) TruncateTo(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(s.Doc.Messages) {
		return
	}
	s.Doc.Messages = s.Doc.Messages[:n]
}

func (s *Session) TruncateToUser() (userText, userImage string) {
	n := len(s.Doc.Messages)
	for i := n - 1; i >= 0; i-- {
		if s.Doc.Messages[i].Role == config.RoleUser {
			userText, userImage = s.Doc.Messages[i].Text, s.Doc.Messages[i].ImagePath
			s.TruncateTo(i)
			return userText, userImage
		}
	}
	return "", ""
}

func (s *Session) CancelTruncate() (userText, userImage string, truncated bool) {
	n := len(s.Doc.Messages)
	if n == 0 {
		return "", "", false
	}
	for i := n - 1; i >= 0; i-- {
		if s.Doc.Messages[i].Role == config.RoleUser {
			userText, userImage = s.Doc.Messages[i].Text, s.Doc.Messages[i].ImagePath
			break
		}
	}
	if n > 0 && s.Doc.Messages[n-1].Role == config.RoleUser {
		s.TruncateTo(n - 1)
		truncated = true
	}
	return userText, userImage, truncated
}

func (s *Session) HasUserMessage() bool {
	for _, msg := range s.Doc.Messages {
		if msg.Role == config.RoleUser {
			return true
		}
	}
	return false
}

func (s *Session) TotalInputTokens() int {
	total := 0
	for _, msg := range s.Doc.Messages {
		total += msg.InputTokens
	}
	return total
}

func (s *Session) TotalOutputTokens() int {
	total := 0
	for _, msg := range s.Doc.Messages {
		total += msg.TextMetrics.Tokens + msg.ToolCallMetrics.Tokens
	}
	return total
}

func (s *Session) TotalTokens() int { return s.TotalInputTokens() + s.TotalOutputTokens() }

func (s *Session) Messages() []config.Message { return s.Doc.Messages }

func (s *Session) BuildMessages() []goai_provider.Message { return BuildAPIMessages(s.Doc.Messages) }

func (s *Session) GetTools() []tools.Tool {
	allowed := make(map[string]bool, len(s.Doc.Config.Tools))
	for _, name := range s.Doc.Config.Tools {
		allowed[name] = true
	}
	registered := tools.GetTools()
	out := make([]tools.Tool, 0, len(s.Doc.Config.Tools))
	for _, tool := range registered {
		if allowed[tool.Name] {
			out = append(out, tool)
		}
	}
	return out
}

func (s *Session) GetTool(name string) *tools.Tool {
	for _, allowed := range s.Doc.Config.Tools {
		if allowed == name {
			return tools.GetRegistry().Get(name)
		}
	}
	return nil
}

func (s *Session) SetTools(names []string) {
	s.Doc.Config.Tools = append([]string(nil), names...)
}

func (s *Session) GetSkills() []string { return append([]string(nil), s.Doc.Config.Skills...) }

func (s *Session) SetSkills(names []string) {
	s.Doc.Config.Skills = append([]string(nil), names...)
}

func (s *Session) CurrentSkill() string { return s.Doc.Config.ActiveSkill }

func (s *Session) EffectiveSkill() string {
	if s.Doc.Pending != nil && s.Doc.Pending.ActiveSkill != nil {
		return *s.Doc.Pending.ActiveSkill
	}
	return s.Doc.Config.ActiveSkill
}

func (s *Session) SetCurrentSkill(name string) {
	s.Doc.Config.ActiveSkill = name
	if s.Doc.Pending != nil {
		s.Doc.Pending.ActiveSkill = nil
	}
}

func (s *Session) PendingSkill() *string {
	if s.Doc.Pending != nil {
		return s.Doc.Pending.ActiveSkill
	}
	return nil
}

func (s *Session) SetPendingSkill(name string) {
	if s.Doc.Pending == nil {
		s.Doc.Pending = &config.PendingConfig{}
	}
	s.Doc.Pending.ActiveSkill = &name
}

func (s *Session) CurrentInference() config.InferenceConfig { return s.Doc.Config.Inference }

func (s *Session) EffectiveInference() config.InferenceConfig {
	if s.Doc.Pending != nil && s.Doc.Pending.Inference != nil {
		return *s.Doc.Pending.Inference
	}
	return s.Doc.Config.Inference
}

func (s *Session) DesiredInference(fallback config.InferenceConfig) config.InferenceConfig {
	if s.Doc.Pending != nil && s.Doc.Pending.Inference != nil {
		return *s.Doc.Pending.Inference
	}
	return fallback
}

func (s *Session) SetPendingInference(cfg config.InferenceConfig) {
	if cfg == s.Doc.Config.Inference {
		if s.Doc.Pending != nil {
			s.Doc.Pending.Inference = nil
		}
		return
	}

	// Before the first user turn, inference changes redefine the session's
	// bootstrap configuration rather than creating a transition message.
	if !s.HasUserMessage() && s.PushConfigChange(cfg) {
		if s.Doc.Pending != nil {
			s.Doc.Pending.Inference = nil
		}
		return
	}

	if s.Doc.Pending == nil {
		s.Doc.Pending = &config.PendingConfig{}
	}
	s.Doc.Pending.Inference = &cfg
}

func (s *Session) SetInference(cfg config.InferenceConfig) {
	s.Doc.Config.Inference = cfg
	if s.Doc.Pending != nil {
		s.Doc.Pending.Inference = nil
	}
}

func (s *Session) PushConfigChange(cfg config.InferenceConfig) bool {
	if s.HasUserMessage() {
		return false
	}

	for i := range s.Doc.Messages {
		if s.Doc.Messages[i].ID == "config0" {
			s.Doc.Messages[i] = BuildConfigMsg(cfg)
			s.Doc.Initial.Inference = cfg
			s.Doc.Config.Inference = cfg
			return true
		}
	}

	return false
}

func (s *Session) PushSystemPromptChange(oldFile, newFile string, paths config.Paths) {
	for i := range s.Doc.Messages {
		if s.Doc.Messages[i].ID == "sys0" {
			newContent := config.LoadSystemPrompt(paths, newFile)
			s.Doc.Messages[i].Text = newContent
			s.Doc.Messages[i].Label = "System Prompt"
			s.Doc.Messages[i].Params = map[string]string{"file": newFile}
			s.Doc.Messages[i].InputTokens = CountTokensApproxString(newContent)
			s.Append(config.Message{
				ID:     fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1),
				Role:   config.RoleInternal,
				Text:   fmt.Sprintf("Switched system prompt from %s to %s", oldFile, newFile),
				Label:  "System Prompt Changed",
				Params: map[string]string{"from": oldFile, "to": newFile},
			})
			return
		}
	}
}

func (s *Session) PushThinkingSwitch(thinking config.ThinkingConfig) {
	to := "off"
	if thinking.Enabled {
		to = "on"
	}
	s.Append(config.Message{
		ID:     fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1),
		Role:   config.RoleInternal,
		Text:   fmt.Sprintf("Switched thinking %s", to),
		Label:  "Thinking Switched",
		Params: map[string]string{"to": to},
	})
}

func (s *Session) PushModelSwitch(oldModel, newModel string) {
	s.Append(config.Message{
		ID:     fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1),
		Role:   config.RoleInternal,
		Text:   fmt.Sprintf("Switched model from %s to %s", oldModel, newModel),
		Label:  "Model Switched",
		Params: map[string]string{"from": oldModel, "to": newModel},
	})
}

func (s *Session) Save(p config.Paths, name string) error {
	s.Doc.TotalTokens = s.TotalTokens()
	return config.SaveSessionDoc(p, name, s.Doc)
}

func BuildConfigMsg(inf config.InferenceConfig) config.Message {
	thinkStr := "off"
	if inf.Thinking.Enabled {
		thinkStr = "on"
	}
	return config.Message{
		ID:     "config0",
		Role:   config.RoleInternal,
		Label:  "Init Config",
		Params: map[string]string{"provider": inf.Provider, "model": inf.Model, "thinking": thinkStr},
	}
}

func BuildToolsEnabledMsg(tl []tools.Tool) config.Message {
	if len(tl) == 0 {
		return config.Message{}
	}
	var names []string
	for _, t := range tl {
		names = append(names, t.Name)
	}
	rawJSON, _ := MarshalToolsJSON(tl)
	return config.Message{
		ID:          "tools0",
		Role:        config.RoleInternal,
		Label:       "Tools Enabled",
		Params:      map[string]string{"tools": strings.Join(names, ", ")},
		InputTokens: CountTokensApproxString(string(rawJSON)),
	}
}

func CountTokensApproxString(s string) int {
	n := len(s) / 4
	if n == 0 && len(s) > 0 {
		n = 1
	}
	return n
}

func NewUserMessage(id, text, imagePath string) config.Message {
	return config.Message{
		ID:          id,
		Role:        config.RoleUser,
		CreatedAt:   time.Now(),
		Text:        text,
		ImagePath:   imagePath,
		InputTokens: CountTokensApproxString(text),
	}
}

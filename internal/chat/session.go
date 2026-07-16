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
}

// SessionConfig carries initial session settings for CLI, subagents, and TUI.
type SessionConfig struct {
	Provider            string
	Model               string
	Thinking            config.ThinkingConfig
	SystemPromptFile    string
	Tools               []string
	Skills              []string
	WorkingDir          string
	MaxToolResultTokens int
	DebugEnabled        bool
}

// NewSession creates a pure session with initial system/environment/config/tool messages.
func NewSession(cfg SessionConfig, paths config.Paths) *Session {
	inf := config.InferenceConfig{Provider: cfg.Provider, Model: cfg.Model, Thinking: cfg.Thinking}
	doc := config.NewSessionDoc(inf, cfg.SystemPromptFile, cfg.WorkingDir, cfg.Tools, cfg.Skills)
	doc.Session.MaxToolResultTokens = cfg.MaxToolResultTokens
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

	env := environment.LoadEnvironment(paths, cfg.WorkingDir, cfg.DebugEnabled)
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

	s.Append(BuildConfigMsg(cfg.Provider, cfg.Model, cfg.Thinking))
	if toolsMsg := BuildToolsEnabledMsg(s.GetTools()); toolsMsg.Text != "" {
		s.Append(toolsMsg)
	}

	return s
}

// LoadSession wraps an existing session document.
func LoadSession(sd config.SessionDoc) *Session { return &Session{Doc: sd} }

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
	allowed := s.Doc.Session.Tools.Current
	if len(allowed) == 0 {
		return tools.GetTools()
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	var out []tools.Tool
	for _, t := range tools.GetTools() {
		if allowedSet[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

func (s *Session) SetTools(names []string) {
	s.Doc.Session.Tools.Current = append([]string(nil), names...)
}

func (s *Session) GetSkills() []string { return append([]string(nil), s.Doc.Session.Skills.Current...) }

func (s *Session) SetSkills(names []string) {
	s.Doc.Session.Skills.Current = append([]string(nil), names...)
}

func (s *Session) CurrentSkill() string { return s.Doc.Session.Skill.Current }

func (s *Session) SetCurrentSkill(name string) {
	s.Doc.Session.Skill.Current = name
	s.Doc.Session.Skill.Next = nil
}

func (s *Session) PendingSkill() *string { return s.Doc.Session.Skill.Next }

func (s *Session) SetPendingSkill(name string) { s.Doc.Session.Skill.Next = &name }

func (s *Session) CurrentInference() config.InferenceConfig { return s.Doc.Session.Inference.Current }

func (s *Session) SetInference(cfg config.InferenceConfig) { s.Doc.Session.Inference.Current = cfg }

func (s *Session) PushConfigChange(provider, model string, thinking config.ThinkingConfig) {
	if s.HasUserMessage() {
		return
	}
	for i := range s.Doc.Messages {
		if s.Doc.Messages[i].ID == "config0" {
			s.Doc.Messages[i] = BuildConfigMsg(provider, model, thinking)
			s.Doc.Session.Inference.Initial = config.InferenceConfig{Provider: provider, Model: model, Thinking: thinking}
			s.Doc.Session.Inference.Current = config.InferenceConfig{Provider: provider, Model: model, Thinking: thinking}
			return
		}
	}
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

func BuildConfigMsg(provider, model string, thinking config.ThinkingConfig) config.Message {
	thinkStr := "off"
	if thinking.Enabled {
		thinkStr = "on"
	}
	return config.Message{
		ID:     "config0",
		Role:   config.RoleInternal,
		Label:  "Init Config",
		Params: map[string]string{"provider": provider, "model": model, "thinking": thinkStr},
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

package chat

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	goai_provider "github.com/zendev-sh/goai/provider"

	"squid-os/internal/config"
	"squid-os/internal/environment"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/tools"
)

// Session is the pure runtime session: persisted document + pure stream state.
type Session struct {
	Doc        config.SessionDoc
	Stream     StreamState
	Info       config.SessionInfo
	Paths      config.Paths
	Catalog    runtimeconfig.Catalog
	SessionDir string // runtime-only: resolved session directory for persistence (not serialized into SessionDoc)
}

// NewRootSession creates a root session with a canonical directory. The directory
// is reserved in memory immediately; persistence still depends on Autosave.Enabled
// or an explicit save.
func NewRootSession(cfg config.SessionConfig, paths config.Paths, catalog runtimeconfig.Catalog) *Session {
	name := cfg.Autosave.Name
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04-05")
	}
	sessionDir := config.RootSessionDir(paths, name)
	doc := config.NewSessionDoc(cfg)
	s := &Session{Doc: doc, Info: config.SessionInfo{Name: name}, Paths: paths, Catalog: catalog, SessionDir: sessionDir}
	return initSessionMessages(s, cfg)
}

// NewChildSession creates a child session beneath its immediate parent.
func NewChildSession(cfg config.SessionConfig, identity config.SessionIdentity, parentSessionDir string, paths config.Paths, catalog runtimeconfig.Catalog) (*Session, error) {
	if !cfg.Autosave.Enabled {
		return nil, fmt.Errorf("child session persistence must be enabled")
	}
	if err := config.ValidateSessionName(cfg.Autosave.Name); err != nil {
		return nil, fmt.Errorf("child session name: %w", err)
	}
	sessionDir := config.ChildSessionDir(parentSessionDir, cfg.Autosave.Name)
	doc := config.NewSessionDocWithIdentity(cfg, identity)
	s := &Session{Doc: doc, Info: config.SessionInfo{Name: cfg.Autosave.Name}, Paths: paths, Catalog: catalog, SessionDir: sessionDir}
	return initSessionMessages(s, cfg), nil
}

func initSessionMessages(s *Session, cfg config.SessionConfig) *Session {
	paths := s.Paths
	catalog := s.Catalog

	sysContent := config.LoadSystemPrompt(paths, cfg.SystemPromptFile)
	s.Append(config.Message{
		ID:          "sys0",
		Role:        config.RoleSystem,
		Text:        sysContent,
		Label:       "System Prompt",
		Params:      map[string]string{"file": cfg.SystemPromptFile},
		InputTokens: CountTokensApproxString(sysContent),
	})

	env := environment.LoadEnvironment(paths, cfg, catalog)
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
		agentText := "## Agent [" + cfg.AgentName + "]\n\n" + cfg.AgentSystem
		s.Append(config.Message{
			ID:          "agent0",
			Role:        config.RoleSystem,
			Text:        agentText,
			Label:       "Agent System",
			Params:      map[string]string{"name": cfg.AgentName},
			InputTokens: CountTokensApproxString(agentText),
		})
	}

	s.Append(BuildConfigMsg(cfg.Inference, cfg.Target))
	if toolsMsg := BuildToolsEnabledMsg(s.GetTools()); toolsMsg.ID != "" {
		s.Append(toolsMsg)
	}

	s.RefreshTokenTally()
	return s
}

// LoadSession wraps a persisted session at its canonical directory and repairs
// any tool batch interrupted by a prior process crash before returning it.
func LoadSession(sd config.SessionDoc, sessionDir string, paths config.Paths, catalog runtimeconfig.Catalog) (*Session, error) {
	info := config.SessionInfo{Name: filepath.Base(sessionDir)}
	if sd.Meta.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, sd.Meta.UpdatedAt); err == nil {
			info.ModTime = t
		}
	}
	s := &Session{Doc: sd, Info: info, Paths: paths, Catalog: catalog, SessionDir: sessionDir}
	if msgIdx, repaired := s.repairInterruptedTools(); repaired {
		FlushToolMessage(s, msgIdx)
		if err := s.Save(); err != nil {
			return nil, fmt.Errorf("persist interrupted tool recovery: %w", err)
		}
	}
	s.RefreshTokenTally()
	return s, nil
}

// LoadRootSession resolves a root name and loads it at its canonical directory.
func LoadRootSession(sd config.SessionDoc, name string, paths config.Paths, catalog runtimeconfig.Catalog) (*Session, error) {
	return LoadSession(sd, config.RootSessionDir(paths, name), paths, catalog)
}

func (s *Session) repairInterruptedTools() (int, bool) {
	for msgIdx := range s.Doc.Messages {
		entries := s.Doc.Messages[msgIdx].ToolCalls
		firstRunning := -1
		for i := range entries {
			if entries[i].Execution.Status == tools.ResultStatusRunning {
				firstRunning = i
				break
			}
		}
		if firstRunning < 0 {
			continue
		}

		entries[firstRunning].Execution.Status = tools.ResultStatusError
		entries[firstRunning].Execution.Result = ""
		entries[firstRunning].Execution.Error = "interrupted during execution"
		for i := firstRunning + 1; i < len(entries); i++ {
			switch entries[i].Execution.Status {
			case "", tools.ResultStatusPending, tools.ResultStatusRunning:
				entries[i].Execution.Status = tools.ResultStatusError
				entries[i].Execution.Result = ""
				entries[i].Execution.Error = "not executed due to runtime interruption"
			}
		}
		s.Doc.Messages[msgIdx].ToolCalls = entries
		return msgIdx, true
	}
	return -1, false
}

// Save persists the session in its canonical directory.
func (s *Session) Save() error {
	s.RefreshTokenTally()
	return config.SaveSessionDoc(s.SessionDir, s.Doc, s.Doc.TokenTally)
}

func (s *Session) Append(msg config.Message) {
	s.Doc.Messages = append(s.Doc.Messages, msg)
	s.RefreshTokenTally()
}

func (s *Session) TruncateTo(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(s.Doc.Messages) {
		return
	}
	s.Doc.Messages = s.Doc.Messages[:n]
	s.RefreshTokenTally()
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

func (s *Session) Messages() []config.Message { return s.Doc.Messages }

func (s *Session) BuildMessages() []goai_provider.Message { return BuildAPIMessages(s.Doc.Messages) }

// BuildContext returns the next provider-message snapshot and refreshes its context tally.
func (s *Session) BuildContext() Context {
	ctx := BuildContext(s.Doc.Messages, s.Doc.Config.ContextCompaction)

	if s.Doc.TokenTally == nil {
		s.Doc.TokenTally = &config.TokenTally{}
	}
	s.Doc.TokenTally.Context = ctx.TokenTally()

	return ctx
}

func (s *Session) ToolContext(toolCallID string, childRef tools.ChildSessionRef) tools.RuntimeContext {
	return tools.RuntimeContext{
		Config:     s.Doc.Config,
		Catalog:    s.Catalog,
		Identity:   s.Doc.Identity,
		SessionDir: s.SessionDir,
		ToolCallID: toolCallID,
		ChildRef:   childRef,
	}
}

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

func (s *Session) GetSkills() []config.CapabilityRef {
	return append([]config.CapabilityRef(nil), s.Doc.Config.Skills...)
}

func (s *Session) SetSkills(refs []config.CapabilityRef) {
	s.Doc.Config.Skills = append([]config.CapabilityRef(nil), refs...)
}

func (s *Session) GetAgents() []config.CapabilityRef {
	return append([]config.CapabilityRef(nil), s.Doc.Config.Agents...)
}

func (s *Session) SetAgents(refs []config.CapabilityRef) {
	s.Doc.Config.Agents = append([]config.CapabilityRef(nil), refs...)
}

// SetWorkingDir atomically swaps workspace catalog and effective capability state.
func (s *Session) SetWorkingDir(path string) (string, error) {
	catalog, err := runtimeconfig.LoadCatalog(s.Paths, path)
	if err != nil {
		return "", err
	}
	resolved := catalog.Resolve(s.Doc.Config.SkillPolicy, s.Doc.Config.AgentPolicy)

	active := s.Doc.Config.ActiveSkill
	oldActive, oldOK := findCapabilityRef(s.Doc.Config.Skills, active)
	newActive, newOK := findCapabilityRef(resolved.Skills, active)

	s.Catalog = catalog
	s.Doc.Config.WorkingDir = path
	s.SetSkills(resolved.Skills)
	s.SetAgents(resolved.Agents)
	if active != "" && (!oldOK || !newOK || oldActive != newActive) {
		s.Doc.Config.ActiveSkill = ""
	}
	return formatWorkspaceState(s.Paths, path, catalog.FormatCapabilities(s.Doc.Config)), nil
}

func formatWorkspaceState(paths config.Paths, workingDir, capabilities string) string {
	info := environment.LoadProjectInfo(workingDir, paths.ProjectDir)
	var b strings.Builder
	b.WriteString("## [Project]\n")
	b.WriteString(environment.FormatProjectInfo(info))
	workspaceRoot := filepath.Join(workingDir, ".squid-os")
	b.WriteString("- workspace-memory: " + filepath.Join(workspaceRoot, "memory") + "\n")
	b.WriteString("- workspace-skills: " + filepath.Join(workspaceRoot, "skills") + "\n")
	b.WriteString("- workspace-agents: " + filepath.Join(workspaceRoot, "agents") + "\n\n")
	b.WriteString(capabilities)
	return b.String()
}

func findCapabilityRef(refs []config.CapabilityRef, name string) (config.CapabilityRef, bool) {
	for _, ref := range refs {
		if ref.Name == name {
			return ref, true
		}
	}
	return config.CapabilityRef{}, false
}

func (s *Session) CurrentSkill() string { return s.Doc.Config.ActiveSkill }

func (s *Session) CurrentTarget() string { return s.Doc.Config.Target }

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
			s.Doc.Messages[i] = BuildConfigMsg(cfg, s.Doc.Config.Target)
			s.Doc.Initial.Inference = cfg
			s.Doc.Config.Inference = cfg
			s.RefreshTokenTally()
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

func BuildConfigMsg(inf config.InferenceConfig, target string) config.Message {
	thinkStr := "off"
	if inf.Thinking.Enabled {
		thinkStr = "on"
	}
	params := map[string]string{"provider": inf.Provider, "model": inf.Model, "thinking": thinkStr}
	if target != "" {
		params["session-mode"] = target
	}
	return config.Message{
		ID:     "config0",
		Role:   config.RoleInternal,
		Label:  "Init Config",
		Params: params,
	}
}

func BuildToolsEnabledMsg(tl []tools.Tool) config.Message {
	if len(tl) == 0 {
		return config.Message{}
	}
	var names []string
	var textLines []string
	for _, t := range tl {
		names = append(names, t.Name)
		textLines = append(textLines, fmt.Sprintf("- %s: %s", t.Name, t.Description))
	}
	rawJSON, _ := MarshalToolsJSON(tl)
	return config.Message{
		ID:          "tools0",
		Role:        config.RoleInternal,
		Text:        strings.Join(textLines, "\n"),
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

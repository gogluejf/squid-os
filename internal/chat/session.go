package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	goai_provider "github.com/zendev-sh/goai/provider"

	"squid-os/internal/config"
	"squid-os/internal/environment"
	"squid-os/internal/media"
	"squid-os/internal/chat/provider"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/tools"
)

// Session is the pure runtime session: persisted document + pure stream state.
type Session struct {
	Doc     config.SessionDoc
	Stream  StreamState
	Info    config.SessionInfo
	Paths   config.Paths
	Catalog runtimeconfig.Catalog
	// SessionDir is the canonical or provisional directory that would contain
	// chat.json. It is always set, even before the session has been persisted.
	SessionDir string
	// Workspace manages session-local media files. May be nil for sessions
	// that haven't initialized attachment support yet.
	Workspace media.Workspace
	// tempWorkspaceDir holds the path to the temporary workspace directory
	// for unsaved sessions. After the first explicit save, this is cleared.
	tempWorkspaceDir string
	// isIncognito indicates whether this session is running in incognito mode.
	// Incognito sessions use isolated temporary workspaces that are removed
	// on normal exit and never become visible as saved sessions.
	isIncognito bool
	// capsCache / capsCacheKey cache ModelCapabilities per (provider, model).
	capsCache  goai_provider.ModelCapabilities
	capsCacheKey string
}

// NewRootSession creates a root session with a canonical directory. Persistence
// still depends on Autosave.Enabled or an explicit save.
func NewRootSession(cfg config.SessionConfig, paths config.Paths, catalog runtimeconfig.Catalog) *Session {
	name := sessionName(cfg.Autosave.Name)
	id := uuid.New().String()
	return newSessionWithIdentity(cfg, config.SessionIdentity{ID: id, RootID: id}, config.RootSessionDir(paths, name), paths, catalog)
}

// NewChildSession creates a child session beneath its immediate parent.
func NewChildSession(cfg config.SessionConfig, identity config.SessionIdentity, parentSessionDir string, paths config.Paths, catalog runtimeconfig.Catalog) *Session {
	name := sessionName(cfg.Autosave.Name)
	return newSessionWithIdentity(cfg, identity, config.ChildSessionDir(parentSessionDir, name), paths, catalog)
}

func newSessionWithIdentity(cfg config.SessionConfig, identity config.SessionIdentity, sessionDir string, paths config.Paths, catalog runtimeconfig.Catalog) *Session {
	name := filepath.Base(sessionDir)
	if cfg.Autosave.Name == "" {
		cfg.Autosave.Name = name
	}
	doc := config.NewSessionDocWithIdentity(cfg, identity)
	s := &Session{Doc: doc, Paths: paths, Catalog: catalog, SessionDir: sessionDir}
	return initSessionMessages(s, cfg)
}

func sessionName(name string) string {
	if name != "" {
		return name
	}
	return time.Now().Format("2006-01-02_15-04-05")
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

// LoadSession wraps a persisted session at its canonical source directory. For
// roots, a different configured autosave name forks the complete tree first.
// Children always remain at their supplied nested directory.
func LoadSession(sd config.SessionDoc, sessionDir string, paths config.Paths, catalog runtimeconfig.Catalog) (*Session, error) {
	currentName := filepath.Base(sessionDir)
	isRoot := sd.Identity.Depth == 0 && sd.Identity.ParentID == ""
	if !isRoot && sd.Config.Autosave.Enabled && sd.Config.Autosave.Name != "" && sd.Config.Autosave.Name != currentName {
		return nil, fmt.Errorf("child session save-as is not supported: current=%q requested=%q", currentName, sd.Config.Autosave.Name)
	}

	locationChanged := false
	if isRoot && sd.Config.Autosave.Enabled {
		destinationName := sd.Config.Autosave.Name
		if destinationName != "" && destinationName != filepath.Base(sessionDir) {
			destinationDir := config.RootSessionDir(paths, destinationName)
			if _, err := config.ForkSessionTree(sessionDir, destinationDir); err != nil {
				return nil, fmt.Errorf("fork loaded session: %w", err)
			}
			forked, err := config.LoadSessionDoc(destinationDir)
			if err != nil {
				return nil, fmt.Errorf("load forked session: %w", err)
			}
			sd = forked
			sd.Config.Autosave.Name = destinationName
			sessionDir = destinationDir
			locationChanged = true
		}
	}

	info := config.SessionInfo{Name: filepath.Base(sessionDir)}
	if sd.Meta.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, sd.Meta.UpdatedAt); err == nil {
			info.ModTime = t
		}
	}
	s := &Session{Doc: sd, Info: info, Paths: paths, Catalog: catalog, SessionDir: sessionDir}
	s.InitWorkspace()
	if locationChanged {
		if err := s.Save(); err != nil {
			return nil, fmt.Errorf("persist fork destination config: %w", err)
		}
	}
	if msgIdx, repaired := s.repairInterruptedTools(); repaired {
		FlushToolMessage(s, msgIdx)
		if err := s.Save(); err != nil {
			return nil, fmt.Errorf("persist interrupted tool recovery: %w", err)
		}
	}
	s.RefreshTokenTally()
	return s, nil
}

// LoadRootSession resolves the source root name before delegating to the generic
// loader. LoadSession may fork the root when its configured autosave name differs.
func LoadRootSession(sd config.SessionDoc, sourceName string, paths config.Paths, catalog runtimeconfig.Catalog) (*Session, error) {
	return LoadSession(sd, config.RootSessionDir(paths, sourceName), paths, catalog)
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
	if s.tempWorkspaceDir != "" && s.Workspace != nil {
		result, err := media.MigrateTempWorkspace(context.Background(), s.Workspace, s.SessionDir)
		if err != nil {
			return fmt.Errorf("migrate temp workspace: %w", err)
		}
		s.Workspace = result.Workspace
		s.tempWorkspaceDir = ""
	}

	s.RefreshTokenTally()
	return config.SaveSessionDoc(s.SessionDir, s.Doc, s.Doc.TokenTally)
}

func (s *Session) Append(msg config.Message) {
	if msg.Role == config.RoleUser {
		msg.Text = s.resolveFileReferences(msg.Text)
		msg.Attachments = s.resolveAttachmentRefs(msg.Text)
	}
	s.Doc.Messages = append(s.Doc.Messages, msg)
	s.RefreshTokenTally()
}

// resolveAttachmentRefs returns an AttachmentRef for each attachment whose
// canonical reference appears in the message text. This is stored on the
// message at creation time so BuildContext doesn't re-scan the text.
func (s *Session) resolveAttachmentRefs(text string) []config.AttachmentRef {
	var refs []config.AttachmentRef
	for _, a := range s.Doc.Attachments {
		if strings.Contains(text, a.CanonicalRef()) {
			refs = append(refs, config.AttachmentRef{
				File:   a.FileName,
				Tokens: EstimateAttachmentTokens(a),
			})
		}
	}
	return refs
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

func (s *Session) TruncateToUser() (userText string) {
	n := len(s.Doc.Messages)
	for i := n - 1; i >= 0; i-- {
		if s.Doc.Messages[i].Role == config.RoleUser {
			userText = s.Doc.Messages[i].Text
			s.TruncateTo(i)
			return userText
		}
	}
	return ""
}

func (s *Session) CancelTruncate() (userText string, truncated bool) {
	n := len(s.Doc.Messages)
	if n == 0 {
		return "", false
	}
	for i := n - 1; i >= 0; i-- {
		if s.Doc.Messages[i].Role == config.RoleUser {
			userText = s.Doc.Messages[i].Text
			break
		}
	}
	if n > 0 && s.Doc.Messages[n-1].Role == config.RoleUser {
		s.TruncateTo(n - 1)
		truncated = true
	}
	return userText, truncated
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
	caps := s.ModelCaps()
	ctx := BuildContext(s.Doc.Messages, s.Doc.Config.ContextCompaction, s.mediaBaseDir(), s.Doc.Attachments, &caps)

	if s.Doc.TokenTally == nil {
		s.Doc.TokenTally = &config.TokenTally{}
	}
	s.Doc.TokenTally.Context = ctx.TokenTally()

	return ctx
}

func (s *Session) ToolContext(toolCallID string, childRef tools.ChildSessionRef) tools.RuntimeContext {
	s.EnsureWorkspace()
	var ingestSvc *media.IngestService
	if s.Workspace != nil {
		ingestSvc = media.NewIngestService(s.Workspace, media.DefaultLimits,
			func() []media.Attachment { return s.Doc.Attachments },
			func(a media.Attachment) { s.AddAttachment(a) },
		)
	}
	return tools.RuntimeContext{
		Config:        s.Doc.Config,
		Catalog:       s.Catalog,
		Identity:      s.Doc.Identity,
		SessionDir:    s.SessionDir,
		ToolCallID:    toolCallID,
		ChildRef:      childRef,
		IngestService: ingestSvc,
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

// ReloadCatalog atomically swaps workspace catalog and effective capability state.
func (s *Session) ReloadCatalog(path string) (string, error) {
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
	s.InvalidateCaps()
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
	s.InvalidateCaps()
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

func NewUserMessage(id, text string) config.Message {
	return config.Message{
		ID:               id,
		Role:             config.RoleUser,
		CreatedAt:        time.Now(),
		Text:             text,
		InputTokens:      CountTokensApproxString(text),
	}
}

// --- Attachment Methods ---

// sessionExists checks whether the session's chat.json has been persisted.
func (s *Session) sessionExists() bool {
	_, err := os.Stat(filepath.Join(s.SessionDir, "chat.json"))
	return err == nil
}

// InitWorkspace initializes the session's media workspace based on its
// incognito state and persistence status.
func (s *Session) InitWorkspace() {
	if s.Workspace != nil {
		return
	}

	// Ensure Attachments slice is initialized so the workspace and session
	// share the same underlying array. Appending to s.Doc.Attachments will
	// be visible through the workspace's Registry.
	if s.Doc.Attachments == nil {
		s.Doc.Attachments = make([]media.Attachment, 0)
	}

	// Incognito sessions always use a temp workspace.
	if s.isIncognito {
		tempDir, err := os.MkdirTemp(s.Paths.TempFolder, "squid-incognito-*")
		if err != nil {
			return
		}
		s.Workspace = media.NewTempWorkspace(tempDir)
		s.tempWorkspaceDir = tempDir
		return
	}

	// Unsaved session: temp workspace. Persisted sessions get a persistent workspace.
	if !s.sessionExists() {
		tempDir, err := os.MkdirTemp(s.Paths.TempFolder, "squid-session-*")
		if err != nil {
			// Fallback to persistent workspace if temp creation fails.
			s.Workspace = media.NewPersistentWorkspace(s.SessionDir)
			return
		}
		s.Workspace = media.NewTempWorkspace(tempDir)
		s.tempWorkspaceDir = tempDir
		return
	}

	// Saved session: persistent workspace.
	s.Workspace = media.NewPersistentWorkspace(s.SessionDir)
}

// EnsureWorkspace returns the workspace, initializing it if needed.
func (s *Session) EnsureWorkspace() media.Workspace {
	if s.Workspace == nil {
		s.InitWorkspace()
	}
	return s.Workspace
}

// AddAttachment registers a new attachment in the session document.
func (s *Session) AddAttachment(a media.Attachment) {
	for _, existing := range s.Doc.Attachments {
		if existing.ID == a.ID {
			return
		}
	}
	s.Doc.Attachments = append(s.Doc.Attachments, a)
}

// GetAttachment resolves an attachment by ID or file name.
func (s *Session) GetAttachment(id string) (media.Attachment, string, error) {
	ws := s.EnsureWorkspace()
	if ws == nil {
		return media.Attachment{}, "", fmt.Errorf("workspace not initialized")
	}
	a, found := media.ResolveRef(s.Doc.Attachments, id)
	if !found {
		return media.Attachment{}, "", fmt.Errorf("attachment not found: %s", id)
	}
	return a, filepath.Join(ws.Dir(), a.FileName), nil
}

// mediaBaseDir returns the directory where session media files are stored.
// Before the first save this is the temporary workspace dir; after save
// (or on load) it is the session media dir.
func (s *Session) mediaBaseDir() string {
	if ws := s.EnsureWorkspace(); ws != nil {
		return ws.Dir()
	}
	return ""
}

// ModelCaps returns the capability set for the session's current model.
// Results are cached per (provider, model). Invalidate by calling
// InvalidateCaps() after a model switch.
func (s *Session) ModelCaps() goai_provider.ModelCapabilities {
	inf := s.CurrentInference()
	key := inf.Provider + "/" + inf.Model
	if s.capsCacheKey == key {
		return s.capsCache
	}
	s.capsCacheKey = key
	s.capsCache = goai_provider.ModelCapabilities{}
	// Capabilities are a pure function of (provider, model) — no credentials needed.
	// Lookup with nil settings constructs the provider shell; BuildGoAIModel
	// returns a GoAI model whose Capabilities() only inspects the model ID.
	prov := provider.Lookup(inf.Provider, nil)
	if prov == nil {
		return s.capsCache
	}
	langModel, _, err := prov.BuildGoAIModel(inf.Model)
	if err != nil {
		return s.capsCache
	}
	s.capsCache = goai_provider.ModelCapabilitiesOf(langModel)
	return s.capsCache
}

// InvalidateCaps clears the cached model capabilities. Call after a model switch.
func (s *Session) InvalidateCaps() {
	s.capsCacheKey = ""
	s.capsCache = goai_provider.ModelCapabilities{}
}

// attachmentByFile returns the attachment for a bare file name.
func (s *Session) attachmentByFile(fileName string) (media.Attachment, bool) {
	for _, a := range s.Doc.Attachments {
		if a.FileName == fileName {
			return a, true
		}
	}
	return media.Attachment{}, false
}

// HasAttachments returns true if the session has any persisted attachments.
func (s *Session) HasAttachments() bool {
	return len(s.Doc.Attachments) > 0
}

// SetIncognito toggles incognito mode for this session.
func (s *Session) SetIncognito(v bool) {
	s.isIncognito = v
	if v && s.Workspace != nil {
		// Switch to incognito temp workspace.
		tempDir, err := os.MkdirTemp(s.Paths.TempFolder, "squid-incognito-*")
		if err == nil {
			s.Workspace = media.NewTempWorkspace(tempDir)
			s.tempWorkspaceDir = tempDir
		}
	}
}

// CleanupWorkspace removes temporary workspace directories for incognito
// or unsaved sessions.
func (s *Session) CleanupWorkspace() error {
	if s.tempWorkspaceDir != "" {
		return os.RemoveAll(s.tempWorkspaceDir)
	}
	return nil
}

// resolveFileReferences scans message text for @file/path patterns and
// @file:<url> patterns, ingests them through the workspace, and replaces
// them with canonical @file:<id> references.
var fileURLPattern = regexp.MustCompile(`@file:https?://\S+`)
var bareURLPattern = regexp.MustCompile(`@https?://\S+`)
var fileRefPattern = regexp.MustCompile(`@file:\S+`)
var barePathPattern = regexp.MustCompile(`@([^\s@]+[./][^\s@]*)`)

func (s *Session) resolveFileReferences(text string) string {
	s.EnsureWorkspace()
	if s.Workspace == nil {
		return text
	}

	ingestSvc := media.NewIngestService(s.Workspace, media.DefaultLimits,
		func() []media.Attachment { return s.Doc.Attachments },
		func(a media.Attachment) { s.AddAttachment(a) },
	)

		// Resolve bare @https://<url> references (no file: prefix)
		text = bareURLPattern.ReplaceAllStringFunc(text, func(match string) string {
			urlStr := strings.TrimPrefix(match, "@")
			attach, err := ingestSvc.Ingest(context.Background(), media.IngestSource{
				Kind: media.IngestSourceKindURL,
				URL:  urlStr,
			})
			if err != nil {
				return match
			}
			return attach.CanonicalRef()
		})

		// Resolve @file:<url> references (explicit file: prefix)
		text = fileURLPattern.ReplaceAllStringFunc(text, func(match string) string {
			urlStr := strings.TrimPrefix(match, "@file:")
			attach, err := ingestSvc.Ingest(context.Background(), media.IngestSource{
				Kind: media.IngestSourceKindURL,
				URL:  urlStr,
			})
			if err != nil {
				return match
			}
			return attach.CanonicalRef()
		})

		// Resolve @file:<path> references (absolute or relative to working dir)
		text = fileRefPattern.ReplaceAllStringFunc(text, func(match string) string {
			relPath := strings.TrimPrefix(match, "@file:")
			var absPath string
			if filepath.IsAbs(relPath) {
				absPath = relPath
			} else {
				absPath = filepath.Join(s.Doc.Config.WorkingDir, relPath)
			}
			attach, err := ingestSvc.Ingest(context.Background(), media.IngestSource{
				Kind: media.IngestSourceKindFile,
				Path: absPath,
			})
			if err != nil {
				return match // leave original reference on error
			}
			return attach.CanonicalRef()
		})

		// Resolve bare @<path> references (e.g. @website/architecture.png)
		// Runs last so it only catches refs not already handled by the patterns above.
		text = barePathPattern.ReplaceAllStringFunc(text, func(match string) string {
			relPath := strings.TrimPrefix(match, "@")
			// Skip if it looks like a capability ref (skill:, agent:, tool:, file:)
			// Those would have been handled already, but guard against edge cases.
			for _, prefix := range []string{"skill:", "agent:", "tool:", "file:"} {
				if strings.HasPrefix(relPath, prefix) {
					return match
				}
			}
			var absPath string
			if filepath.IsAbs(relPath) {
				absPath = relPath
			} else {
				absPath = filepath.Join(s.Doc.Config.WorkingDir, relPath)
			}
			attach, err := ingestSvc.Ingest(context.Background(), media.IngestSource{
				Kind: media.IngestSourceKindFile,
				Path: absPath,
			})
			if err != nil {
				return match // leave original reference on error
			}
			return attach.CanonicalRef()
		})

	return text
}

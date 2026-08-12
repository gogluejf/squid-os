package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

// CapabilityScope identifies where an effective capability was discovered.
type CapabilityScope string

const (
	CapabilityScopeGlobal    CapabilityScope = "global"
	CapabilityScopeWorkspace CapabilityScope = "workspace"
)

// CapabilityRef identifies one effective capability. Name is unique within a catalog.
type CapabilityRef struct {
	Scope CapabilityScope `json:"scope"`
	Name  string          `json:"name"`
}

// CapabilityPolicy is the persistent rule used to resolve an effective list.
type CapabilityPolicy struct {
	Mode      string   `json:"mode"` // "all" or "allowlist"
	Requested []string `json:"requested,omitempty"`
}

// Role constants — used for message filtering in buildAPI and rendering
const (
	PolicyModeAll       = "all"
	PolicyModeAllowlist = "allowlist"
)

const (
	RoleUser      = "user"      // user context (chat)
	RoleAssistant = "assistant" // model output
	RoleSynthetic = "synthetic" // inject synthetic messages as assistant message, those message are shaped by the app
	RoleSystem    = "system"    // system prompt loaded from file; included in API
	RoleInternal  = "internal"  // metadata visible to user; excluded from API
)

// TokenTally is the structured token overview persisted in session files
// and consumed by footer rendering, Analytics, and future APIs.
type TokenTally struct {
	Lifetime LifetimeTokenTally `json:"lifetime"`
	Context  ContextTokenTally  `json:"context"`
}

// LifetimeTokenTally holds cumulative input and output token breakdowns
// across all messages in the session.
type LifetimeTokenTally struct {
	Input  InputTokenTally  `json:"input"`
	Output OutputTokenTally `json:"output"`
	Total  int              `json:"total"`
}

// InputTokenTally breaks down input tokens by source.
type InputTokenTally struct {
	User            int `json:"user"`
	ToolExecution   int `json:"tool_execution"`
	SystemPrompt    int `json:"system_prompt"`
	ToolDefinitions int `json:"tool_definitions"`
	Synthetic       int `json:"synthetic"`
	Total           int `json:"total"`
}

// OutputTokenTally breaks down output tokens by type.
type OutputTokenTally struct {
	Assistant int `json:"assistant"`
	Thinking  int `json:"thinking"`
	ToolCalls int `json:"tool_calls"`
	Total     int `json:"total"`
}

// ContextTokenTally holds exact next-request provider context token totals.
type ContextTokenTally struct {
	Raw              int `json:"raw"`
	RawInput         int `json:"raw_input"`
	RawOutput        int `json:"raw_output"`
	Compacted        int `json:"compacted"`
	CompactedInput   int `json:"compacted_input"`
	CompactedOutput  int `json:"compacted_output"`
	Saved            int `json:"saved"`
	SavedInstruction int `json:"saved_instruction"`
	SavedExecution   int `json:"saved_execution"`
}

// SessionIdentity holds immutable lineage fields for a session.
// Root sessions have ParentID and ParentToolCallID empty and Depth zero.
type SessionIdentity struct {
	ID               string `json:"id"`
	ParentID         string `json:"parent_id,omitempty"`
	RootID           string `json:"root_id"`
	ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
	Depth            int    `json:"depth"`
}

// SessionDoc is the persisted session document.
// Identity holds lineage. Meta holds timestamps. Initial holds provenance. Config holds current runtime state.
// Pending holds desired next state (nil = nothing pending).
type SessionDoc struct {
	Version     int                       `json:"version"`
	Identity    SessionIdentity           `json:"identity"`
	Meta        SessionMeta               `json:"meta"`
	Initial     SessionConfig             `json:"initial"`
	Config      SessionConfig             `json:"config"`
	Pending     *PendingConfig            `json:"pending,omitempty"`
	Messages    []Message                 `json:"messages"`
	TotalTokens int                       `json:"total_tokens,omitempty"` // legacy, kept for backward compat on load
	TokenTally  *TokenTally               `json:"token_tally,omitempty"`
	FileState   map[string]FileStateEntry `json:"file_state,omitempty"`
}

// SessionMeta holds timestamp-only fields that change on save.
type SessionMeta struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SessionConfig is the single runtime configuration for a session.
// Used by runtime.Resolve, chat.NewSession, and persisted in SessionDoc.
type SessionConfig struct {
	Inference         InferenceConfig   `json:"inference"`
	SystemPromptFile  string            `json:"system_prompt_file,omitempty"`
	Target            string            `json:"target,omitempty"` // "interactive" or "autonomous"
	AgentName         string            `json:"agent_name,omitempty"`
	AgentSystem       string            `json:"agent_system,omitempty"`
	ActiveSkill       string            `json:"active_skill,omitempty"`
	AuthMode          AuthorizationMode `json:"auth_mode,omitempty"`
	WorkingDir        string            `json:"working_dir,omitempty"`
	Tools             []string          `json:"tools,omitempty"`
	Skills            []CapabilityRef   `json:"skills,omitempty"`
	Agents            []CapabilityRef   `json:"agents,omitempty"`
	SkillPolicy       CapabilityPolicy  `json:"skill_policy"`
	AgentPolicy       CapabilityPolicy  `json:"agent_policy"`
	Memory            SessionMemory     `json:"memory,omitempty"`
	Autosave          SessionAutosave   `json:"autosave,omitempty"`
	Limits            SessionLimits     `json:"limits,omitempty"`
	DebugEnabled      bool              `json:"debug_enabled,omitempty"`
	ContextCompaction bool              `json:"context_compaction"`
}

// PendingConfig holds desired next-state changes. Non-nil fields are pending.
type PendingConfig struct {
	Target      *string          `json:"target,omitempty"`
	Inference   *InferenceConfig `json:"inference,omitempty"`
	ActiveSkill *string          `json:"active_skill,omitempty"`
	Tools       *[]string        `json:"tools,omitempty"`
	Skills      *[]CapabilityRef `json:"skills,omitempty"`
	Agents      *[]CapabilityRef `json:"agents,omitempty"`
}

type InferenceConfig struct {
	Provider string         `json:"provider"`
	Model    string         `json:"model"`
	Thinking ThinkingConfig `json:"thinking"`
}

type SessionMemory struct {
	Namespace    string `json:"namespace,omitempty"`
	Path         string `json:"path,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type SessionAutosave struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
}

type SessionLimits struct {
	MaxSteps            int    `json:"max_steps,omitempty"`
	MaxTools            int    `json:"max_tools,omitempty"`
	MaxToolResultTokens int    `json:"max_tool_result_tokens,omitempty"`
	MaxAgentDepth       int    `json:"max_agent_depth,omitempty"`
	MaxTime             string `json:"max_time,omitempty"`
}

type ContentMetrics struct {
	Tokens               int   `json:"tokens,omitempty"`
	InferenceDuractionMs int64 `json:"inference_duration_ms,omitempty"`
	TimeToFirstTokenMs   int64 `json:"time_to_first_token_ms,omitempty"`
}

// File tracking constants
const (
	TraceRead   = "read"
	TraceWrite  = "write"
	TraceCreate = "create"
	TraceEdit   = "edit"
	TraceDelete = "delete"
)

// FileEntry tracks a single file affected by a tool call.
type FileEntry struct {
	Path       string    `json:"path"`
	Trace      string    `json:"trace"`
	Checksum   string    `json:"checksum"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Time       time.Time `json:"time"`
	Diff       string    `json:"diff,omitempty"`
}

// FileStateEntry tracks the last known state of a file in a session/turn.
type FileStateEntry struct {
	Checksum   string    `json:"checksum"`
	Trace      string    `json:"trace"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ToolCallEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	// Instruction: what the model requested
	Instruction struct {
		Name       string `json:"name"`
		Arguments  string `json:"arguments"`
		Tokens     int    `json:"tokens,omitempty"`
		DurationMs int64  `json:"duration_ms,omitempty"`
	} `json:"instruction"`

	// Execution: result of running the tool (empty if not yet executed)
	Execution struct {
		Status     string      `json:"status,omitempty"`
		Result     string      `json:"result,omitempty"`
		Error      string      `json:"error,omitempty"`
		Tokens     int         `json:"tokens,omitempty"`
		DurationMs int64       `json:"duration_ms"`
		Files      []FileEntry `json:"files,omitempty"`

		// ChildSessionID and ChildSessionName identify a delegated agent run.
		// Empty for non-agent tools.
		ChildSessionID   string `json:"child_session_id,omitempty"`
		ChildSessionName string `json:"child_session_name,omitempty"`
	} `json:"execution,omitempty"`
}

type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`

	OutputTokens       int     `json:"output_tokens,omitempty"` // total output tokens for this message
	DurationTimeMs     int64   `json:"duration_ms,omitempty"`
	TimeToFirstTokenMs int64   `json:"time_to_first_token_ms,omitempty"`
	TokensPerSecond    float64 `json:"tok_per_sec,omitempty"`

	ImagePath   string `json:"image_path,omitempty"`
	InputTokens int    `json:"input_tokens"` // user message and execution tokens

	Text            string         `json:"text"`
	TextMetrics     ContentMetrics `json:"text_metrics,omitempty"`
	ThinkingText    string         `json:"thinking_text,omitempty"`
	ThinkingMetrics ContentMetrics `json:"thinking_metrics,omitempty"`

	ToolCalls       []ToolCallEntry `json:"tool_calls,omitempty"`
	ToolCallMetrics ContentMetrics  `json:"tool_call_metrics,omitempty"`

	SequenceStat *SequenceStat `json:"sequence_stat,omitempty"`

	StopReason string `json:"stop_reason,omitempty"`

	// Label is a human-readable label for synthetic/internal/system messages
	// (e.g. "Stream aborted by user", "System Prompt", "Tools Enabled").
	// Displayed as the primary title in the UI.
	Label string `json:"label,omitempty"`

	// Params is a key-value map of parameters for syntethic/system/internal messages.
	// they allow to provide second level metadata for the message, which can be used for display ( like tools with their params)
	// Rendered as styled chips next to the label, analogous to tool DisplayParam.
	// Not sent to the API — metadata only.
	Params map[string]string `json:"params,omitempty"`
}

// SessionInfo holds display metadata for a saved session.
type SessionInfo struct {
	Name    string
	ModTime time.Time
}

// TotalExecutionTokens sums the execution tokens across a slice of ToolCallEntry.
func TotalExecutionTokens(entries []ToolCallEntry) int {
	var total int
	for _, tc := range entries {
		total += tc.Execution.Tokens
	}
	return total
}

// NewSessionDoc creates a new empty session with independent initial/current config copies.
// The new session is a root: RootID equals ID, parent fields are empty, depth is zero.
func NewSessionDoc(cfg SessionConfig) SessionDoc {
	id := uuid.New().String()
	return NewSessionDocWithIdentity(cfg, SessionIdentity{
		ID:     id,
		RootID: id,
	})
}

// NewSessionDocWithIdentity creates a new empty session with the given identity.
// Used for delegated child sessions that have preallocated lineage.
func NewSessionDocWithIdentity(cfg SessionConfig, identity SessionIdentity) SessionDoc {
	now := time.Now().UTC().Format(time.RFC3339)
	return SessionDoc{
		Version:  1,
		Identity: identity,
		Meta: SessionMeta{
			CreatedAt: now,
			UpdatedAt: now,
		},
		Initial:  CloneSessionConfig(cfg),
		Config:   cfg,
		Messages: []Message{},
	}
}

func CloneSessionConfig(cfg SessionConfig) SessionConfig {
	cfg.Tools = append([]string(nil), cfg.Tools...)
	cfg.Skills = append([]CapabilityRef(nil), cfg.Skills...)
	cfg.Agents = append([]CapabilityRef(nil), cfg.Agents...)
	cfg.SkillPolicy.Requested = append([]string(nil), cfg.SkillPolicy.Requested...)
	cfg.AgentPolicy.Requested = append([]string(nil), cfg.AgentPolicy.Requested...)
	return cfg
}

// ValidateSessionName ensures the name cannot escape its parent directory via
// path traversal or absolute paths. Only valid filesystem names are allowed.
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("session name is empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("session name must not be an absolute path: %q", name)
	}
	if filepath.Clean(name) != name {
		return fmt.Errorf("session name must be clean: %q", name)
	}
	if containsDotDot(name) {
		return fmt.Errorf("session name must not contain path traversal: %q", name)
	}
	if containsSeparator(name) {
		return fmt.Errorf("session name must not contain path separators: %q", name)
	}
	return nil
}

func containsDotDot(s string) bool {
	for i := 0; i <= len(s)-2; i++ {
		if s[i] == '.' && s[i+1] == '.' {
			return true
		}
	}
	return false
}

func containsSeparator(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			return true
		}
	}
	return false
}

// RootSessionDir returns the filesystem directory for a root session by name.
// Layout: sessions/<name>/
func RootSessionDir(p Paths, name string) string {
	return filepath.Join(p.Sessions, name)
}

// ChildSessionOptions holds preallocated lineage for a delegated child session.
type ChildSessionOptions struct {
	ID               string
	ParentID         string
	RootID           string
	ParentToolCallID string
	Depth            int
	ParentSessionDir string
}

// ChildSessionDir returns the filesystem directory for a child session beneath
// its immediate parent. Layout: parentDir/agents/<childName>/
func ChildSessionDir(parentDir, childName string) string {
	return filepath.Join(parentDir, "agents", childName)
}

// SessionFilePath returns the chat document path for a resolved session directory.
// Layout: sessionDir/chat.json
func SessionFilePath(sessionDir string) string {
	return filepath.Join(sessionDir, "chat.json")
}

// SaveSessionDoc writes a session to sessionDir/chat.json.
// It persists TokenTally and clears the legacy scalar total_tokens.
func SaveSessionDoc(sessionDir string, doc SessionDoc, tally *TokenTally) error {
	if sessionDir == "" {
		return fmt.Errorf("session directory is required")
	}
	doc.Meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	doc.TokenTally = tally
	doc.TotalTokens = 0 // clear legacy scalar on save
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	path := SessionFilePath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.Chtimes(filepath.Dir(path), info.ModTime(), info.ModTime())
}

// LoadSessionDoc reads a session from sessionDir/chat.json.
func LoadSessionDoc(sessionDir string) (SessionDoc, error) {
	if sessionDir == "" {
		return SessionDoc{}, fmt.Errorf("session directory is required")
	}
	file := SessionFilePath(sessionDir)
	data, err := os.ReadFile(file)
	if err != nil {
		return SessionDoc{}, err
	}
	var sf SessionDoc
	if err := json.Unmarshal(data, &sf); err != nil {
		return SessionDoc{}, err
	}
	return sf, nil
}

// LoadChildSession resolves a child session from its parent's tool-call link,
// loads the child document, and validates its identity against the parent and
// tool call. The child is resolved as parentDir/agents/ChildSessionName/chat.json.
//
// Validation checks:
//   - ChildSessionName is present in the tool-call execution.
//   - The child file exists and deserializes.
//   - Child Identity.ID matches the tool-call's ChildSessionID.
//   - Child Identity.ParentID matches the parent's Identity.ID.
//   - Child Identity.RootID matches the parent's Identity.RootID.
//   - Child Identity.ParentToolCallID matches the tool-call's ID.
//   - Child Identity.Depth equals parent's Depth + 1.
//
// Returns the loaded child document and its runtime directory on success.
func LoadChildSession(parentDir string, parent SessionDoc, toolCall ToolCallEntry) (doc SessionDoc, childDir string, err error) {
	childName := toolCall.Execution.ChildSessionName
	if childName == "" {
		return SessionDoc{}, "", fmt.Errorf("tool call %q has no child session name", toolCall.ID)
	}

	dir := ChildSessionDir(parentDir, childName)

	child, err := LoadSessionDoc(dir)
	if err != nil {
		return SessionDoc{}, "", fmt.Errorf("load child session at %s: %w", SessionFilePath(dir), err)
	}

	var validationErrors []string

	if toolCall.Execution.ChildSessionID != "" && child.Identity.ID != toolCall.Execution.ChildSessionID {
		validationErrors = append(validationErrors,
			fmt.Sprintf("child ID mismatch: tool call expects %q, child has %q",
				toolCall.Execution.ChildSessionID, child.Identity.ID))
	}

	if child.Identity.ParentID != parent.Identity.ID {
		validationErrors = append(validationErrors,
			fmt.Sprintf("child ParentID mismatch: expected parent ID %q, child has %q",
				parent.Identity.ID, child.Identity.ParentID))
	}

	if child.Identity.RootID != parent.Identity.RootID {
		validationErrors = append(validationErrors,
			fmt.Sprintf("child RootID mismatch: expected root ID %q, child has %q",
				parent.Identity.RootID, child.Identity.RootID))
	}

	if child.Identity.ParentToolCallID != toolCall.ID {
		validationErrors = append(validationErrors,
			fmt.Sprintf("child ParentToolCallID mismatch: expected tool call ID %q, child has %q",
				toolCall.ID, child.Identity.ParentToolCallID))
	}

	expectedDepth := parent.Identity.Depth + 1
	if child.Identity.Depth != expectedDepth {
		validationErrors = append(validationErrors,
			fmt.Sprintf("child Depth mismatch: expected %d (parent depth %d + 1), child has %d",
				expectedDepth, parent.Identity.Depth, child.Identity.Depth))
	}

	if len(validationErrors) > 0 {
		return SessionDoc{}, "", fmt.Errorf("child session identity validation failed: %s",
			validationErrors[0])
	}

	return child, dir, nil
}

// ListSessions returns available session info (name + chat document modified time), sorted by most recently modified.
func ListSessions(p Paths) []SessionInfo {
	entries, err := os.ReadDir(p.Sessions)
	if err != nil {
		return nil
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := os.Stat(filepath.Join(p.Sessions, e.Name(), "chat.json"))
		if err != nil || info.IsDir() {
			continue
		}
		sessions = append(sessions, SessionInfo{
			Name:    e.Name(),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions
}

// SessionTreeForkResult holds the outcome of a session-tree fork operation.
type SessionTreeForkResult struct {
	// RootIdentity is the new identity assigned to the forked root session.
	RootIdentity SessionIdentity
	// IDMap maps every original session ID to its new forked ID.
	IDMap map[string]string
}

// ForkSessionTree copies the complete recursive session tree rooted at sourceDir
// into destinationDir, rewriting every session identity and tool-call child link
// so the forked tree is fully independent of the source.
//
// The directory layout (agents subdirectories, child names, depth, ParentToolCallID,
// messages, configs) is preserved. Only ID-related fields are remapped:
//
//   - Identity.ID → new unique ID
//   - Identity.ParentID → mapped new ID (empty for the new root)
//   - Identity.RootID → new root ID for all sessions in the fork
//   - ToolCallEntry.Execution.ChildSessionID → mapped new ID
//
// Returns an error if:
//
//   - Source directory or chat.json does not exist.
//   - Destination already contains a session document (collision).
//   - A copied child's identity is inconsistent with its parent (broken lineage).
//   - The forked tree would contain duplicate IDs.
func ForkSessionTree(sourceDir string, destinationDir string) (SessionTreeForkResult, error) {
	// 1. Validate source
	sourceDocPath := SessionFilePath(sourceDir)
	if _, err := os.Stat(sourceDocPath); os.IsNotExist(err) {
		return SessionTreeForkResult{}, fmt.Errorf("source session document not found: %s", sourceDocPath)
	}

	// 2. Validate destination collision
	destDocPath := SessionFilePath(destinationDir)
	if _, err := os.Stat(destDocPath); err == nil {
		return SessionTreeForkResult{}, fmt.Errorf("destination session already exists: %s", destDocPath)
	}

	// 3. Collect all chat.json paths in the source tree, relative to sourceDir
	relPaths, err := collectChatJSONPaths(sourceDir)
	if err != nil {
		return SessionTreeForkResult{}, fmt.Errorf("collect source tree: %w", err)
	}
	if len(relPaths) == 0 {
		return SessionTreeForkResult{}, fmt.Errorf("no session documents found in source directory: %s", sourceDir)
	}

	// 4. Load all source sessions
	type sessionNode struct {
		relPath string
		doc     SessionDoc
	}
	nodes := make([]sessionNode, 0, len(relPaths))
	for _, rel := range relPaths {
		absPath := filepath.Join(sourceDir, rel)
		dir := filepath.Dir(absPath)
		doc, err := LoadSessionDoc(dir)
		if err != nil {
			return SessionTreeForkResult{}, fmt.Errorf("load source session %s: %w", rel, err)
		}
		nodes = append(nodes, sessionNode{relPath: rel, doc: doc})
	}

	// 5. Collect all source IDs first
	sourceIDs := make(map[string]bool, len(nodes))
	idToNodeIdx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		sourceIDs[n.doc.Identity.ID] = true
		idToNodeIdx[n.doc.Identity.ID] = i
	}

	// 6. Validate source lineage: check for duplicate IDs and broken parent refs
	for _, n := range nodes {
		// Validate that ParentID points to an ID in the source tree
		if n.doc.Identity.ParentID != "" && !sourceIDs[n.doc.Identity.ParentID] {
			return SessionTreeForkResult{}, fmt.Errorf("source tree has broken lineage: %s references parent ID %q which is not in the tree",
				n.relPath, n.doc.Identity.ParentID)
		}
	}

	// Check for duplicate IDs (map size should equal slice length)
	if len(sourceIDs) != len(nodes) {
		// Find the duplicate
		seen := make(map[string]bool)
		for _, n := range nodes {
			if seen[n.doc.Identity.ID] {
				return SessionTreeForkResult{}, fmt.Errorf("source tree has duplicate ID %q", n.doc.Identity.ID)
			}
			seen[n.doc.Identity.ID] = true
		}
	}

	// 6. Identify the root document (chat.json at the top level of sourceDir)
	rootID := ""
	for _, n := range nodes {
		if filepath.Dir(n.relPath) == "." {
			rootID = n.doc.Identity.ID
			break
		}
	}
	if rootID == "" {
		return SessionTreeForkResult{}, fmt.Errorf("no root session found at source directory: %s", sourceDir)
	}

	// 7. Generate new IDs and build the map
	idMap := make(map[string]string, len(nodes))
	for _, n := range nodes {
		newID := uuid.New().String()
		idMap[n.doc.Identity.ID] = newID
	}

	// 8. Determine new root ID
	newRootID := idMap[rootID]

	// 9. Rewrite and save each session
	now := time.Now().UTC().Format(time.RFC3339)

	for _, n := range nodes {
		doc := n.doc // copy struct

		// Rewrite identity
		doc.Identity.ID = idMap[doc.Identity.ID]
		if doc.Identity.ParentID != "" {
			doc.Identity.ParentID = idMap[doc.Identity.ParentID]
		}
		doc.Identity.RootID = newRootID

		// Rewrite child session IDs in tool executions
		for mi := range doc.Messages {
			for ti := range doc.Messages[mi].ToolCalls {
				tc := &doc.Messages[mi].ToolCalls[ti]
				if tc.Execution.ChildSessionID != "" {
					if newChildID, ok := idMap[tc.Execution.ChildSessionID]; ok {
						tc.Execution.ChildSessionID = newChildID
					}
				}
			}
		}

		// Update timestamps to reflect the fork
		doc.Meta.CreatedAt = now
		doc.Meta.UpdatedAt = now

		// Compute destination directory
		destSubDir := filepath.Join(destinationDir, filepath.Dir(n.relPath))
		destDocPath := SessionFilePath(destSubDir)

		// Write
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return SessionTreeForkResult{}, fmt.Errorf("marshal forked session %s: %w", n.relPath, err)
		}
		if err := os.MkdirAll(filepath.Dir(destDocPath), 0755); err != nil {
			return SessionTreeForkResult{}, fmt.Errorf("create destination directory %s: %w", filepath.Dir(destDocPath), err)
		}
		if err := os.WriteFile(destDocPath, data, 0644); err != nil {
			return SessionTreeForkResult{}, fmt.Errorf("write forked session %s: %w", destDocPath, err)
		}
	}

	// 10. Validate the forked tree has no duplicate new IDs
	seenNewIDs := make(map[string]bool, len(idMap))
	for _, newID := range idMap {
		if seenNewIDs[newID] {
			return SessionTreeForkResult{}, fmt.Errorf("forked tree contains duplicate ID %q", newID)
		}
		seenNewIDs[newID] = true
	}

	return SessionTreeForkResult{
		RootIdentity: SessionIdentity{
			ID:     newRootID,
			RootID: newRootID,
			Depth:  0,
		},
		IDMap: idMap,
	}, nil
}

// collectChatJSONPaths walks the directory tree rooted at dir and returns
// relative paths to every chat.json file it finds.
func collectChatJSONPaths(dir string) ([]string, error) {
	var results []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "chat.json" {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			results = append(results, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

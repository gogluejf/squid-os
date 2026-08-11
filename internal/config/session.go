package config

import (
	"encoding/json"
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

// SessionDoc is the persisted session document.
// Meta holds identity. Initial holds provenance. Config holds current runtime state.
// Pending holds desired next state (nil = nothing pending).
type SessionDoc struct {
	Version     int                       `json:"version"`
	Meta        SessionMeta               `json:"meta"`
	Initial     SessionConfig             `json:"initial"`
	Config      SessionConfig             `json:"config"`
	Pending     *PendingConfig            `json:"pending,omitempty"`
	Messages    []Message                 `json:"messages"`
	TotalTokens int                       `json:"total_tokens,omitempty"` // legacy, kept for backward compat on load
	TokenTally  *TokenTally               `json:"token_tally,omitempty"`
	FileState   map[string]FileStateEntry `json:"file_state,omitempty"`
}

// SessionMeta holds identity-only fields that never change.
type SessionMeta struct {
	ID        string `json:"id"`
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
func NewSessionDoc(cfg SessionConfig) SessionDoc {
	now := time.Now().UTC().Format(time.RFC3339)
	return SessionDoc{
		Version: 1,
		Meta: SessionMeta{
			ID:        uuid.New().String(),
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

// SessionPath returns the chat document path for a session by name.
func SessionPath(p Paths, name string) string {
	return filepath.Join(p.Sessions, name, "chat.json")
}

// SaveSessionDoc writes a session to sessions/<name>/chat.json.
// It persists TokenTally and clears the legacy scalar total_tokens.
func SaveSessionDoc(p Paths, name string, doc SessionDoc, tally *TokenTally) error {
	doc.Meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	doc.TokenTally = tally
	doc.TotalTokens = 0 // clear legacy scalar on save
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	path := SessionPath(p, name)
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

// LoadSessionDoc reads a session from sessions/<name>/chat.json.
func LoadSessionDoc(p Paths, name string) (SessionDoc, error) {
	file := SessionPath(p, name)
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

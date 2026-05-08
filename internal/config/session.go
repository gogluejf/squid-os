package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type SessionFile struct {
	Version     int       `json:"version"`
	Session     Session   `json:"session"`
	Messages    []Message `json:"messages"`
	TotalTokens int       `json:"total_tokens"`
}

type Session struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	Thinking         bool   `json:"thinking"`
	SystemPromptFile string `json:"system_prompt_file"`
}

type ContentMetrics struct {
	Tokens               int   `json:"tokens,omitempty"`
	InferenceDuractionMs int64 `json:"inference_duration_ms,omitempty"`
	TimeToFirstTokenMs   int64 `json:"time_to_first_token_ms,omitempty"`
}

type SequenceStat struct {
	AvgTokensPerSec      float64 `json:"avg_tok_per_sec,omitempty"`
	OutputTokens         int     `json:"output_tokens,omitempty"`
	DurationMs           int64   `json:"duration_ms,omitempty"`
	InferenceDuractionMs int64   `json:"inference_duration_ms,omitempty"`
	InputTokens          int     `json:"input_tokens,omitempty"`
	ExecDurMs            int64   `json:"exec_dur_ms,omitempty"`
}

// Add sums another SequenceStat into this one and recomputes AvgTokensPerSec.
func (ss *SequenceStat) Add(other *SequenceStat) {
	ss.OutputTokens += other.OutputTokens
	ss.DurationMs += other.DurationMs
	ss.InferenceDuractionMs += other.InferenceDuractionMs
	ss.ExecDurMs += other.ExecDurMs
	ss.InputTokens += other.InputTokens
	if ss.InferenceDuractionMs > 0 {
		ss.AvgTokensPerSec = float64(ss.OutputTokens) / float64(ss.InferenceDuractionMs) * 1000.0
	}
}

func (ss *SequenceStat) Accumulate(msg Message) {
	ss.OutputTokens += msg.Tokens
	ss.DurationMs += msg.DurationTimeMs
	ss.InferenceDuractionMs += msg.TextMetrics.InferenceDuractionMs
	ss.InferenceDuractionMs += msg.ThinkingMetrics.InferenceDuractionMs
	ss.InferenceDuractionMs += msg.ToolCallMetrics.InferenceDuractionMs
	for _, tc := range msg.ToolCalls {
		ss.ExecDurMs += tc.Execution.DurationMs
		ss.InputTokens += tc.Execution.Tokens
	}
	if ss.InferenceDuractionMs > 0 {
		ss.AvgTokensPerSec = float64(ss.OutputTokens) / float64(ss.InferenceDuractionMs) * 1000.0
	}
}

// FindSequenceHeadIdx returns the index of the first assistant message after
// the last user message, skipping any "synthetic" messages in between,
// or -1 if none exists yet.
func FindSequenceHeadIdx(msgs []Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			for j := i + 1; j < len(msgs); j++ {
				if msgs[j].Role == "assistant" {
					return j
				}
			}
			return -1
		}
	}
	return -1
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
		Status     string `json:"status,omitempty"`
		Result     string `json:"result,omitempty"`
		Error      string `json:"error,omitempty"`
		Tokens     int    `json:"tokens,omitempty"`
		DurationMs int64  `json:"duration_ms"`
	} `json:"execution,omitempty"`
}

type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`

	Tokens             int     `json:"tokens,omitempty"`
	DurationTimeMs     int64   `json:"duration_ms,omitempty"`
	TimeToFirstTokenMs int64   `json:"time_to_first_token_ms,omitempty"`
	TokensPerSecond    float64 `json:"tok_per_sec,omitempty"`

	ImagePath  string `json:"image_path,omitempty"`
	UserTokens int    `json:"user_tokens"`

	Text            string         `json:"text"`
	TextMetrics     ContentMetrics `json:"text_metrics,omitempty"`
	ThinkingText    string         `json:"thinking_text,omitempty"`
	ThinkingMetrics ContentMetrics `json:"thinking_metrics,omitempty"`

	ToolCalls       []ToolCallEntry `json:"tool_calls,omitempty"`
	ToolCallMetrics ContentMetrics  `json:"tool_call_metrics,omitempty"`

	SequenceStat *SequenceStat `json:"sequence_stat,omitempty"`

	StopReason string `json:"stop_reason,omitempty"`

	// Label is a human-readable label for synthetic/internal messages (e.g. "Stream aborted by user", "Stream error [details]").
	Label string `json:"label,omitempty"`
}

// NewSessionFile creates a new empty session
func NewSessionFile(provider, model string, thinking bool, systemPrompt string) SessionFile {
	now := time.Now().UTC().Format(time.RFC3339)
	return SessionFile{
		Version: 1,
		Session: Session{
			ID:               uuid.New().String(),
			CreatedAt:        now,
			UpdatedAt:        now,
			Provider:         provider,
			Model:            model,
			Thinking:         thinking,
			SystemPromptFile: systemPrompt,
		},
	}
}

// SessionPath returns the file path for a session by name.
func SessionPath(p Paths, name string) string {
	return filepath.Join(p.Sessions, name+".chat.json")
}

// SaveSession writes a session to sessions/<name>.chat.json
func SaveSession(p Paths, name string, sf SessionFile) error {
	sf.Session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(SessionPath(p, name), data, 0644)
}

// LoadSession reads a session from sessions/<name>.chat.json
func LoadSession(p Paths, name string) (SessionFile, error) {
	file := SessionPath(p, name)
	data, err := os.ReadFile(file)
	if err != nil {
		return SessionFile{}, err
	}
	var sf SessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return SessionFile{}, err
	}
	return sf, nil
}

// SessionInfo holds display metadata for a saved session.
type SessionInfo struct {
	Name    string
	ModTime time.Time
}

// ListSessions returns available session info (name + modified time), sorted by most recently modified.
func ListSessions(p Paths) []SessionInfo {
	entries, err := os.ReadDir(p.Sessions)
	if err != nil {
		return nil
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".chat.json") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			sessions = append(sessions, SessionInfo{
				Name:    strings.TrimSuffix(e.Name(), ".chat.json"),
				ModTime: info.ModTime(),
			})
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions
}

package config

import "time"

// Settings persisted to settings.json
type Settings struct {
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	Thinking            bool   `json:"thinking"`
	SystemPromptFile    string `json:"system_prompt_file"`
	MaxHistory          int    `json:"max_history"`
	LastSessionName     string `json:"last_session_name"`
	AutoSave            bool   `json:"auto_save"`
	AutoLoadLastSession bool   `json:"auto_load_last_session"`
}

// ProviderConfig from endpoints.json
type ProviderConfig struct {
	Name      string `json:"name"`
	ChatURL   string `json:"chat_completions_url"`
	ModelsURL string `json:"models_url"`
}

// EndpointsConfig is the full endpoints.json
type EndpointsConfig struct {
	Providers []ProviderConfig `json:"providers"`
}

// History is prompt recall (LRU)
type History struct {
	Entries []string `json:"entries"`
}

// SessionFile is the full *.chat.json
type SessionFile struct {
	Version     int       `json:"version"`
	Session     Session   `json:"session"`
	Messages    []Message `json:"messages"`
	TotalTokens int       `json:"total_tokens"`
}

// Session metadata
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

// Message in a chat session
type Message struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
	ImagePath  string    `json:"image_path,omitempty"`
	UserTokens int       `json:"user_tokens"`

	TokensPerSecond    float64 `json:"tokens_per_second,omitempty"`
	Tokens             int     `json:"tokens_ms,omitempty"`
	DurationTimeMs     int64   `json:"duration_time_ms,omitempty"`
	TimeToFirstTokenMs int64   `json:"time_to_first_token_ms,omitempty"`

	Text                   string `json:"text"`
	TextTokens             int    `json:"text_tokens"`
	TextDurationMs         int64  `json:"text_duration_ms,omitempty"`
	TextTimeToFirstTokenMs int64  `json:"text_time_to_first_token_ms,omitempty"`

	ThinkingText               string `json:"thinking_text,omitempty"`
	ThinkingTokens             int    `json:"thinking_tokens,omitempty"`
	ThinkingDurationMs         int64  `json:"thinking_duration_ms,omitempty"`
	ThinkingTimeToFirstTokenMs int64  `json:"thinking_time_to_first_token_ms,omitempty"`

	StopReason string `json:"stop_reason,omitempty"`
}

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
	ID              string    `json:"id"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	Text            string    `json:"text"`
	ThinkingText    string    `json:"thinking_text,omitempty"`
	ImagePath       string    `json:"image_path,omitempty"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	TokensPerSecond float64   `json:"tokens_per_second,omitempty"`
	ResponseTimeMs  int64     `json:"response_time_ms,omitempty"`
	StopReason      string    `json:"stop_reason,omitempty"`
}

// DisplayMessage is a message ready for rendering in the TUI
type DisplayMessage struct {
	Message
	ThinkingExpanded bool
}

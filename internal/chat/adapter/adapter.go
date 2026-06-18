package adapter

import (
	"squid-os/internal/config"
	"squid-os/internal/tools"
)

// APIAdapter translates between our internal message/tool format and the
// provider-specific request body and SSE event stream.
type APIAdapter interface {
	// BuildBody creates the JSON request body from messages and tools.
	// messages is serialized JSON of the message array.
	BuildBody(model string, messagesJSON []byte, toolDefs []tools.Tool, thinking bool) ([]byte, error)
	// ParseSSE parses one SSE JSON payload. Returns nil to skip.
	ParseSSE(payload string) *AdapterEvent
	// GetChatURL returns the inference endpoint for these settings.
	GetChatURL(settings *config.ProviderSettings) string
	// GetModelsURL returns the models listing endpoint, or "" if none.
	GetModelsURL(settings *config.ProviderSettings) string
}

// AdapterEvent is the adapter's parsed SSE event.
// The engine translates this into StreamEvent.
type AdapterEvent struct {
	Text          string
	Thinking      string
	Done          bool
	StopReason    string
	ToolCallDelta string
	ToolCallIdx   int
	ToolCallName  string
	ToolCallID    string
	ToolCallType  string
}

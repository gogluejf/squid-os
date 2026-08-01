package run

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"squid-os/internal/chat"
)

type OutputMode string

const (
	OutputFinalMessage OutputMode = "final_message"
	OutputStream       OutputMode = "stream"
	OutputSilent       OutputMode = "silent"
	OutputStructured   OutputMode = "structured"
)

type StreamEnvelope struct {
	Event       string `json:"event"`
	Timestamp   string `json:"ts"`
	Role        string `json:"role,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	Tool        string `json:"tool,omitempty"`
	Text        string `json:"text,omitempty"`
	Saved       *bool  `json:"saved,omitempty"`
	SessionName string `json:"session,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	Status      string `json:"status,omitempty"`
}

type StreamWriter struct{ encoder *json.Encoder }

func NewStreamWriter(writer io.Writer) *StreamWriter {
	return &StreamWriter{encoder: json.NewEncoder(writer)}
}
func (w *StreamWriter) Write(envelope StreamEnvelope) error {
	if envelope.Timestamp == "" {
		envelope.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return w.encoder.Encode(envelope)
}
func (w *StreamWriter) WriteLoopEvent(event chat.LoopEvent) error {
	envelope := StreamEnvelope{}
	switch event.Type {
	case chat.LoopEventText:
		envelope.Event, envelope.Role, envelope.Text = "message_delta", "assistant", event.Text
	case chat.LoopEventThinking:
		envelope.Event, envelope.Role, envelope.Text = "thinking_delta", "assistant", event.Thinking
	case chat.LoopEventToolDelta:
		envelope.Event, envelope.Role, envelope.Text = "tool_call_update", "assistant", event.Text
	case chat.LoopEventToolFlushed:
		envelope.Event, envelope.Role = "tool_execution_end", "tool"
	case chat.LoopEventNeedAuth:
		envelope.Event, envelope.Role, envelope.Tool = "permission_request", "tool", event.AuthRequest.ToolName
	case chat.LoopEventError:
		envelope.Event = "error"
		if event.Error != nil {
			envelope.Text = event.Error.Error()
		}
	case chat.LoopEventDone:
		envelope.Event = "finished"
		envelope.StopReason = "end_turn"
	default:
		return nil
	}
	return w.Write(envelope)
}

func WriteResult(mode OutputMode, result Result, stdout, stderr io.Writer) error {
	switch mode {
	case OutputFinalMessage:
		if result.FinalText != "" {
			fmt.Fprintln(stdout, result.FinalText)
		}
	case OutputSilent:
	case OutputStructured:
		return fmt.Errorf("structured output mode is not implemented")
	case OutputStream:
		return nil
	default:
		return fmt.Errorf("unknown output mode %q", mode)
	}
	if result.SavedSessionName != "" {
		fmt.Fprintln(stderr, result.SavedSessionName)
	}
	return nil
}

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/tools"
	"strings"

	jsonrepair "github.com/kaptinlin/jsonrepair"
	goai "github.com/zendev-sh/goai"
	goai_provider "github.com/zendev-sh/goai/provider"
)

// StreamEvent is sent for each chunk during inference.
type StreamEvent struct {
	Text          string     // visible delta text
	Thinking      string     // thinking delta text
	InThinking    bool       // currently inside think block
	Done          bool       // stream finished
	StopReason    string     // from the final chunk
	Error         error      // non-nil on error
	IsAuthError   bool       // true when Error is an authentication failure (401/expired)
	ToolCalls     []ToolCall // non-nil when model requests tool calls (flush at end)
	ToolCallDelta string     // incremental arg fragment for timing/token tracking
	ToolCallIdx   int        // tool call index this delta belongs to
	ToolCallName  string     // accumulated name so far for this tool call
	ToolCallID    string     // tool call ID when available
}

// ToolCall represents a single tool call from the model.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		Args string `json:"arguments"`
	} `json:"function,omitempty"`
	Name     string // for internal use, not serialized to JSON
	ArgsJSON string // raw JSON string of arguments, for internal use
}

// toolAccum tracks buffered tool call deltas.
type toolAccum struct {
	nameBuf strings.Builder
	argsBuf strings.Builder
	id      string
}

// ChatMessage is a message for the API request (will be converted to GoAI provider.Message).
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

// toolDefinition is the OpenAI-compatible tool definition sent in the request.
type toolDefinition struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

// functionDef controls key ordering: name, description, parameters.
type functionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Engine manages chat inference against a provider endpoint.
type Engine struct {
	settings *config.ProviderSettings
	Model    string
	Thinking config.ThinkingConfig
	provider provider.Provider
}

// isAuthFailure returns true if the error message indicates an authentication failure.
func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "invalidapi") ||
		strings.Contains(msg, "invalid_api") ||
		strings.Contains(msg, "invalid token") ||
		strings.Contains(msg, "token expired") ||
		strings.Contains(msg, "no credentials")
}

func NewEngine(settings *config.ProviderSettings, model string, thinking config.ThinkingConfig) *Engine {
	if settings == nil {
		return &Engine{settings: nil}
	}

	p := provider.Lookup(settings.Name, settings)

	return &Engine{
		settings: settings,
		Model:    model,
		Thinking: thinking,
		provider: p,
	}
}

// toolsToDefinitions converts our Tool structs to API tool definitions.
func toolsToDefinitions(ts []tools.Tool) []toolDefinition {
	defs := make([]toolDefinition, len(ts))
	for i, t := range ts {
		defs[i] = toolDefinition{
			Type: "function",
			Function: functionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		}
	}
	return defs
}

// MarshalToolsJSON returns the raw JSON of tool definitions as sent in the API request body.
// Used by the app for token counting on internal messages.
func MarshalToolsJSON(ts []tools.Tool) ([]byte, error) {
	return json.Marshal(toolsToDefinitions(ts))
}

// Stream sends the chat request and returns a channel of StreamEvents.
// Cancel via the context. The channel is closed when done.
// Pass nil for toolDefs if no tools are available.
func (e *Engine) Stream(ctx context.Context, messages []goai_provider.Message, toolDefs []tools.Tool) <-chan StreamEvent {
	ch := make(chan StreamEvent, 64)

	if e.settings == nil {
		ch <- StreamEvent{Error: fmt.Errorf("no provider defined — please select one with /model")}
		close(ch)
		return ch
	}

	if e.provider == nil {
		ch <- StreamEvent{Error: fmt.Errorf("unknown provider: %s", e.settings.Name)}
		close(ch)
		return ch
	}

	go func() {
		defer close(ch)
		e.runStream(ctx, ch, messages, toolDefs, false)
	}()

	return ch
}

// runStream performs one streaming attempt, forwarding events to ch. If a
// media rejection occurs and this is the first attempt, it strips media parts
// and retries once (retrying=true prevents recursion).
func (e *Engine) runStream(ctx context.Context, ch chan<- StreamEvent, messages []goai_provider.Message, toolDefs []tools.Tool, retrying bool) {

		// Build GoAI LanguageModel from provider
		langModel, parseThinking, err := e.provider.BuildGoAIModel(e.Model)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("build model: %w", err), IsAuthError: isAuthFailure(err)}
			return
		}

		// Convert our tools to GoAI tools (no Execute — we handle execution ourselves)
		var goaiTools []goai.Tool
		for _, t := range toolDefs {
			goaiTools = append(goaiTools, goai.Tool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Schema,
			})
		}

		// Call StreamText with MaxSteps(1) so our app controls the tool loop
		providerOptions := e.provider.RequestProviderOptions(e.Model, e.Thinking.Enabled)
		streamOpts := []goai.Option{
			goai.WithMessages(messages...),
			goai.WithTools(goaiTools...),
			goai.WithMaxSteps(1),
		}
		if providerOptions != nil {
			streamOpts = append(streamOpts, goai.WithProviderOptions(providerOptions))
		}

		stream, err := goai.StreamText(ctx, langModel, streamOpts...)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("stream init: %w", err), IsAuthError: isAuthFailure(err)}
			return
		}

		// Process chunks from the raw stream.
		// Inline reasoning parsing is only enabled when:
		// 1. thinking is requested,
		// 2. user enables parse_reasoning_from_text,
		// 3. provider/backend indicates it may return reasoning inline in text.
		useTextReasoningParser := e.Thinking.Enabled && parseThinking && e.settings != nil && e.Thinking.ParseReasoningFromText
		parser := &ThinkParser{}
		if useTextReasoningParser {
			// Qwen-style providers may emit hidden reasoning text before an
			// explicit <think> tag arrives, so preserve the legacy bootstrap.
			parser.InThink = true
		}

		// Accumulate partial tool deltas for streaming UI timing.
		toolBuffers := make(map[int]*toolAccum)
		nextIdx := 0
		var stopReason string
		// Route: either raw chunks (for tool deltas) or just result
		chunkStream := stream.Stream()

		for chunk := range chunkStream {
			if ctx.Err() != nil {
				ch <- StreamEvent{Done: true}
				return
			}

			logChunk := fmt.Sprintf("type=%s text=%q tool_name=%q tool_call_id=%q tool_input=%q finish_reason=%q err=%v metadata=%v", chunk.Type, chunk.Text, chunk.ToolName, chunk.ToolCallID, chunk.ToolInput, chunk.FinishReason, chunk.Error, chunk.Metadata)
			log.LogSSEChunk(logChunk)

			switch chunk.Type {
			case goai_provider.ChunkText:
				if useTextReasoningParser {
					// Provider emits reasoning inline in text content — split it locally.
					result := parser.Process(chunk.Text)
					if result.Text != "" || result.Thinking != "" {
						ch <- StreamEvent{
							Text:       result.Text,
							Thinking:   result.Thinking,
							InThinking: parser.InThink,
						}
					}
				} else {
					// Native provider text chunk — not in think-tag parsing mode.
					ch <- StreamEvent{Text: chunk.Text, InThinking: false}
				}

			case goai_provider.ChunkReasoning:
				// Native reasoning support from provider/GoAI.
				ch <- StreamEvent{Thinking: chunk.Text, InThinking: true}

			case goai_provider.ChunkToolCallStreamStart:
				idx := nextIdx
				if ci, ok := chunk.Metadata["index"].(int); ok {
					idx = ci
					if idx >= nextIdx {
						nextIdx = idx + 1
					}
				} else {
					nextIdx = idx + 1
				}
				toolBuffers[idx] = &toolAccum{}

			case goai_provider.ChunkToolCallDelta:
				idx := -1
				if ci, ok := chunk.Metadata["index"].(int); ok {
					idx = ci
				}
				if idx < 0 && len(chunk.ToolInput) > 0 {
					idx = nextIdx - 1
				}
				if buf, ok := toolBuffers[idx]; ok {
					if chunk.ToolName != "" && buf.nameBuf.Len() == 0 {
						buf.nameBuf.WriteString(chunk.ToolName)
					}
					if chunk.ToolCallID != "" && buf.id == "" {
						buf.id = chunk.ToolCallID
					}
					if chunk.ToolInput != "" {
						buf.argsBuf.WriteString(chunk.ToolInput)
					}
					ch <- StreamEvent{
						ToolCallDelta: chunk.ToolInput,
						ToolCallIdx:   idx,
						ToolCallName:  buf.nameBuf.String(),
						ToolCallID:    buf.id,
					}
				}

			case goai_provider.ChunkToolCall:
				tc := goaiToInternalToolCall(chunk)
				idx := nextIdx - 1
				if ci, ok := chunk.Metadata["index"].(int); ok {
					idx = ci
				}
				if idx < 0 {
					idx = 0
				}
				if _, ok := toolBuffers[idx]; !ok {
					toolBuffers[idx] = &toolAccum{}
				}
				toolBuffers[idx].id = tc.ID
				toolBuffers[idx].nameBuf.Reset()
				toolBuffers[idx].argsBuf.Reset()
				toolBuffers[idx].nameBuf.WriteString(tc.Name)
				toolBuffers[idx].argsBuf.WriteString(tc.ArgsJSON)
				ch <- StreamEvent{ToolCalls: []ToolCall{tc}, ToolCallIdx: idx}

			case goai_provider.ChunkStepFinish:
				stopReason = string(chunk.FinishReason)
				if stopReason == "" && len(toolBuffers) > 0 {
					stopReason = "tool_calls"
				}

			case goai_provider.ChunkFinish:
				stopReason = string(chunk.FinishReason)
				finalFlush := parser.Flush()
				if stopReason == "" && len(toolBuffers) > 0 {
					stopReason = "tool_calls"
				}
				ch <- StreamEvent{
					Text:       finalFlush.Text,
					Thinking:   finalFlush.Thinking,
					InThinking: parser.InThink,
					Done:       true,
					StopReason: stopReason,
				}
				return

			case goai_provider.ChunkError:
				// If this is the first attempt and the error is a classified
				// media rejection, retry once without media parts.
				if !retrying && isMediaRejection(chunk.Error) {
					// Strip media parts from messages and retry.
					stripped := stripMediaParts(messages)
					ch <- StreamEvent{
						Text: "Retrying without attachments: model does not support this media type",
					}
					// Recurse with stripped messages and retrying=true.
					e.runStream(ctx, ch, stripped, toolDefs, true)
					return
				}
				ch <- StreamEvent{Error: chunk.Error, IsAuthError: isAuthFailure(chunk.Error)}
				return
			}
		}

		// Stream channel closed without ChunkFinish — emit one terminal text/thinking flush event.
		result := parser.Flush()
		if len(toolBuffers) > 0 && stopReason == "" {
			stopReason = "tool_calls"
		}
		ch <- StreamEvent{
			Text:       result.Text,
			Thinking:   result.Thinking,
			InThinking: parser.InThink,
			Done:       true,
			StopReason: stopReason,
		}
	}

// BuildAPIMessages converts config.Message history to GoAI provider.Message.
// Attachment references in user messages are not resolved — pass a workspace
// and attachments via BuildContext to include them.
func BuildAPIMessages(messages []config.Message) []goai_provider.Message {
	return buildProviderMessages(messages, nil, nil, "", nil)
}

// goaiToInternalToolCall converts a GoAI StreamChunk tool call to our internal format.
func goaiToInternalToolCall(chunk goai_provider.StreamChunk) ToolCall {
	return ToolCall{
		ID:   chunk.ToolCallID,
		Type: "function",
		Name: chunk.ToolName,
		Function: struct {
			Name string `json:"name"`
			Args string `json:"arguments"`
		}{Name: chunk.ToolName, Args: chunk.ToolInput},
		ArgsJSON: chunk.ToolInput,
	}
}

// RepairArgs attempts to fix malformed JSON from the model's streamed arguments.
// Returns the repaired string and whether it is now valid JSON.
func RepairArgs(args string) (string, bool) {
	if args == "" {
		return args, false
	}
	var check map[string]interface{}
	if json.Unmarshal([]byte(args), &check) == nil {
		return args, true
	}
	repaired, err := jsonrepair.Repair(args)
	if err == nil && repaired != "" {
		if json.Unmarshal([]byte(repaired), &check) == nil {
			return repaired, true
		}
	}
	t := strings.TrimSpace(args)
	if len(t) > 0 && t[0] == '{' && t[len(t)-1] != '}' {
		closed := args + "}"
		if json.Unmarshal([]byte(closed), &check) == nil {
			return closed, true
		}
	}
	return `{"_error": "malformed JSON from model, original args discarded"}`, false
}

// stripMediaParts removes non-text parts (images, files) from messages,
// preserving text, reasoning, tool-call, and tool-result parts.
func stripMediaParts(messages []goai_provider.Message) []goai_provider.Message {
	stripped := make([]goai_provider.Message, len(messages))
	for i, msg := range messages {
		stripped[i] = goai_provider.Message{
			Role:            msg.Role,
			ProviderOptions: msg.ProviderOptions,
		}
		for _, part := range msg.Content {
			switch part.Type {
			case goai_provider.PartImage, goai_provider.PartFile:
				// Skip media parts
				continue
			default:
				stripped[i].Content = append(stripped[i].Content, part)
			}
		}
		// If all parts were stripped, leave a text part with empty content
		// to keep the message valid.
		if len(stripped[i].Content) == 0 {
			stripped[i].Content = []goai_provider.Part{{Type: goai_provider.PartText, Text: ""}}
		}
	}
	return stripped
}

// collectOmitted examines messages to find attachments that were omitted
// due to known-unsupported modalities. This is used for user notifications.

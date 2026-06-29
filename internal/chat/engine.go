package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/tools"
	"strings"
	"time"

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

// ChatMessage is a message for the API request (will be converted to GoAI provider.Message).
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

// ContentPart for multimodal messages
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL for image content parts
type ImageURL struct {
	URL string `json:"url"`
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
	Thinking bool
	provider provider.Provider
}

func NewEngine(settings *config.ProviderSettings, model string, thinking bool) *Engine {
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

		// Build GoAI LanguageModel from provider
		langModel, parseThinking, err := e.provider.BuildGoAIModel(e.Model)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("build model: %w", err)}
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
		providerOptions := e.provider.RequestProviderOptions(e.Model, e.Thinking)
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
			ch <- StreamEvent{Error: fmt.Errorf("stream init: %w", err)}
			return
		}

		// Process chunks from the raw stream.
		// Providers flagged by BuildGoAIModel(...)=parseThinkingFromText use the
		// local ThinkParser because their reasoning is embedded inline in text
		// content (e.g. Qwen via compat/openai-like transport). Providers that
		// emit native ChunkReasoning are passed through directly.
		parser := &ThinkParser{}
		if e.Thinking && parseThinking {
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
				if parseThinking {
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
				ch <- StreamEvent{ToolCalls: []ToolCall{tc}}

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
				ch <- StreamEvent{Error: chunk.Error}
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
	}()

	return ch
}

// BuildGoAIMessages converts config.Message history to GoAI provider.Message.
func BuildGoAIMessages(paths config.Paths, settings config.Settings, messages []config.Message) []goai_provider.Message {
	var out []goai_provider.Message

	// Collect all system messages and concatenate with \n\n, preserving old behavior.
	var sysParts []string
	for _, msg := range messages {
		if msg.Role == config.RoleSystem {
			sysParts = append(sysParts, msg.Text)
		}
	}
	if len(sysParts) > 0 {
		out = append(out, goai_provider.Message{
			Role: goai_provider.RoleSystem,
			Content: []goai_provider.Part{
				{Type: goai_provider.PartText, Text: strings.Join(sysParts, "\n\n")},
			},
		})
	}

	for _, msg := range messages {
		switch msg.Role {
		case config.RoleSystem, config.RoleInternal:
			continue
		case config.RoleUser:
			if msg.ImagePath != "" {
				parts, err := BuildMultimodalContent(msg.Text, msg.ImagePath)
				if err == nil {
					var goaiParts []goai_provider.Part
					for _, p := range parts {
						switch p.Type {
						case "text":
							goaiParts = append(goaiParts, goai_provider.Part{Type: goai_provider.PartText, Text: p.Text})
						case "image_url":
							if p.ImageURL != nil {
								goaiParts = append(goaiParts, goai_provider.Part{Type: goai_provider.PartImage, URL: p.ImageURL.URL})
							}
						}
					}
					out = append(out, goai_provider.Message{Role: goai_provider.RoleUser, Content: goaiParts})
					continue
				}
			}
			out = append(out, goai_provider.Message{
				Role:    goai_provider.RoleUser,
				Content: []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}},
			})
		case config.RoleAssistant:
			var parts []goai_provider.Part
			if msg.ThinkingText != "" {
				parts = append(parts, goai_provider.Part{Type: goai_provider.PartReasoning, Text: msg.ThinkingText})
			}
			if msg.Text != "" {
				parts = append(parts, goai_provider.Part{Type: goai_provider.PartText, Text: msg.Text})
			}
			for _, tc := range msg.ToolCalls {
				args := tc.Instruction.Arguments
				args, _ = RepairArgs(args)
				parts = append(parts, goai_provider.Part{
					Type:       goai_provider.PartToolCall,
					ToolCallID: tc.ID,
					ToolName:   tc.Instruction.Name,
					ToolInput:  json.RawMessage(args),
				})
			}
			if len(parts) > 0 {
				out = append(out, goai_provider.Message{Role: goai_provider.RoleAssistant, Content: parts})
			}
			for _, tc := range msg.ToolCalls {
				if tc.Execution.Status == "" {
					continue
				}
				content := tc.Execution.Result
				if tc.Execution.Status == tools.ResultStatusError && tc.Execution.Error != "" {
					content = tc.Execution.Error
				}
				out = append(out, goai_provider.Message{
					Role: goai_provider.RoleTool,
					Content: []goai_provider.Part{{
						Type:       goai_provider.PartToolResult,
						ToolCallID: tc.ID,
						ToolName:   tc.Instruction.Name,
						ToolOutput: content,
					}},
				})
			}
		case config.RoleSynthetic:
			out = append(out, goai_provider.Message{
				Role:    goai_provider.RoleAssistant,
				Content: []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}},
			})
		}
	}

	_ = paths
	_ = settings
	return out
}

// mergeResponseMessages merges GoAI response messages back into config.Message history
// while preserving the metadata already tracked in config.Message.
func mergeResponseMessages(result *goai.TextResult, messages []config.Message) []config.Message {
	if result == nil || len(result.ResponseMessages) == 0 {
		return messages
	}

	merged := append([]config.Message(nil), messages...)
	for _, rm := range result.ResponseMessages {
		msg := config.Message{
			ID:        fmt.Sprintf("msg_%d", len(merged)+1),
			CreatedAt: time.Now(),
		}
		switch rm.Role {
		case goai_provider.RoleAssistant:
			msg.Role = config.RoleAssistant
		case goai_provider.RoleTool:
			msg.Role = config.RoleSynthetic
		default:
			continue
		}
		for _, p := range rm.Content {
			switch p.Type {
			case goai_provider.PartText:
				msg.Text += p.Text
			case goai_provider.PartReasoning:
				msg.ThinkingText += p.Text
			case goai_provider.PartToolCall:
				entry := config.ToolCallEntry{ID: p.ToolCallID, Type: "function"}
				entry.Instruction.Name = p.ToolName
				entry.Instruction.Arguments = string(p.ToolInput)
				msg.ToolCalls = append(msg.ToolCalls, entry)
			case goai_provider.PartToolResult:
				if msg.Text == "" {
					msg.Text = p.ToolOutput
				}
			}
		}
		if msg.Text != "" || msg.ThinkingText != "" || len(msg.ToolCalls) > 0 {
			merged = append(merged, msg)
		}
	}
	return merged
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

// toolAccum tracks buffered tool call deltas.
type toolAccum struct {
	nameBuf strings.Builder
	argsBuf strings.Builder
	id      string
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

// FetchModels queries /v1/models endpoint and returns model IDs.
func FetchModels(ctx context.Context, modelsURL string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// BuildAPIMessages converts Message to ChatMessages for the API.
// This function centralizes message building logic used by both headless and TUI modes.
//
// System prompt: all RoleSystem messages are concatenated with \n\n into a single system message.
//
// TODO: Tools are loaded from current engine config (tools.GetTools()), not from
// session messages. This means if tools change between sessions, the saved
// tools_definition internal message will not match the actual tools sent in
// the API request. Fix later.
func BuildAPIMessages(paths config.Paths, settings config.Settings, messages []config.Message) []ChatMessage {
	var msgs []ChatMessage

	// Collect all system messages and concatenate with \n\n
	var sysParts []string
	for _, msg := range messages {
		if msg.Role == config.RoleSystem {
			sysParts = append(sysParts, msg.Text)
		}
	}
	if len(sysParts) > 0 {
		msgs = append(msgs, ChatMessage{Role: "system", Content: strings.Join(sysParts, "\n\n")})
	}

	// Convert display messages to API messages
	for _, msg := range messages {
		// Skip system and internal messages — system is handled above, internal is metadata only
		switch msg.Role {
		case config.RoleSystem, config.RoleInternal:
			continue
		}

		switch msg.Role {
		case config.RoleUser:
			if msg.ImagePath != "" {
				parts, err := BuildMultimodalContent(msg.Text, msg.ImagePath)
				if err == nil {
					msgs = append(msgs, ChatMessage{Role: "user", Content: parts})
				} else {
					msgs = append(msgs, ChatMessage{Role: "user", Content: msg.Text})
				}
			} else {
				msgs = append(msgs, ChatMessage{Role: "user", Content: msg.Text})
			}
		case config.RoleAssistant:
			cm := ChatMessage{Role: "assistant", Content: msg.Text}
			if len(msg.ToolCalls) > 0 {
				cm.ToolCalls = make([]ToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					args := tc.Instruction.Arguments
					// Repair malformed arguments stored from previous turns
					args, _ = RepairArgs(args)
					cm.ToolCalls[i] = ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: struct {
							Name string `json:"name"`
							Args string `json:"arguments"`
						}{Name: tc.Instruction.Name, Args: args},
						Name:     tc.Instruction.Name,
						ArgsJSON: args,
					}
				}
				if msg.Text == "" {
					cm.Content = ""
				}
			}
			msgs = append(msgs, cm)
			// Generate tool role messages for any executed tool calls
			for _, tc := range msg.ToolCalls {
				if tc.Execution.Status == "" {
					continue
				}
				content := tc.Execution.Result
				if tc.Execution.Status == "error" && tc.Execution.Error != "" {
					content = tc.Execution.Error
				}
				msgs = append(msgs, ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    content,
					Name:       tc.Instruction.Name,
				})
			}
		case config.RoleSynthetic:
			// Internal messages (e.g. stream aborted) become a synthetic assistant
			// message for the API so the model knows the previous turn was interrupted.
			msgs = append(msgs, ChatMessage{Role: "assistant", Content: msg.Text})
		}
	}

	return msgs
}

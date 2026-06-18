package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"squid-os/internal/chat/adapter"
	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/tools"
	"strings"
	"time"

	jsonrepair "github.com/kaptinlin/jsonrepair"
)

// StreamEvent is sent for each SSE chunk during inference
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
	ToolCallID    string     // tool call ID from adapter
	ToolCallType  string     // tool call type from adapter
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

// ChatMessage is an OpenAI-compatible message for the API request
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

// toolDefinition is the OpenAI-compatible tool definition sent in the request
type toolDefinition struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

// functionDef controls key ordering: name, description, parameters
type functionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// chatRequest is the OpenAI-compatible request body
type chatRequest struct {
	Model              string                 `json:"model"`
	Stream             bool                   `json:"stream"`
	Messages           []ChatMessage          `json:"messages"`
	Tools              []toolDefinition       `json:"tools,omitempty"`       // available tools
	ToolChoice         interface{}            `json:"tool_choice,omitempty"` // "auto" | "none" | "required" | {"type":"function","function":{"name":"..."}}
	ChatTemplateKwargs map[string]interface{} `json:"chat_template_kwargs,omitempty"`
}

// sseChoice is the delta within a streaming response chunk
type sseChoice struct {
	Delta struct {
		Content   string          `json:"content"`
		ToolCalls []toolCallDelta `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// toolCallDelta represents a single tool call in a delta chunk
type toolCallDelta struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Index    int                    `json:"index"`
	Function map[string]interface{} `json:"function,omitempty"`
}

// sseResponse is a single SSE data payload
type sseResponse struct {
	Choices []sseChoice `json:"choices"`
}

// Engine manages chat inference against a provider endpoint
type Engine struct {
	settings *config.ProviderSettings
	ChatURL  string
	Model    string
	Thinking bool
	provider ProviderImpl
	adapter  adapter.APIAdapter
	client   *http.Client
}

func NewEngine(settings *config.ProviderSettings, model string, thinking bool) *Engine {
	if settings == nil {
		return &Engine{settings: nil}
	}

	p := provider.Lookup(settings.Name, settings.Credentials)
	var a adapter.APIAdapter
	switch p.Dialect() {
	case config.DialectOpenAICodex:
		a = &adapter.CodexAdapter{}
	default:
		a = &adapter.ChatCompletionsAdapter{}
	}

	chatURL := p.GetChatURL(settings)

	return &Engine{
		settings: settings,
		ChatURL:  chatURL,
		Model:    model,
		Thinking: thinking,
		provider: p,
		adapter:  a,
		client: &http.Client{
			Timeout: 0,
		},
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
func (e *Engine) Stream(ctx context.Context, messages []ChatMessage, toolDefs []tools.Tool) <-chan StreamEvent {
	ch := make(chan StreamEvent, 64)

	if e.settings == nil {
		ch <- StreamEvent{Error: fmt.Errorf("no provider defined — please select one with /model")}
		close(ch)
		return ch
	}

	go func() {
		defer close(ch)

		// Serialize messages to JSON for the adapter
		messagesJSON, err := json.Marshal(messages)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("marshal messages: %w", err)}
			return
		}

		// Build request body using adapter
		body, err := e.adapter.BuildBody(e.Model, messagesJSON, toolDefs, e.Thinking)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("marshal request: %w", err)}
			return
		}

		var prettyBody bytes.Buffer
		json.Indent(&prettyBody, body, "", "  ")
		f, _ := os.Create("/tmp/squid-os-debug.json")
		defer f.Close()
		f.Write(prettyBody.Bytes())

		req, err := http.NewRequestWithContext(ctx, "POST", e.ChatURL, bytes.NewReader(body))
		if err != nil {
			// Return: Failed to create HTTP request
			ch <- StreamEvent{Error: fmt.Errorf("create request: %w", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")

		if e.provider != nil {
			if err := e.provider.PrepareRequest(req); err != nil {
				ch <- StreamEvent{Error: fmt.Errorf("provider auth: %w", err)}
				return
			}
		}

		resp, err := e.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				// Return: Context cancelled (user pressed cancel)
				ch <- StreamEvent{Done: true}
				return
			}
			// Return: Network/API error (connection failed, timeout, etc.)
			ch <- StreamEvent{Error: fmt.Errorf("request failed: %w", err)}
			return
		}

		// Handle non-OK response with 401 retry logic
		if resp.StatusCode != http.StatusOK {
			// Handle 401 — try token refresh and retry once
			if resp.StatusCode == http.StatusUnauthorized && e.provider != nil {
				refreshErr := e.provider.Refresh()
				if refreshErr == nil {
					// Refresh succeeded — retry with new token
					retryBodyFn := req.GetBody
					if retryBodyFn != nil {
						bodyReader, brErr := retryBodyFn()
						if brErr == nil {
							req2, rqErr := http.NewRequestWithContext(ctx, "POST", e.ChatURL, bodyReader)
							if rqErr == nil {
								req2.Header.Set("Content-Type", "application/json")
								authErr := e.provider.PrepareRequest(req2)
								if authErr == nil {
									resp.Body.Close()
									resp, err = e.client.Do(req2)
									if err != nil {
										ch <- StreamEvent{Error: fmt.Errorf("retry request failed: %w", err)}
										return
									}
								}
							}
						}
					}
				}
				// If we didn't get a new OK response, return re-auth error
				if resp == nil || resp.StatusCode != http.StatusOK {
					ch <- StreamEvent{Error: fmt.Errorf("provider authentication failed — reconfigure provider settings (use command /model)")}
					return
				}
			} else {
				// Non-401 error — parse and return
				var errorResp struct {
					Error struct {
						Message string      `json:"message"`
						Type    string      `json:"type"`
						Code    interface{} `json:"code"`
					} `json:"error"`
				}
				if jsonErr := json.NewDecoder(resp.Body).Decode(&errorResp); jsonErr == nil && errorResp.Error.Message != "" {
					ch <- StreamEvent{Error: fmt.Errorf("API error [%d]: %s", resp.StatusCode, errorResp.Error.Message)}
				} else {
					ch <- StreamEvent{Error: fmt.Errorf("API returned %d", resp.StatusCode)}
				}
				return
			}
		}

		// Stream processing
		defer resp.Body.Close()
		parser := &ThinkParser{}
		// Qwen quirk: when thinking enabled but hidden, model may emit
		// reasoning before any <think> open tag
		if e.Thinking {
			parser.InThink = true
		}

		// Buffer for accumulating tool call deltas by index
		toolBuffers := make(map[int]*toolCallBuffer)

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer for large chunks
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			if ctx.Err() != nil {
				// Return: Context cancelled during stream (user pressed cancel)
				ch <- StreamEvent{Done: true}
				return
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimPrefix(line, "data: ")
			payload = strings.TrimPrefix(payload, "data:")
			payload = strings.TrimSpace(payload)

			log.LogSSEChunk(line)

			if payload == "[DONE]" {
				// Return: Server sent explicit [DONE] marker
				// Flush any remaining buffered content
				result := parser.Flush()
				if result.Text != "" || result.Thinking != "" {
					ch <- StreamEvent{
						Text:       result.Text,
						Thinking:   result.Thinking,
						InThinking: parser.InThink,
					}
				}
				// Flush any remaining tool calls
				if len(toolBuffers) > 0 {
					tc := flushToolCalls(toolBuffers)
					ch <- StreamEvent{ToolCalls: tc}
				}
				ch <- StreamEvent{Done: true}
				return
			}

			// Use adapter to parse SSE event
			evt := e.adapter.ParseSSE(payload)
			if evt == nil {
				continue
			}

			// Handle done event
			if evt.Done {
				result := parser.Flush()
				if result.Text != "" || result.Thinking != "" {
					ch <- StreamEvent{
						Text:       result.Text,
						Thinking:   result.Thinking,
						InThinking: parser.InThink,
					}
				}
				if evt.StopReason == "tool_calls" && len(toolBuffers) > 0 {
					tc := flushToolCalls(toolBuffers)
					ch <- StreamEvent{ToolCalls: tc}
				}
				ch <- StreamEvent{Done: true, StopReason: evt.StopReason}
				return
			}

			// Handle tool call deltas from adapter
			if evt.ToolCallDelta != "" || evt.ToolCallName != "" {
				// Flush parser carry before tool call deltas
				result := parser.Flush()
				if result.Text != "" || result.Thinking != "" {
					ch <- StreamEvent{
						Text:       result.Text,
						Thinking:   result.Thinking,
						InThinking: parser.InThink,
					}
				}

				// For Codex adapter, we may not have index — use a single buffer
				idx := 0
				if evt.ToolCallIdx > 0 {
					idx = evt.ToolCallIdx
				}
				buf, ok := toolBuffers[idx]
				if !ok {
					buf = &toolCallBuffer{}
					toolBuffers[idx] = buf
				}
				if evt.ToolCallID != "" {
					buf.ID = evt.ToolCallID
				}
				if evt.ToolCallType != "" {
					buf.Type = evt.ToolCallType
				}
				if evt.ToolCallName != "" {
					buf.NameBuf.WriteString(evt.ToolCallName)
				}
				if evt.ToolCallDelta != "" {
					buf.ArgsBuf.WriteString(evt.ToolCallDelta)
					ch <- StreamEvent{
						ToolCallDelta: evt.ToolCallDelta,
						ToolCallIdx:   idx,
						ToolCallName:  buf.NameBuf.String(),
					}
				}
				continue
			}

			// Handle text/thinking content
			if evt.Text != "" || evt.Thinking != "" {
				// For thinking events, use the parser to handle think blocks
				if evt.Thinking != "" {
					result := parser.Process(evt.Thinking)
					if result.Text != "" || result.Thinking != "" {
						ch <- StreamEvent{
							Text:       result.Text,
							Thinking:   result.Thinking,
							InThinking: parser.InThink,
						}
					}
				} else if evt.Text != "" {
					result := parser.Process(evt.Text)
					if result.Text != "" || result.Thinking != "" {
						ch <- StreamEvent{
							Text:       result.Text,
							Thinking:   result.Thinking,
							InThinking: parser.InThink,
						}
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				// Return: Context cancelled during scanner error check
				ch <- StreamEvent{Done: true}
				return
			}
			// Return: Scanner error (malformed SSE, read error)
			ch <- StreamEvent{Error: fmt.Errorf("read stream: %w", err)}
			return
		}

		// Return: Stream ended naturally without [DONE] marker
		result := parser.Flush()
		if result.Text != "" || result.Thinking != "" {
			ch <- StreamEvent{
				Text:       result.Text,
				Thinking:   result.Thinking,
				InThinking: parser.InThink,
			}
		}
		if len(toolBuffers) > 0 {
			tc := flushToolCalls(toolBuffers)
			ch <- StreamEvent{ToolCalls: tc}
		}
		ch <- StreamEvent{Done: true}
	}()

	return ch
}

// toolCallBuffer accumulates tool call deltas by index during streaming.
type toolCallBuffer struct {
	ID      string
	Type    string
	NameBuf strings.Builder
	ArgsBuf strings.Builder
}

// RepairArgs attempts to fix malformed JSON from the model's streamed arguments.
// Returns the repaired string and whether it is now valid JSON.
// If repair fails completely, returns a sanitized version that is safe to embed
// in API requests (won't cause HTTP 400 from invalid escape sequences).
func RepairArgs(args string) (string, bool) {
	if args == "" {
		return args, false
	}
	// Fast path: already valid
	var check map[string]interface{}
	if json.Unmarshal([]byte(args), &check) == nil {
		return args, true
	}
	// Try repair
	repaired, err := jsonrepair.Repair(args)
	if err == nil && repaired != "" {
		if json.Unmarshal([]byte(repaired), &check) == nil {
			return repaired, true
		}
	}
	// jsonrepair failed or produced unparseable output.
	// Try closing an unclosed brace.
	t := strings.TrimSpace(args)
	if len(t) > 0 && t[0] == '{' && t[len(t)-1] != '}' {
		closed := args + "}"
		if json.Unmarshal([]byte(closed), &check) == nil {
			return closed, true
		}
	}
	// Completely unrepairable. Return a safe placeholder so it doesn't
	// break API requests when embedded in BuildAPIMessages.
	return `{"_error": "malformed JSON from model, original args discarded"}`, false
}

// flushToolCalls converts buffered tool call deltas into ToolCall structs.
func flushToolCalls(buffers map[int]*toolCallBuffer) []ToolCall {
	result := make([]ToolCall, 0, len(buffers))
	for i := 0; i < len(buffers); i++ {
		buf := buffers[i]
		if buf == nil {
			continue
		}
		name := buf.NameBuf.String()
		args := buf.ArgsBuf.String()

		// Repair potentially malformed JSON from the model
		args, _ = RepairArgs(args)

		result = append(result, ToolCall{
			ID:   buf.ID,
			Type: buf.Type,
			Function: struct {
				Name string `json:"name"`
				Args string `json:"arguments"`
			}{Name: name, Args: args},
			Name:     name,
			ArgsJSON: args,
		})
	}
	return result
}

// FetchModels queries /v1/models endpoint and returns model IDs
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

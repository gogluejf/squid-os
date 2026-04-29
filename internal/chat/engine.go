package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"rig-chat/internal/config"
	"rig-chat/internal/tools"
	"strings"
	"time"
)

// StreamEvent is sent for each SSE chunk during inference
type StreamEvent struct {
	Text       string     // visible delta text
	Thinking   string     // thinking delta text
	InThinking bool       // currently inside think block
	Done       bool       // stream finished
	StopReason string     // from the final chunk
	Error      error      // non-nil on error
	ToolCalls  []ToolCall // non-nil when model requests tool calls
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
	Type     string                 `json:"type"`
	Function map[string]interface{} `json:"function"`
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

// Engine manages chat inference against an OpenAI-compatible endpoint
type Engine struct {
	ChatURL  string
	Model    string
	Thinking bool
	client   *http.Client
}

func NewEngine(chatURL, model string, thinking bool) *Engine {
	return &Engine{
		ChatURL:  chatURL,
		Model:    model,
		Thinking: thinking,
		client: &http.Client{
			Timeout: 0, // no timeout for streaming
		},
	}
}

// toolsToDefinitions converts our Tool structs to API tool definitions.
func toolsToDefinitions(ts []tools.Tool) []toolDefinition {
	defs := make([]toolDefinition, len(ts))
	for i, t := range ts {
		defs[i] = toolDefinition{
			Type: "function",
			Function: map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Schema,
			},
		}
	}
	return defs
}

// Stream sends the chat request and returns a channel of StreamEvents.
// Cancel via the context. The channel is closed when done.
// Pass nil for toolDefs if no tools are available.
func (e *Engine) Stream(ctx context.Context, messages []ChatMessage, toolDefs []tools.Tool) <-chan StreamEvent {
	ch := make(chan StreamEvent, 64)

	go func() {
		defer close(ch)

		reqBody := chatRequest{
			Model:    e.Model,
			Stream:   true,
			Messages: messages,
		}

		if len(toolDefs) > 0 {
			reqBody.Tools = toolsToDefinitions(toolDefs)
			reqBody.ToolChoice = "auto"
		}

		reqBody.ChatTemplateKwargs = map[string]interface{}{
			"enable_thinking": e.Thinking,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			// Return: Failed to marshal request body to JSON
			ch <- StreamEvent{Error: fmt.Errorf("marshal request: %w", err)}
			return
		}

		var prettyBody bytes.Buffer
		json.Indent(&prettyBody, body, "", "  ")
		f, _ := os.Create("/tmp/rig-chat-debug.json")
		defer f.Close()
		f.Write(prettyBody.Bytes())

		req, err := http.NewRequestWithContext(ctx, "POST", e.ChatURL, bytes.NewReader(body))
		if err != nil {
			// Return: Failed to create HTTP request
			ch <- StreamEvent{Error: fmt.Errorf("create request: %w", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")

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
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Parse error response body to extract error message
			var errorResp struct {
				Error struct {
					Message string      `json:"message"`
					Type    string      `json:"type"`
					Code    interface{} `json:"code"` // Can be string or number
				} `json:"error"`
			}

			// Try to parse the error response
			if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil && errorResp.Error.Message != "" {
				// Return: API error with message from server
				ch <- StreamEvent{Error: fmt.Errorf("API error [%d]: %s", resp.StatusCode, errorResp.Error.Message)}
			} else {
				// Fallback: generic error with status code
				ch <- StreamEvent{Error: fmt.Errorf("API returned %d", resp.StatusCode)}
			}
			return
		}

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

			var sse sseResponse
			if err := json.Unmarshal([]byte(payload), &sse); err != nil {
				continue
			}

			if len(sse.Choices) == 0 {
				continue
			}

			choice := sse.Choices[0]

			// Process tool call deltas
			for _, tc := range choice.Delta.ToolCalls {
				buf, ok := toolBuffers[tc.Index]
				if !ok {
					buf = &toolCallBuffer{}
					toolBuffers[tc.Index] = buf
				}
				if tc.ID != "" {
					buf.ID = tc.ID
				}
				if tc.Type != "" {
					buf.Type = tc.Type
				}
				if tc.Function != nil {
					if name, ok := tc.Function["name"].(string); ok {
						buf.NameBuf.WriteString(name)
					}
					if args, ok := tc.Function["arguments"].(string); ok {
						buf.ArgsBuf.WriteString(args)
					}
				}
			}

			content := choice.Delta.Content
			if content == "" {
				// Check for finish reason even without content
				if choice.FinishReason != nil {
					// Return: Empty content but has finish_reason (stream complete)
					result := parser.Flush()
					if result.Text != "" || result.Thinking != "" {
						ch <- StreamEvent{
							Text:       result.Text,
							Thinking:   result.Thinking,
							InThinking: parser.InThink,
						}
					}
					// Flush tool calls if finish_reason is tool_calls
					if *choice.FinishReason == "tool_calls" && len(toolBuffers) > 0 {
						tc := flushToolCalls(toolBuffers)
						ch <- StreamEvent{ToolCalls: tc}
					}
					ch <- StreamEvent{Done: true, StopReason: *choice.FinishReason}
					return
				}
				continue
			}

			result := parser.Process(content)
			if result.Text != "" || result.Thinking != "" {
				ch <- StreamEvent{
					Text:       result.Text,
					Thinking:   result.Thinking,
					InThinking: parser.InThink,
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
func BuildAPIMessages(paths config.Paths, settings config.Settings, messages []config.Message) []ChatMessage {
	var msgs []ChatMessage

	// Add system prompt
	sysPrompt := config.LoadSystemPrompt(paths, settings.SystemPromptFile)
	msgs = append(msgs, ChatMessage{Role: "system", Content: sysPrompt})

	// Convert display messages to API messages
	for _, msg := range messages {
		switch msg.Role {
		case "user":
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
		case "assistant":
			cm := ChatMessage{Role: "assistant", Content: msg.Text}
			if len(msg.ToolCalls) > 0 {
				cm.ToolCalls = make([]ToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					cm.ToolCalls[i] = ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: struct {
							Name string `json:"name"`
							Args string `json:"arguments"`
						}{Name: tc.Name, Args: tc.Arguments},
						Name:     tc.Name,
						ArgsJSON: tc.Arguments,
					}
				}
				if msg.Text == "" {
					cm.Content = "" // still needed for models that expect it
				}
			}
			msgs = append(msgs, cm)
		case "tool":
			for _, tr := range msg.ToolResults {
				msgs = append(msgs, ChatMessage{
					Role:       "tool",
					ToolCallID: tr.ToolCallID,
					Content:    tr.Result,
					Name:       tr.Name,
				})
			}
		}
	}

	return msgs
}

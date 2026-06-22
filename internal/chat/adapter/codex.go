package adapter

import (
	"encoding/json"
	"squid-os/internal/tools"
)

// CodexAdapter implements the OpenAI Responses API format.
type CodexAdapter struct{}

type codexRequest struct {
	Model        string            `json:"model"`
	Instructions string            `json:"instructions,omitempty"`
	Input        []json.RawMessage `json:"input"`
	Tools        []codexToolDef    `json:"tools,omitempty"`
	Stream       bool              `json:"stream"`
	Store        bool              `json:"store"`
	ToolChoice   interface{}       `json:"tool_choice,omitempty"`
	Text         *codexTextFmt     `json:"text,omitempty"`
	Reasoning    *codexReasoning   `json:"reasoning,omitempty"`
}

type codexTextFmt struct {
	Format    codexTextFmtType `json:"format"`
	Verbosity string           `json:"verbosity,omitempty"`
}

type codexTextFmtType struct {
	Type string `json:"type"`
}

type codexReasoning struct {
	Context string `json:"context"`
	Effort  string `json:"effort,omitempty"`
}

// Input item: {"type": "message", "role": "...", "content": [...]}
type codexMessage struct {
	Type    string         `json:"type"`
	Role    string         `json:"role,omitempty"`
	Content []codexContent `json:"content,omitempty"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Input item: {"type": "function_call", "call_id": "...", "name": "...", "arguments": "..."}
type codexFunctionCall struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Input item: {"type": "function_call_output", "call_id": "...", "output": "..."}
type codexFunctionCallOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type codexToolDef struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatMsg struct {
	Role       string         `json:"role"`
	Content    interface{}    `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		Args string `json:"arguments"`
	} `json:"function,omitempty"`
}

type codexTextDelta struct {
	Delta string `json:"delta"`
}

type codexReasoningDelta struct {
	Delta string `json:"delta"`
}

func (a *CodexAdapter) BuildBody(model string, messagesJSON []byte, toolDefs []tools.Tool, thinking bool) ([]byte, error) {
	var msgs []chatMsg
	if err := json.Unmarshal(messagesJSON, &msgs); err != nil {
		return nil, err
	}

	var sysPrompt string
	var inputItems []json.RawMessage

	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			if str, ok := msg.Content.(string); ok {
				sysPrompt = str
			}

		case "user":
			item := codexMessage{Type: "message", Role: "user"}
			if str, ok := msg.Content.(string); ok {
				item.Content = []codexContent{{Type: "input_text", Text: str}}
			} else if parts, ok := msg.Content.([]interface{}); ok {
				for _, p := range parts {
					if part, ok := p.(map[string]interface{}); ok {
						if ptype, ok := part["type"].(string); ok && ptype == "text" {
							if text, ok := part["text"].(string); ok {
								item.Content = append(item.Content, codexContent{Type: "input_text", Text: text})
							}
						}
					}
				}
			}
			data, _ := json.Marshal(item)
			inputItems = append(inputItems, data)

		case "assistant":
			// If the assistant has text content, emit a message item
			if str, ok := msg.Content.(string); ok && str != "" {
				item := codexMessage{Type: "message", Role: "assistant"}
				item.Content = []codexContent{{Type: "output_text", Text: str}}
				data, _ := json.Marshal(item)
				inputItems = append(inputItems, data)
			}
			// Emit each tool call as a standalone function_call item
			for _, tc := range msg.ToolCalls {
				item := codexFunctionCall{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Args,
				}
				data, _ := json.Marshal(item)
				inputItems = append(inputItems, data)
			}
			// Assistant with neither text nor tool calls (edge case) — skip

		case "tool":
			// Tool result as function_call_output
			var output string
			if str, ok := msg.Content.(string); ok {
				output = str
			}
			item := codexFunctionCallOutput{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: output,
			}
			data, _ := json.Marshal(item)
			inputItems = append(inputItems, data)

		case "synthetic":
			item := codexMessage{Type: "message", Role: "assistant"}
			if str, ok := msg.Content.(string); ok {
				item.Content = []codexContent{{Type: "output_text", Text: str}}
			}
			data, _ := json.Marshal(item)
			inputItems = append(inputItems, data)
		}
	}

	req := codexRequest{
		Model:  model,
		Stream: true,
		Store:  false,
		Input:  inputItems,
		Text: &codexTextFmt{
			Format:    codexTextFmtType{Type: "text"},
			Verbosity: "medium",
		},
	}

	if sysPrompt != "" {
		req.Instructions = sysPrompt
	}

	if thinking {
		req.Reasoning = &codexReasoning{Context: "current_turn", Effort: "low"}
	}

	if len(toolDefs) > 0 {
		req.Tools = make([]codexToolDef, len(toolDefs))
		for i, t := range toolDefs {
			req.Tools[i] = codexToolDef{
				Type:        "function",
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			}
		}
		req.ToolChoice = "auto"
	}

	return json.Marshal(req)
}

func (a *CodexAdapter) ParseSSE(payload string) *AdapterEvent {
	if payload == "[DONE]" {
		return &AdapterEvent{Done: true}
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return nil
	}

	switch envelope.Type {
	case "response.output_text.delta":
		var delta codexTextDelta
		if err := json.Unmarshal([]byte(payload), &delta); err != nil {
			return nil
		}
		return &AdapterEvent{Text: delta.Delta}

	case "response.reasoning_summary_text.delta":
		var delta codexReasoningDelta
		if err := json.Unmarshal([]byte(payload), &delta); err != nil {
			return nil
		}
		return &AdapterEvent{Thinking: delta.Delta}

	// response.output_item.added — new tool call started
	// Payload has: call_id, name, type:"function_call"
	case "response.output_item.added":
		var item struct {
			Item struct {
				CallID string `json:"call_id"`
				Name   string `json:"name"`
				Type   string `json:"type"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil
		}
		if item.Item.Type != "function_call" {
			return nil
		}
		return &AdapterEvent{
			ToolCallID: item.Item.CallID,
			ToolCallName: item.Item.Name,
		}

	// response.function_call_arguments.delta — incremental arg chunks
	// Note: the event name is "function_call_arguments" not "function_call.arguments"
	case "response.function_call_arguments.delta":
		var delta struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &delta); err != nil {
			return nil
		}
		return &AdapterEvent{ToolCallDelta: delta.Delta}

	case "response.completed":
		return &AdapterEvent{Done: true}
	}

	return nil
}

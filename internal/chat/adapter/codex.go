package adapter

import (
	"encoding/json"
	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/tools"
)

// CodexAdapter implements the OpenAI Responses API format.
type CodexAdapter struct{}

type codexRequest struct {
	Model        string           `json:"model"`
	Instructions string           `json:"instructions,omitempty"`
	Input        []codexInputItem `json:"input"`
	Tools        []codexToolDef   `json:"tools,omitempty"`
	Stream       bool             `json:"stream"`
	Store        bool             `json:"store"`
	ToolChoice   interface{}      `json:"tool_choice,omitempty"`
	Text         *codexTextFmt    `json:"text,omitempty"`
	Reasoning    *codexReasoning  `json:"reasoning,omitempty"`
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

type codexInputItem struct {
	Type       string          `json:"type"`
	Role       string          `json:"role,omitempty"`
	Content    []codexContent  `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []codexToolCall `json:"call_arguments,omitempty"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type codexToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function codexFuncCall `json:"function"`
}

type codexFuncCall struct {
	Name string `json:"name"`
	Args string `json:"arguments"`
}

type codexToolDef struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// chatMsg is a minimal view of ChatMessage for JSON decoding
type chatMsg struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Function struct {
		Name string `json:"name"`
		Args string `json:"arguments"`
	} `json:"function,omitempty"`
	Name     string `json:"-"` // not from JSON
	ArgsJSON string `json:"-"` // not from JSON
}

type codexTextDelta struct {
	Delta string `json:"delta"`
}

type codexReasoningDelta struct {
	Delta string `json:"delta"`
}

func (a *CodexAdapter) BuildBody(model string, messagesJSON []byte, toolDefs []tools.Tool, thinking bool) ([]byte, error) {
	// Decode the messages from the engine's JSON representation
	var msgs []chatMsg
	if err := json.Unmarshal(messagesJSON, &msgs); err != nil {
		return nil, err
	}

	var sysPrompt string
	var inputItems []codexInputItem

	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			if str, ok := msg.Content.(string); ok {
				sysPrompt = str
			}
		case "user":
			item := codexInputItem{Type: "message", Role: "user"}
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
			inputItems = append(inputItems, item)
		case "assistant":
			item := codexInputItem{Type: "message", Role: "assistant"}
			if str, ok := msg.Content.(string); ok {
				item.Content = []codexContent{{Type: "output_text", Text: str}}
			}
			if len(msg.ToolCalls) > 0 {
				item.ToolCalls = make([]codexToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					item.ToolCalls[i] = codexToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: codexFuncCall{
							Name: tc.Function.Name,
							Args: tc.Function.Args,
						},
					}
				}
			}
			inputItems = append(inputItems, item)
		case "tool":
			item := codexInputItem{
				Type:       "function_call_output",
				ToolCallID: msg.ToolCallID,
			}
			if str, ok := msg.Content.(string); ok {
				item.Content = []codexContent{{Type: "output_text", Text: str}}
			}
			inputItems = append(inputItems, item)
		case "synthetic":
			item := codexInputItem{Type: "message", Role: "assistant"}
			if str, ok := msg.Content.(string); ok {
				item.Content = []codexContent{{Type: "output_text", Text: str}}
			}
			inputItems = append(inputItems, item)
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

	case "response.function_call.arguments.delta":
		var delta struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &delta); err != nil {
			return nil
		}
		return &AdapterEvent{ToolCallDelta: delta.Delta}

	case "response.function_call.name.delta":
		var delta struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &delta); err != nil {
			return nil
		}
		return &AdapterEvent{ToolCallName: delta.Delta}

	case "response.completed":
		return &AdapterEvent{Done: true}
	}

	return nil
}

func (a *CodexAdapter) GetChatURL(settings *config.ProviderSettings) string {
	if settings.Credentials != nil && settings.Credentials.ActiveAuthMethod == config.AuthOAuth {
		return "https://chatgpt.com/backend-api/codex/responses"
	}
	return "https://api.openai.com/v1/responses"
}

func (a *CodexAdapter) GetModelsURL(settings *config.ProviderSettings) string {
	meta := provider.GetMetaForAdapter(settings.Name)
	if meta.ModelsURL != "" {
		return meta.ModelsURL
	}
	return ""
}

// CodexOAuthModels returns the list of models available via OpenAI Codex OAuth.
func CodexOAuthModels() []string {
	return []string{
		"gpt-5.1-codex",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
		"gpt-5.2",
		"gpt-5.2-codex",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
	}
}

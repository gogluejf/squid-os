package adapter

import (
	"encoding/json"
	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/tools"
)

// ChatCompletionsAdapter implements the standard OpenAI-compatible /v1/chat/completions format.
type ChatCompletionsAdapter struct{}

type chatRequest struct {
	Model              string                 `json:"model"`
	Stream             bool                   `json:"stream"`
	Messages           json.RawMessage        `json:"messages"`
	Tools              []toolDefinition       `json:"tools,omitempty"`
	ToolChoice         interface{}            `json:"tool_choice,omitempty"`
	ChatTemplateKwargs map[string]interface{} `json:"chat_template_kwargs,omitempty"`
}

type toolDefinition struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func (a *ChatCompletionsAdapter) BuildBody(model string, messagesJSON []byte, toolDefs []tools.Tool, thinking bool) ([]byte, error) {
	reqBody := chatRequest{
		Model:    model,
		Stream:   true,
		Messages: messagesJSON,
	}

	if len(toolDefs) > 0 {
		reqBody.Tools = make([]toolDefinition, len(toolDefs))
		for i, t := range toolDefs {
			reqBody.Tools[i] = toolDefinition{
				Type: "function",
				Function: functionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Schema,
				},
			}
		}
		reqBody.ToolChoice = "auto"
	}

	reqBody.ChatTemplateKwargs = map[string]interface{}{
		"enable_thinking": thinking,
	}

	return json.Marshal(reqBody)
}

type sseResponse struct {
	Choices []sseChoice `json:"choices"`
}

type sseChoice struct {
	Delta struct {
		Content   string                 `json:"content"`
		ToolCalls []sseToolCallDelta     `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type sseToolCallDelta struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Index    int                    `json:"index"`
	Function map[string]interface{} `json:"function,omitempty"`
}

func (a *ChatCompletionsAdapter) ParseSSE(payload string) *AdapterEvent {
	if payload == "[DONE]" {
		return &AdapterEvent{Done: true}
	}

	var sse sseResponse
	if err := json.Unmarshal([]byte(payload), &sse); err != nil {
		return nil
	}

	if len(sse.Choices) == 0 {
		return nil
	}

	choice := sse.Choices[0]

	if choice.Delta.Content == "" && choice.FinishReason != nil {
		return &AdapterEvent{Done: true, StopReason: *choice.FinishReason}
	}

	if len(choice.Delta.ToolCalls) > 0 {
		evt := &AdapterEvent{}
		for _, tc := range choice.Delta.ToolCalls {
			if tc.ID != "" {
				evt.ToolCallID = tc.ID
			}
			if tc.Type != "" {
				evt.ToolCallType = tc.Type
			}
			if tc.Function != nil {
				if name, ok := tc.Function["name"].(string); ok {
					evt.ToolCallName = name
				}
				if args, ok := tc.Function["arguments"].(string); ok && args != "" {
					evt.ToolCallDelta = args
					evt.ToolCallIdx = tc.Index
				}
			}
		}
		return evt
	}

	content := choice.Delta.Content
	if content != "" {
		return &AdapterEvent{Text: content}
	}

	return nil
}

func (a *ChatCompletionsAdapter) GetChatURL(settings *config.ProviderSettings) string {
	meta := provider.GetMetaForAdapter(settings.Name)
	if meta.ChatURL != "" {
		return meta.ChatURL
	}
	if meta.Dialect == config.DialectAnthropic {
		return settings.BaseURL + "/v1/messages"
	}
	return settings.BaseURL + "/v1/chat/completions"
}

func (a *ChatCompletionsAdapter) GetModelsURL(settings *config.ProviderSettings) string {
	meta := provider.GetMetaForAdapter(settings.Name)
	if meta.ModelsURL != "" {
		return meta.ModelsURL
	}
	if meta.Dialect == config.DialectOpenAICompatible {
		return settings.BaseURL + "/v1/models"
	}
	return ""
}

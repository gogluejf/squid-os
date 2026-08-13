package chat

import (
	"encoding/json"
	"fmt"
	"squid-os/internal/config"
	"squid-os/internal/tools"

	goai_provider "github.com/zendev-sh/goai/provider"
)

// ContextTokens holds raw, compacted, and saved provider-message tokens.
type ContextTokens struct {
	Raw             int // tokens in the un-compacted provider messages
	RawInput        int // raw system, user, and tool-result tokens
	RawOutput       int // raw assistant text, reasoning, and tool-call tokens
	Compacted       int // tokens in the compacted projection
	CompactedInput  int // compacted system, user, and tool-result tokens
	CompactedOutput int // compacted assistant text, reasoning, and tool-call tokens
	Saved           int // Raw - Compacted
}

// Context holds the next provider-message snapshot and its token accounting.
type Context struct {
	Messages   []goai_provider.Message
	Compaction CompactionPlan
	Tokens     ContextTokens
}

// TokenTally converts request accounting to the persisted context tally.
func (c Context) TokenTally() config.ContextTokenTally {
	return config.ContextTokenTally{
		Raw:              c.Tokens.Raw,
		RawInput:         c.Tokens.RawInput,
		RawOutput:        c.Tokens.RawOutput,
		Compacted:        c.Tokens.Compacted,
		CompactedInput:   c.Tokens.CompactedInput,
		CompactedOutput:  c.Tokens.CompactedOutput,
		Saved:            c.Tokens.Saved,
		SavedInstruction: c.Compaction.Tokens.SavedInstruction,
		SavedExecution:   c.Compaction.Tokens.SavedExecution,
	}
}

// BuildContext returns raw or compacted outgoing messages based on enabled.
// Token counts always include the potential compaction projection.
func BuildContext(messages []config.Message, enabled bool) Context {
	plan := BuildCompactionPlan(messages)
	rawMsgs := BuildAPIMessages(messages)
	rawTokens := tallyAPIMessagesTokens(rawMsgs)

	compactedMsgs := buildCompactedAPIMessages(messages, plan)
	compactedTokens := tallyAPIMessagesTokens(compactedMsgs)
	tokens := ContextTokens{
		Raw:             rawTokens.Input + rawTokens.Output,
		RawInput:        rawTokens.Input,
		RawOutput:       rawTokens.Output,
		Compacted:       compactedTokens.Input + compactedTokens.Output,
		CompactedInput:  compactedTokens.Input,
		CompactedOutput: compactedTokens.Output,
	}
	tokens.Saved = tokens.Raw - tokens.Compacted

	if enabled {
		return Context{
			Messages:   compactedMsgs,
			Compaction: plan,
			Tokens:     tokens,
		}
	}

	return Context{
		Messages:   rawMsgs,
		Compaction: plan,
		Tokens:     tokens,
	}
}

func buildCompactedAPIMessages(messages []config.Message, plan CompactionPlan) []goai_provider.Message {
	return buildProviderMessages(messages, &plan)
}

func buildProviderMessages(messages []config.Message, plan *CompactionPlan) []goai_provider.Message {
	var out []goai_provider.Message

	var sysParts []string
	for _, msg := range messages {
		if msg.Role == config.RoleSystem {
			sysParts = append(sysParts, msg.Text)
		}
	}
	if len(sysParts) > 0 {
		out = append(out, goai_provider.Message{
			Role:    goai_provider.RoleSystem,
			Content: []goai_provider.Part{{Type: goai_provider.PartText, Text: joinSystemParts(sysParts)}},
		})
	}

	for _, msg := range messages {
		switch msg.Role {
		case config.RoleSystem, config.RoleInternal:
			continue
		case config.RoleUser:
			out = appendUserProviderMessage(out, msg)
		case config.RoleAssistant:
			out = appendAssistantProviderMessages(out, msg, plan)
		case config.RoleSynthetic:
			out = append(out, goai_provider.Message{
				Role:    goai_provider.RoleAssistant,
				Content: []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}},
			})
		}
	}

	return out
}

func appendUserProviderMessage(out []goai_provider.Message, msg config.Message) []goai_provider.Message {
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
			return append(out, goai_provider.Message{Role: goai_provider.RoleUser, Content: goaiParts})
		}
	}
	return append(out, goai_provider.Message{
		Role:    goai_provider.RoleUser,
		Content: []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}},
	})
}

func appendAssistantProviderMessages(out []goai_provider.Message, msg config.Message, plan *CompactionPlan) []goai_provider.Message {
	var parts []goai_provider.Part
	if msg.ThinkingText != "" {
		parts = append(parts, goai_provider.Part{Type: goai_provider.PartReasoning, Text: msg.ThinkingText})
	}
	if msg.Text != "" {
		parts = append(parts, goai_provider.Part{Type: goai_provider.PartText, Text: msg.Text})
	}
	for _, tc := range msg.ToolCalls {
		args := tc.Instruction.Arguments
		if plan != nil {
			if d, ok := plan.Decisions[tc.ID]; ok && d.CompactArguments {
				args = compactArguments(tc, d)
			}
		}
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
		if plan != nil {
			if d, ok := plan.Decisions[tc.ID]; ok && d.CompactResult {
				content = compactResult(tc, d)
			}
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
	return out
}

// compactArguments replaces superseded tool-call arguments with valid compact JSON.
func compactArguments(tc config.ToolCallEntry, d CompactionDecision) string {
	path := d.Path
	if path == "" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Instruction.Arguments), &args); err == nil {
			if p, ok := args["path"].(string); ok {
				path = p
			}
		}
	}

	switch tc.Instruction.Name {
	case "read_file":
		b, _ := json.Marshal(map[string]interface{}{"path": path})
		return string(b)
	case "write_file":
		b, _ := json.Marshal(map[string]interface{}{"path": path, "content": "<COMPACTED>"})
		return string(b)
	case "edit_file":
		b, _ := json.Marshal(map[string]interface{}{"path": path, "old_string": "<COMPACTED>", "new_string": "<COMPACTED>"})
		return string(b)
	default:
		b, _ := json.Marshal(map[string]interface{}{"path": path})
		return string(b)
	}
}

// compactResult replaces superseded tool-call results with compact text.
func compactResult(tc config.ToolCallEntry, d CompactionDecision) string {
	path := d.Path
	if path == "" {
		path = "<unknown>"
	}

	switch tc.Instruction.Name {
	case "read_file":
		return fmt.Sprintf("[compacted] file read: %s", path)
	case "write_file":
		return fmt.Sprintf("[compacted] file written: %s", path)
	case "edit_file":
		return fmt.Sprintf("[compacted] file edited: %s", path)
	default:
		return fmt.Sprintf("[compacted] tool result: %s", path)
	}
}

// joinSystemParts concatenates system message texts with \n\n.
func joinSystemParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n\n"
		}
		out += p
	}
	return out
}

type apiMessageTokenTally struct {
	Input  int
	Output int
}

// tallyAPIMessagesTokens approximates provider-message tokens by role.
func tallyAPIMessagesTokens(msgs []goai_provider.Message) apiMessageTokenTally {
	var tally apiMessageTokenTally
	for _, msg := range msgs {
		msgTokens := 0
		for _, part := range msg.Content {
			switch part.Type {
			case goai_provider.PartText, goai_provider.PartReasoning:
				msgTokens += CountTokensApproxString(part.Text)
			case goai_provider.PartToolCall:
				msgTokens += CountTokensApproxString(part.ToolCallID)
				msgTokens += CountTokensApproxString(part.ToolName)
				msgTokens += CountTokensApproxString(string(part.ToolInput))
			case goai_provider.PartToolResult:
				msgTokens += CountTokensApproxString(part.ToolCallID)
				msgTokens += CountTokensApproxString(part.ToolName)
				msgTokens += CountTokensApproxString(part.ToolOutput)
			case goai_provider.PartImage:
				msgTokens += CountTokensApproxString(part.URL)
			}
		}
		switch msg.Role {
		case goai_provider.RoleAssistant:
			tally.Output += msgTokens
		default:
			tally.Input += msgTokens
		}
	}
	return tally
}

func countAPIMessagesTokens(msgs []goai_provider.Message) int {
	tally := tallyAPIMessagesTokens(msgs)
	return tally.Input + tally.Output
}

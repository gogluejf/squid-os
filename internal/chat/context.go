package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/media"
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
	ToolDefinitions int // request-body tool schema cost (internal messages), never compacted
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
		ToolDefinitions:  c.Tokens.ToolDefinitions,
	}
}

// BuildContext returns raw or compacted outgoing messages based on enabled.
// Token counts always include the potential compaction projection.
// Pass empty baseDir and nil attachments if attachment resolution is not needed.
// Pass nil caps to skip capability-based omission.
func BuildContext(messages []config.Message, enabled bool, baseDir string, attachments []media.Attachment, caps *goai_provider.ModelCapabilities) Context {
	plan := BuildCompactionPlan(messages)
	rawMsgs := buildProviderMessages(messages, nil, attachments, baseDir, caps)
	rawTokens := tallyAPIMessagesTokens(rawMsgs)

	compactedMsgs := buildProviderMessages(messages, &plan, attachments, baseDir, caps)
	compactedTokens := tallyAPIMessagesTokens(compactedMsgs)
	toolDefs := internalWireTokens(messages)
	tokens := ContextTokens{
		Raw:             rawTokens.Input + rawTokens.Output + toolDefs,
		RawInput:        rawTokens.Input + toolDefs,
		RawOutput:       rawTokens.Output,
		Compacted:       compactedTokens.Input + compactedTokens.Output + toolDefs,
		CompactedInput:  compactedTokens.Input + toolDefs,
		CompactedOutput: compactedTokens.Output,
		ToolDefinitions: toolDefs,
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

func internalWireTokens(messages []config.Message) int {
	var n int
	for _, m := range messages {
		if m.Role == config.RoleInternal {
			n += m.InputTokens
		}
	}
	return n
}

// buildProviderMessages builds the provider-message projection. When plan is
// non-nil, superseded tool calls are compacted per the plan's decisions.
func buildProviderMessages(messages []config.Message, plan *CompactionPlan, attachments []media.Attachment, baseDir string, caps *goai_provider.ModelCapabilities) []goai_provider.Message {
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
			parts := buildUserMessageParts(msg, attachments, baseDir, caps)
			out = append(out, goai_provider.Message{
				Role:    goai_provider.RoleUser,
				Content: parts,
			})
		case config.RoleAssistant:
			out = appendAssistantProviderMessages(out, msg, plan, attachments, baseDir, caps)
		case config.RoleSynthetic:
			out = append(out, goai_provider.Message{
				Role:    goai_provider.RoleAssistant,
				Content: []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}},
			})
		}
	}

	return out
}

func appendAssistantProviderMessages(out []goai_provider.Message, msg config.Message, plan *CompactionPlan, attachments []media.Attachment, baseDir string, caps *goai_provider.ModelCapabilities) []goai_provider.Message {
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

		// For inspect_media, generate a synthetic user multimodal message
		// so the model receives the media as a user message (not a tool result).
		if tc.Instruction.Name == "inspect_media" && tc.Execution.Status == tools.ResultStatusSuccess {
			query := extractInspectMediaQuery(tc.Instruction.Arguments)
			attachmentRef := tc.Execution.Result // "@file:<id>"
			syntheticParts := buildSyntheticInspectParts(query, attachmentRef, attachments, baseDir, caps)
			if len(syntheticParts) > 0 {
				out = append(out, goai_provider.Message{
					Role:    goai_provider.RoleUser,
					Content: syntheticParts,
				})
			}
		}
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

// buildUserMessageParts constructs the content parts for a user message.
// It resolves stored attachment refs into GoAI parts (image, file, text).
// Caps-based omission: if the model doesn't support a modality, the part
// is skipped and an omission note is appended.
func buildUserMessageParts(msg config.Message, attachments []media.Attachment, baseDir string, caps *goai_provider.ModelCapabilities) []goai_provider.Part {
	if len(msg.Attachments) == 0 || baseDir == "" {
		return []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}}
	}

	var parts []goai_provider.Part
	parts = append(parts, goai_provider.Part{Type: goai_provider.PartText, Text: msg.Text})
	var omitted []string
	for _, ref := range msg.Attachments {
		a, found := media.ResolveRef(attachments, ref.File)
		if !found {
			continue
		}
		if caps != nil && MediaDecisionFor(caps, a.Kind) == MediaOmit {
			omitted = append(omitted, a.FileName)
			continue
		}
		part, err := resolveAttachmentToPart(a, baseDir)
		if err != nil {
			continue
		}
		parts = append(parts, part)
	}
	if len(omitted) > 0 {
		parts = append(parts, goai_provider.Part{Type: goai_provider.PartText, Text: "[omitted: model does not support " + strings.Join(omitted, ", ") + "]"})
	}
	return parts
}
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

// extractInspectMediaQuery extracts the "query" argument from inspect_media tool call arguments.
func extractInspectMediaQuery(argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	if query, ok := args["query"].(string); ok {
		return query
	}
	return ""
}

// buildSyntheticInspectParts builds the synthetic user message parts for an
// inspect_media result. It resolves the @file:<ref> attachment and combines
// the query text with the resolved media part.
func buildSyntheticInspectParts(query, attachmentRef string, attachments []media.Attachment, baseDir string, caps *goai_provider.ModelCapabilities) []goai_provider.Part {
	if attachmentRef == "" {
		return []goai_provider.Part{{Type: goai_provider.PartText, Text: query}}
	}

	fileName := strings.TrimPrefix(attachmentRef, "@file:")
	a, found := media.ResolveRef(attachments, fileName)
	if !found || baseDir == "" {
		return []goai_provider.Part{{Type: goai_provider.PartText, Text: query + " " + attachmentRef}}
	}

	if caps != nil && MediaDecisionFor(caps, a.Kind) == MediaOmit {
		return []goai_provider.Part{{Type: goai_provider.PartText, Text: query + " [omitted: model does not support " + string(a.Kind) + "]"}}
	}

	part, err := resolveAttachmentToPart(a, baseDir)
	if err != nil {
		return []goai_provider.Part{{Type: goai_provider.PartText, Text: query + " " + attachmentRef}}
	}

	return []goai_provider.Part{
		{Type: goai_provider.PartText, Text: query},
		part,
	}
}

type apiMessageTokenTally struct {
	Input  int
	Output int
}

// tallyAPIMessagesTokens approximates provider-message tokens by role.
// For media parts (images, files), it uses conservative size-based estimates
// instead of tokenizing base64-encoded URLs, which would massively overcount.
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
			case goai_provider.PartImage, goai_provider.PartFile:
				// Media parts: use conservative estimate instead of counting
				// base64-encoded data URI length as text tokens.
				// The Part.URL contains a data URI like "data:image/png;base64,..."
				// which is ~33% larger than the raw bytes and not meaningful text.
				// Estimate ~1 token per 4 bytes of the underlying media.
				if part.URL != "" {
					msgTokens += estimateMediaPartTokens(part.URL, part.MediaType)
				}
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

// estimateMediaPartTokens returns a conservative token estimate for a media
// part (image or file) based on its data URI.
//
// TODO: Stubbed to return 1 until we implement proper estimation.
func estimateMediaPartTokens(dataURI, mimeType string) int {
	return 1
	/*
		// Extract base64 data after "data:...;base64,"
		const prefix = ";base64,"
		idx := strings.Index(dataURI, prefix)
		if idx < 0 {
			// Not a data URI — might be a regular URL. Use a small conservative
			// estimate for remote image references.
			return 256 // typical minimum tile cost
		}
		b64Len := len(dataURI) - idx - len(prefix)
		// base64 expands by ~33%, so raw bytes ≈ b64Len * 0.75
		// Token estimate: ~1 token per 4 raw bytes
		rawBytes := b64Len * 3 / 4
		if rawBytes == 0 {
			return 1
		}
		return rawBytes / 4
	*/
}

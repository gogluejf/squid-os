package app

import (
	"encoding/json"
	"fmt"
	"squid-os/internal/config"
	"squid-os/internal/tools"
	"strings"
)

// chatSession bundles the active chat: its session file and render cache.
// Messages live in file.Messages — there is no separate copy.
type chatSession struct {
	file             config.SessionFile
	renderedMessages []string // glamour cache, 1:1 with file.Messages
	renderedWidth    int
	undoStack        [][]config.Message
}

// clear resets to a fresh session and pushes init messages (system prompt + tools).
func (cs *chatSession) clear(provider, model string, thinking bool, systemPromptFile string, paths config.Paths) {
	cs.file = config.NewSessionFile(provider, model, thinking, systemPromptFile)
	cs.renderedMessages = nil
	cs.renderedWidth = 0
	cs.undoStack = nil

	// Push system prompt message (included in API as system role)
	sysContent := config.LoadSystemPrompt(paths, systemPromptFile)
	cs.appendMsg(config.Message{
		ID:          "sys0",
		Role:        config.RoleSystem,
		Text:        sysContent,
		Label:       "System Prompt",
		Params:      map[string]string{"file": systemPromptFile},
		InputTokens: countTokensApprox(sysContent),
	})

	// Push tools enabled internal message
	names, tokens := buildToolsEnabledContent()
	if names != "" {
		cs.appendMsg(config.Message{
			ID:          "tools0",
			Role:        config.RoleInternal,
			Text:        names,
			Label:       "Tools Enabled",
			Params:      map[string]string{"count": fmt.Sprintf("%d", len(tools.GetTools()))},
			InputTokens: tokens,
		})
	}
}

// updateSystemPromptMsg updates the existing sys0 message and pushes an internal message.
func (cs *chatSession) updateSystemPromptMsg(oldFile, newFile string, paths config.Paths) {
	for i := range cs.file.Messages {
		if cs.file.Messages[i].ID == "sys0" {
			newContent := config.LoadSystemPrompt(paths, newFile)
			cs.file.Messages[i].Text = newContent
			cs.file.Messages[i].Label = "System Prompt"
			cs.file.Messages[i].Params = map[string]string{"file": newFile}
			cs.file.Messages[i].InputTokens = countTokensApprox(newContent)

			// Push internal message for the change
			cs.appendMsg(config.Message{
				ID:    fmt.Sprintf("msg_%d", len(cs.file.Messages)+1),
				Role:  config.RoleInternal,
				Text:  fmt.Sprintf("Switched system prompt from %s to %s", oldFile, newFile),
				Label: "System Prompt Changed",
				Params: map[string]string{"from": oldFile, "to": newFile},
			})
			return
		}
	}
}

// pushModelSwitchMsg pushes an internal message when the model is switched.
func (cs *chatSession) pushModelSwitchMsg(oldModel, newModel string) {
	cs.appendMsg(config.Message{
		ID:    fmt.Sprintf("msg_%d", len(cs.file.Messages)+1),
		Role:  config.RoleInternal,
		Text:  fmt.Sprintf("Switched model from %s to %s", oldModel, newModel),
		Label: "Model Switched",
		Params: map[string]string{"from": oldModel, "to": newModel},
	})
}

// buildToolsEnabledContent returns a comma-separated list of tool names and the
// token count of the raw JSON tool definitions (sent in the API request body).
func buildToolsEnabledContent() (string, int) {
	tl := tools.GetTools()
	if len(tl) == 0 {
		return "", 0
	}

	// Build display string: just comma-separated names.
	names := make([]string, len(tl))
	for i, t := range tl {
		names[i] = t.Name
	}
	display := strings.Join(names, ", ")

	// Count tokens from the raw JSON we'd actually send in the API request.
	type toolDef struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	defs := make([]toolDef, len(tl))
	for i, t := range tl {
		defs[i] = toolDef{Type: "function"}
		defs[i].Function.Name = t.Name
		defs[i].Function.Description = t.Description
		defs[i].Function.Parameters = t.Schema
	}
	rawJSON, _ := json.Marshal(defs)
	tokens := countTokensApprox(string(rawJSON))

	return display, tokens
}

// setFrom loads a saved session, replacing all state and clearing the render cache.
// Pass clearUndo=false when previewing so the existing undo stack is preserved.
func (cs *chatSession) setFrom(sf config.SessionFile, clearUndo ...bool) {
	cs.file = sf
	cs.renderedMessages = nil
	cs.renderedWidth = 0
	if len(clearUndo) == 0 || clearUndo[0] {
		cs.undoStack = nil
	}
}

// appendMsg appends a message; the render cache grows lazily in updateViewportContent.
func (cs *chatSession) appendMsg(msg config.Message) {
	cs.file.Messages = append(cs.file.Messages, msg)
}

// truncateTo shrinks messages and cache atomically.
func (cs *chatSession) truncateTo(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(cs.file.Messages) {
		return
	}
	cs.file.Messages = cs.file.Messages[:n]
	cs.invalidateRenderFrom(n)
}

// truncateToUser scans messages backwards, removes everything from the last
// user message to the end, and returns the user message's text and image so
// the caller can restore them. Everything removed is discarded (no undo stack).
func (cs *chatSession) truncateToUser() (userText, userImage string) {
	n := len(cs.file.Messages)
	for i := n - 1; i >= 0; i-- {
		if cs.file.Messages[i].Role == "user" {
			userText, userImage = cs.file.Messages[i].Text, cs.file.Messages[i].ImagePath
			cs.truncateTo(i)
			return userText, userImage
		}
	}
	return "", ""
}

// cancelTruncate always finds and returns the last user message's text/image
// for restoring to the textarea. It truncates only if that user message is the
// last one in the session (i.e., not mid-tool-loop). This lets the user cancel
// mid-loop without losing earlier assistant work, while still getting their
// input back for quick re-edit.
// Returns (userText, userImage, truncated) where truncated is true if the
// user message was the last message and was removed.
func (cs *chatSession) cancelTruncate() (userText, userImage string, truncated bool) {
	n := len(cs.file.Messages)
	if n == 0 {
		return "", "", false
	}

	// Always find the last user message for restoring input.
	for i := n - 1; i >= 0; i-- {
		if cs.file.Messages[i].Role == "user" {
			userText, userImage = cs.file.Messages[i].Text, cs.file.Messages[i].ImagePath
			break
		}
	}

	// Truncate only if the user message is on top.
	if n > 0 && cs.file.Messages[n-1].Role == "user" {
		cs.truncateTo(n - 1)
		truncated = true
	}

	return userText, userImage, truncated
}

// destroyLastSequence removes the last user message and everything after it,
// pushes the removed messages onto the undo stack, and returns the destroyed
// user message's text and image so the caller can restore them to the textarea.
// Handles multi-round tool sequences (user + any number of assistant/tool msgs).
func (cs *chatSession) destroyLastSequence() (userText, userImage string) {
	n := len(cs.file.Messages)
	if n == 0 {
		return "", ""
	}
	for i := n - 1; i >= 0; i-- {
		if cs.file.Messages[i].Role == "user" {
			seq := make([]config.Message, n-i)
			copy(seq, cs.file.Messages[i:])
			cs.undoStack = append(cs.undoStack, seq)
			userText, userImage = cs.file.Messages[i].Text, cs.file.Messages[i].ImagePath
			cs.truncateTo(i)
			return userText, userImage
		}
	}
	return "", ""
}

// userTextImage extracts the text and image from the user message within a range.
func (cs *chatSession) userTextImage(start, end int) (string, string) {
	for i := start; i < end; i++ {
		if cs.file.Messages[i].Role == "user" {
			return cs.file.Messages[i].Text, cs.file.Messages[i].ImagePath
		}
	}
	return "", ""
}

// undoDestroy pops the last destroy, restores its messages to the session,
// and returns what should be placed in the textarea and attachedImage:
// - if more undos remain: the next entry's user message text/image (preview)
// - if stack is now empty: "", ""
func (cs *chatSession) undoDestroy() (textarea, image string, ok bool) {
	if len(cs.undoStack) == 0 {
		return "", "", false
	}
	entry := cs.undoStack[len(cs.undoStack)-1]
	cs.undoStack = cs.undoStack[:len(cs.undoStack)-1]
	restoreAt := len(cs.file.Messages)
	for _, msg := range entry {
		cs.file.Messages = append(cs.file.Messages, msg)
	}
	cs.invalidateRenderFrom(restoreAt)
	// If more undos remain, preview the next one's user message in the textarea
	if len(cs.undoStack) > 0 {
		next := cs.undoStack[len(cs.undoStack)-1]
		for _, msg := range next {
			if msg.Role == "user" {
				return msg.Text, msg.ImagePath, true
			}
		}
	}
	return "", "", true
}

// invalidateRenderFrom truncates the render cache starting from index i.
func (cs *chatSession) invalidateRenderFrom(i int) {
	if i < len(cs.renderedMessages) {
		cs.renderedMessages = cs.renderedMessages[:i]
	}
}

// invalidateRenderAll clears the entire render cache.
func (cs *chatSession) invalidateRenderAll() {
	cs.renderedMessages = nil
}

// totalTokens returns the sum of all token counts across every message.
// Computed from messages so it stays correct after destroy or load.
func (cs *chatSession) totalTokens() int {
	return cs.totalInputTokens() + cs.totalOutputTokens()
}

// totalInputTokens sums user message tokens and tool execution tokens.
func (cs *chatSession) totalInputTokens() int {
	total := 0
	for _, msg := range cs.file.Messages {
		total += msg.InputTokens
		for _, tc := range msg.ToolCalls {
			total += tc.Execution.Tokens
		}
	}
	return total
}

// totalOutputTokens sums assistant and synthetic text tokens.
// Thinking tokens are excluded — they are never sent back to the API.
func (cs *chatSession) totalOutputTokens() int {
	total := 0
	for _, msg := range cs.file.Messages {
		if msg.Role == "assistant" || msg.Role == "synthetic" {
			total += msg.TextMetrics.Tokens
		}
	}
	return total
}

package app

import (
	"fmt"
	"strings"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/environment"
	"squid-os/internal/tools"
	"squid-os/internal/util"
)

// chatSession bundles the active chat: its session file and render cache.
// Messages live in file.Messages — there is no separate copy.
type chatSession struct {
	file             config.SessionFile
	renderedMessages []string // glamour cache, 1:1 with file.Messages
	renderedWidth    int
	undoStack        [][]config.Message
}

// clear resets to a fresh session and pushes init messages (system prompt + env + tools + config).
func (cs *chatSession) clear(settings config.Settings, paths config.Paths, currentDir string) {
	cs.file = config.NewSessionFile(settings.Provider, settings.Model, settings.Thinking, settings.SystemPromptFile)
	cs.renderedMessages = nil
	cs.renderedWidth = 0
	cs.undoStack = nil

	// Push system prompt message (included in API as system role)
	sysContent := config.LoadSystemPrompt(paths, settings.SystemPromptFile)
	cs.appendMsg(config.Message{
		ID:          "sys0",
		Role:        config.RoleSystem,
		Text:        sysContent,
		Label:       "System Prompt",
		Params:      map[string]string{"file": settings.SystemPromptFile},
		InputTokens: countTokensApprox(sysContent),
	})

	// Push environment message (included in API as second system message, after system prompt)
	env := environment.LoadEnvironment(paths, settings, currentDir)
	envContent := environment.FormatEnvironment(env)
	cs.appendMsg(config.Message{
		ID:          "env0",
		Role:        config.RoleSystem,
		Text:        envContent,
		Label:       "Environment",
		InputTokens: countTokensApprox(envContent),
	})

	// Push current config internal message (collapsed: provider=model · thinking=on/off)
	cs.appendMsg(buildConfigMsg(settings.Provider, settings.Model, settings.Thinking))

	// Push tools enabled internal message (collapsed: tools=names, expanded: name→description table)
	toolsMsg := buildToolsEnabledMsg()
	if toolsMsg.Text != "" {
		cs.appendMsg(toolsMsg)
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
				ID:     fmt.Sprintf("msg_%d", len(cs.file.Messages)+1),
				Role:   config.RoleInternal,
				Text:   fmt.Sprintf("Switched system prompt from %s to %s", oldFile, newFile),
				Label:  "System Prompt Changed",
				Params: map[string]string{"from": oldFile, "to": newFile},
			})
			return
		}
	}
}

// updateConfigMsg refreshes the existing config0 message in place with new values.
// Like updateSystemPromptMsg — it updates the fixed-ID message, no history message pushed.
func (cs *chatSession) updateConfigMsg(provider, model string, thinking bool) {
	for i := range cs.file.Messages {
		if cs.file.Messages[i].ID == "config0" {
			cs.file.Messages[i] = buildConfigMsg(provider, model, thinking)

			// Sync session metadata
			cs.file.Session.Provider = provider
			cs.file.Session.Model = model
			cs.file.Session.Thinking = thinking

			return
		}
	}
}

// pushModelSwitchMsg pushes an internal message when the model is switched.
func (cs *chatSession) pushModelSwitchMsg(oldModel, newModel string) {
	cs.appendMsg(config.Message{
		ID:     fmt.Sprintf("msg_%d", len(cs.file.Messages)+1),
		Role:   config.RoleInternal,
		Text:   fmt.Sprintf("Switched model from %s to %s", oldModel, newModel),
		Label:  "Model Switched",
		Params: map[string]string{"from": oldModel, "to": newModel},
	})
}

// buildConfigMsg creates an internal message showing current config state.
// Collapsed: params "provider=... · model=... · thinking=on/off"
// Expanded: multi-line detail.
func buildConfigMsg(provider, model string, thinking bool) config.Message {
	thinkStr := "off"
	if thinking {
		thinkStr = "on"
	}

	return config.Message{
		ID:     "config0",
		Role:   config.RoleInternal,
		Label:  "Config",
		Params: map[string]string{"provider": provider, "model": model, "thinking": thinkStr},
	}
}

// buildToolsEnabledMsg builds the internal tools message.
// Collapsed: shows a single param "tools= name1, name2, ..." (styled with each tool's color)
// Expanded: shows a neat table of name -> description.
// Includes InputTokens from the real JSON sent in the API request body.
func buildToolsEnabledMsg() config.Message {
	tl := tools.GetTools()
	if len(tl) == 0 {
		return config.Message{}
	}

	var names []string
	for _, t := range tl {
		names = append(names, t.Name)
	}

	var b strings.Builder
	descMax := 62 // 15 (name) + 1 (space) + 62 = 78 chars per line, stays on one line
	for i, t := range tl {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%-15s %s", t.Name, util.Truncate(t.Description, descMax)))
	}

	// Fake the token count by measuring the actual JSON sent in the API request.
	rawJSON, _ := chat.MarshalToolsJSON(tl)
	tokens := countTokensApprox(string(rawJSON))

	return config.Message{
		ID:          "tools0",
		Role:        config.RoleInternal,
		Text:        b.String(),
		Label:       "Tools Enabled",
		Params:      map[string]string{"tools": strings.Join(names, ", ")},
		InputTokens: tokens,
	}
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

// hasUserMessage returns true if the session has at least one user message.
func (cs *chatSession) hasUserMessage() bool {
	for _, msg := range cs.file.Messages {
		if msg.Role == "user" {
			return true
		}
	}
	return false
}
func (cs *chatSession) invalidateRenderAll() {
	cs.renderedMessages = nil
}

// invalidateRenderAt clears the cached render at a single index so it gets re-rendered next time.
func (cs *chatSession) invalidateRenderAt(i int) {
	if i < len(cs.renderedMessages) {
		cs.renderedMessages[i] = ""
	}
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
	}
	return total
}

// Thinking tokens are excluded — they are never sent back to the API.
func (cs *chatSession) totalOutputTokens() int {
	total := 0
	for _, msg := range cs.file.Messages {
		total += msg.TextMetrics.Tokens + msg.ToolCallMetrics.Tokens

	}
	return total
}

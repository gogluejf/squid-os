package app

import "rig-chat/internal/config"

// chatSession bundles the active chat: its session file and render cache.
// Messages live in file.Messages — there is no separate copy.
type chatSession struct {
	file             config.SessionFile
	renderedMessages []string // glamour cache, 1:1 with file.Messages
	renderedWidth    int
	undoStack        [][]config.Message
}

// clear resets to a fresh session.
func (cs *chatSession) clear(provider, model string, thinking bool, systemPromptFile string) {
	cs.file = config.NewSessionFile(provider, model, thinking, systemPromptFile)
	cs.renderedMessages = nil
	cs.renderedWidth = 0
	cs.undoStack = nil
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
			cs.truncateTo(i)
			return cs.userTextImage(i + 1, n)
		}
	}
	return "", ""
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
			cs.truncateTo(i)
			return cs.userTextImage(i, n)
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
// Computed from messages so it stays correct after destroy or load
func (cs *chatSession) totalTokens() int {
	total := 0

	// Sum tokens from all saved messages. Thinking tokens are excluded because
	// they are never sent back to the API on subsequent calls — they only exist
	// in the current active inference.
	for _, msg := range cs.file.Messages {
		total += msg.UserTokens + msg.TextTokens
	}
	return total
}

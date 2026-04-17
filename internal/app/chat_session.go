package app

import "rig-chat/internal/config"

// chatSession bundles the active chat: its session file and render cache.
// Messages live in file.Messages — there is no separate copy.
type chatSession struct {
	file             config.SessionFile
	renderedMessages []string // glamour cache, 1:1 with file.Messages
	renderedWidth    int
}

// clear resets to a fresh session.
func (cs *chatSession) clear(provider, model string, thinking bool, systemPromptFile string) {
	cs.file = config.NewSessionFile(provider, model, thinking, systemPromptFile)
	cs.renderedMessages = nil
	cs.renderedWidth = 0
}

// setFrom loads a saved session, replacing all state and clearing the render cache.
func (cs *chatSession) setFrom(sf config.SessionFile) {
	cs.file = sf
	cs.renderedMessages = nil
	cs.renderedWidth = 0
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

// destroyLastPair removes the last user-assistant message pair.
// If there's an odd number of messages, it removes just the last message.
func (cs *chatSession) destroyLastPair() {
	n := len(cs.file.Messages)
	if n == 0 {
		return
	}
	// Remove 2 messages if even count, otherwise remove 1
	removeCount := 2
	if n%2 == 1 {
		removeCount = 1
	}
	cs.truncateTo(n - removeCount)
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
	total := 0
	for _, msg := range cs.file.Messages {
		total += msg.InputTokens + msg.OutputTokens
	}
	return total
}

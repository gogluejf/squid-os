package app

import "rig-chat/internal/config"

// chatSession bundles the active chat: its session file, messages, and render cache.
type chatSession struct {
	file             config.SessionFile
	messages         []config.DisplayMessage
	renderedMessages []string // glamour cache, 1:1 with messages
	renderedWidth    int
	totalTokens      int
}

// clear resets to a fresh session.
func (cs *chatSession) clear(provider, model string, thinking bool, systemPromptFile string) {
	cs.file = config.NewSessionFile(provider, model, thinking, systemPromptFile)
	cs.messages = nil
	cs.renderedMessages = nil
	cs.renderedWidth = 0
	cs.totalTokens = 0
}

// setFrom loads a saved session, replacing all state and clearing the render cache.
func (cs *chatSession) setFrom(sf config.SessionFile) {
	cs.file = sf
	cs.messages = make([]config.DisplayMessage, len(sf.Messages))
	for i, msg := range sf.Messages {
		cs.messages[i] = config.DisplayMessage{Message: msg}
	}
	cs.renderedMessages = nil
	cs.renderedWidth = 0
	cs.totalTokens = sf.TotalTokens
}

// appendMsg appends a message; the render cache grows lazily in updateViewportContent.
func (cs *chatSession) appendMsg(msg config.DisplayMessage) {
	cs.messages = append(cs.messages, msg)
}

// truncateTo shrinks messages and cache atomically.
func (cs *chatSession) truncateTo(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(cs.messages) {
		return
	}
	cs.messages = cs.messages[:n]
	if n < len(cs.renderedMessages) {
		cs.renderedMessages = cs.renderedMessages[:n]
	}
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

// extractMessages strips display-only fields for persistence.
func (cs *chatSession) extractMessages() []config.Message {
	msgs := make([]config.Message, len(cs.messages))
	for i, dm := range cs.messages {
		msgs[i] = dm.Message
	}
	return msgs
}

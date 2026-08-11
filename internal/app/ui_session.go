package app

import (
	"squid-os/internal/chat"
	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/tools"
)

type MessageLineRange struct {
	ID    string
	Start int // inclusive viewport line
	End   int // exclusive viewport line
}

// UISession is the TUI wrapper around a pure chat.Session.
type UISession struct {
	*chat.Session
	UIStream         UIStreamState
	renderedMessages []string
	renderedWidth    int
	messageRanges    []MessageLineRange
	undoStack        [][]config.Message
}

func NewUISession(cfg config.SessionConfig, paths config.Paths, catalog runtimeconfig.Catalog) *UISession {
	return &UISession{Session: chat.NewSession(cfg, paths, catalog)}
}

func NewUISessionFromDoc(sd config.SessionDoc, name string, paths config.Paths, catalog runtimeconfig.Catalog) *UISession {
	return &UISession{Session: chat.LoadSession(sd, name, paths, catalog)}
}

func (u *UISession) destroyLastSequence() (userText, userImage string) {
	n := len(u.Doc.Messages)
	if n == 0 {
		return "", ""
	}
	for i := n - 1; i >= 0; i-- {
		if u.Doc.Messages[i].Role == config.RoleUser {
			seq := make([]config.Message, n-i)
			copy(seq, u.Doc.Messages[i:])
			u.undoStack = append(u.undoStack, seq)
			userText, userImage = u.Doc.Messages[i].Text, u.Doc.Messages[i].ImagePath
			u.TruncateTo(i)
			u.invalidateRenderFrom(i)
			return userText, userImage
		}
	}
	return "", ""
}

func (u *UISession) undoDestroy() (textarea, image string, ok bool) {
	if len(u.undoStack) == 0 {
		return "", "", false
	}
	entry := u.undoStack[len(u.undoStack)-1]
	u.undoStack = u.undoStack[:len(u.undoStack)-1]
	restoreAt := len(u.Doc.Messages)
	for _, msg := range entry {
		u.Append(msg)
	}
	u.invalidateRenderFrom(restoreAt)
	if len(u.undoStack) > 0 {
		next := u.undoStack[len(u.undoStack)-1]
		for _, msg := range next {
			if msg.Role == config.RoleUser {
				return msg.Text, msg.ImagePath, true
			}
		}
	}
	return "", "", true
}

func (u *UISession) invalidateRenderFrom(i int) {
	if i < len(u.renderedMessages) {
		u.renderedMessages = u.renderedMessages[:i]
	}
	u.messageRanges = nil
}

func (u *UISession) invalidateRenderAll() {
	u.renderedMessages = nil
	u.messageRanges = nil
}

func (u *UISession) invalidateRenderAt(i int) {
	if i < len(u.renderedMessages) {
		u.renderedMessages[i] = ""
	}
	u.messageRanges = nil
}

func (u *UISession) lastPendingToolMsgIdx() (int, bool) {
	for i := len(u.Doc.Messages) - 1; i >= 0; i-- {
		msg := u.Doc.Messages[i]
		if msg.Role != config.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.Execution.Status == tools.ResultStatusPending {
				return i, true
			}
		}
		return -1, false
	}
	return -1, false
}

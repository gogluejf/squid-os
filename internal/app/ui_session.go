package app

import (
	"squid-os/internal/chat"
	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/tools"
	"squid-os/internal/ui"
)

type MessageLineRange struct {
	ID    string
	Start int // inclusive viewport line
	End   int // exclusive viewport line
}

// BlockLineRange is a clickable sub-block within a message (thinking, tool call).
type BlockLineRange struct {
	Key   string // ExpandTracker key: msgID + "\x00" + block
	Start int
	End   int
}

// UISession is the TUI wrapper around a pure chat.Session.
type UISession struct {
	*chat.Session
	UIStream            UIStreamState
	renderedMessages    []string
	renderedBlockRanges [][]ui.BlockRange
	renderedWidth       int
	messageRanges       []MessageLineRange
	blockRanges         []BlockLineRange
	undoStack           [][]config.Message
	expand              *ui.ExpandTracker
}

func NewRootUISession(cfg config.SessionConfig, paths config.Paths, catalog runtimeconfig.Catalog) *UISession {
	return &UISession{Session: chat.NewRootSession(cfg, paths, catalog), expand: ui.NewExpandTracker(false)}
}

func LoadRootUISession(sd config.SessionDoc, sourceName string, paths config.Paths, catalog runtimeconfig.Catalog) (*UISession, error) {
	session, err := chat.LoadRootSession(sd, sourceName, paths, catalog)
	if err != nil {
		return nil, err
	}
	return &UISession{Session: session, expand: ui.NewExpandTracker(false)}, nil
}

func (u *UISession) destroyLastSequence() (userText string) {
	n := len(u.Doc.Messages)
	if n == 0 {
		return ""
	}
	for i := n - 1; i >= 0; i-- {
		if u.Doc.Messages[i].Role == config.RoleUser {
			seq := make([]config.Message, n-i)
			copy(seq, u.Doc.Messages[i:])
			u.undoStack = append(u.undoStack, seq)
			userText = u.Doc.Messages[i].Text
			u.TruncateTo(i)
			u.invalidateRenderFrom(i)
			return userText
		}
	}
	return ""
}

func (u *UISession) undoDestroy() (textarea string, ok bool) {
	if len(u.undoStack) == 0 {
		return "", false
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
				return msg.Text, true
			}
		}
	}
	return "", true
}

func (u *UISession) invalidateRenderFrom(i int) {
	if i < len(u.renderedMessages) {
		u.renderedMessages = u.renderedMessages[:i]
	}
	if i < len(u.renderedBlockRanges) {
		u.renderedBlockRanges = u.renderedBlockRanges[:i]
	}
	u.messageRanges = nil
	u.blockRanges = nil
}

func (u *UISession) invalidateRenderAll() {
	u.renderedMessages = nil
	u.renderedBlockRanges = nil
	u.messageRanges = nil
	u.blockRanges = nil
}

// setExpanded sets the global expand default and clears per-block overrides.
func (u *UISession) setExpanded(v bool) {
	u.expand.Global = v
	u.expand.Clear()
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

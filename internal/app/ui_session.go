package app

import (
	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/skills"
	"squid-os/internal/tools"
)

// UISession is the TUI wrapper around a pure chat.Session.
type UISession struct {
	*chat.Session
	UIStream         UIStreamState
	renderedMessages []string
	renderedWidth    int
	undoStack        [][]config.Message
}

func NewUISession(cfg chat.SessionConfig, paths config.Paths) *UISession {
	return &UISession{Session: chat.NewSession(cfg, paths)}
}

func UISessionFromDoc(sd config.SessionDoc) *UISession {
	return &UISession{Session: chat.LoadSession(sd)}
}

func NewUISessionFromSettings(settings config.Settings, paths config.Paths, workingDir string) *UISession {
	return NewUISession(chat.SessionConfig{
		Provider:         settings.Provider,
		Model:            settings.Model,
		Thinking:         settings.Thinking,
		SystemPromptFile: settings.SystemPromptFile,
		Tools:            availableToolNames(),
		Skills:           availableSkillNames(),
		WorkingDir:       workingDir,
		DebugEnabled:     settings.DebugEnabled,
	}, paths)
}

func (u *UISession) LoadFromDoc(sd config.SessionDoc) {
	u.Session = chat.LoadSession(sd)
	u.renderedMessages = nil
	u.renderedWidth = 0
	u.undoStack = nil
	u.UIStream.reset()
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
		u.Doc.Messages = append(u.Doc.Messages, msg)
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
}

func (u *UISession) invalidateRenderAll() { u.renderedMessages = nil }

func (u *UISession) invalidateRenderAt(i int) {
	if i < len(u.renderedMessages) {
		u.renderedMessages[i] = ""
	}
}

func availableToolNames() []string {
	var names []string
	for _, t := range tools.GetTools() {
		names = append(names, t.Name)
	}
	return names
}

func availableSkillNames() []string {
	reg := skills.GetRegistry()
	if reg == nil {
		return nil
	}
	var names []string
	for _, s := range reg.List() {
		names = append(names, s.Name)
	}
	return names
}

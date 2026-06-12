package app

// Mode represents the current UI mode
type Mode int

const (
	ModeChat            Mode = iota // Default: textarea focused
	ModeStreaming                   // Inference active, input disabled
	ModeComponentPicker             // Active component picker (model, skill, session, file, command)
	ModeHelp                        // Help overlay
	ModeSessionSave                 // Save session name input
	ModeHistorySearch               // Reverse search through prompt history
	ModeAuthorize                   // Awaiting user authorization for tool execution
)

func (m Mode) String() string {
	switch m {
	case ModeChat:
		return "chat"
	case ModeStreaming:
		return "streaming"
	case ModeComponentPicker:
		return "component-picker"
	case ModeHelp:
		return "help"
	case ModeSessionSave:
		return "session-save"
	case ModeHistorySearch:
		return "history-search"
	case ModeAuthorize:
		return "authorize"
	default:
		return "unknown"
	}
}

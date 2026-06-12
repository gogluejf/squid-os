package app

// Mode represents the current UI mode
type Mode int

const (
	ModeChat            Mode = iota // Default: textarea focused
	ModeStreaming                   // Inference active, input disabled
	ModeComponent                 // Active component (picker or prompt)
	ModeHelp                        // Help overlay
	ModeHistorySearch               // Reverse search through prompt history
	ModeAuthorize                   // Awaiting user authorization for tool execution
)

func (m Mode) String() string {
	switch m {
	case ModeChat:
		return "chat"
	case ModeStreaming:
		return "streaming"
	case ModeComponent:
		return "component"
	case ModeHelp:
		return "help"
	case ModeHistorySearch:
		return "history-search"
	case ModeAuthorize:
		return "authorize"
	default:
		return "unknown"
	}
}

package app

// Mode represents the current UI mode
type Mode int

const (
	ModeChat          Mode = iota // Default: textarea focused
	ModeStreaming                 // Inference active, input disabled
	ModeComponent                 // Active component (picker, prompt, or question)
	ModeHelp                      // Help overlay
	ModeHistorySearch             // Reverse search through prompt history
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
	default:
		return "unknown"
	}
}

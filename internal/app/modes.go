package app

// Mode represents the current UI mode
type Mode int

const (
	ModeChat          Mode = iota // Default: textarea focused
	ModeStreaming                 // Inference active, input disabled
	ModeModelPicker               // Model selection
	ModeHelp                      // Help overlay
	ModeFilePicker                // File path completion for /image, /system
	ModeSessionPicker             // Session list for /load
	ModeSavePrompt                // Save session name input
	ModeHistorySearch             // Reverse search through prompt history
)

func (m Mode) String() string {
	switch m {
	case ModeChat:
		return "chat"
	case ModeStreaming:
		return "streaming"
	case ModeModelPicker:
		return "model-picker"
	case ModeHelp:
		return "help"
	case ModeFilePicker:
		return "file-picker"
	case ModeSessionPicker:
		return "session-picker"
	case ModeSavePrompt:
		return "save-prompt"
	case ModeHistorySearch:
		return "history-search"
	default:
		return "unknown"
	}
}

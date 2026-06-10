package app

// Mode represents the current UI mode
type Mode int

const (
	ModeChat          Mode = iota // Default: textarea focused
	ModeStreaming                 // Inference active, input disabled
	ModeModelPicker               // Model selection
	ModeSkillPicker               // Skill selection
	ModeHelp                      // Help overlay
	ModeFilePicker                // File path completion for /image, /system
	ModeSessionPicker             // Session list for /load
	ModeSavePrompt                // Save session name input
	ModeHistorySearch             // Reverse search through prompt history
	ModeAuthorize                 // Awaiting user authorization for tool execution
)

func (m Mode) String() string {
	switch m {
	case ModeChat:
		return "chat"
	case ModeStreaming:
		return "streaming"
	case ModeModelPicker:
		return "model-picker"
	case ModeSkillPicker:
		return "skill-picker"
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
	case ModeAuthorize:
		return "authorize"
	default:
		return "unknown"
	}
}

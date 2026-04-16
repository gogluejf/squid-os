package app

import (
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rig-chat/internal/chat"
	"rig-chat/internal/config"
	"rig-chat/internal/ui"
)

// Model is the top-level Bubble Tea model
type Model struct {
	// UI components
	textarea textarea.Model
	viewport viewport.Model
	mode     Mode
	ready    bool
	width    int
	height   int

	// Command palette
	cmdPalette ui.CommandPalette

	// Pickers
	modelEntries   []chat.ModelEntry
	modelPicker    ui.PickerList
	sessionPicker  ui.PickerList
	filePicker     ui.PickerList
	thinkingToggle ui.ThinkingToggle
	savePrompt     ui.SavePrompt
	filePickerFor  string // "image" or "system"

	// Session + messages (bundled)
	session chatSession

	// Stream state (bundled)
	stream streamState

	// Config
	settings  config.Settings
	endpoints config.EndpointsConfig
	paths     config.Paths
	history   config.History

	// Model cache
	modelCache *chat.ModelCache

	// Prompt history navigation
	historyIdx int // -1 = draft, 0..n = browsing history
	draft      string

	// Misc
	attachedImage   string
	lastError       string
	incognito       bool
	sessionSnapshot *sessionSnapshot

	// Global thinking visibility state (NOT persisted)
	thinkingExpanded bool
}

// sessionSnapshot captures live state so session-picker Esc can restore it.
type sessionSnapshot struct {
	session  chatSession
	settings config.Settings
}

// New creates a new app Model. Pass a non-nil initialSession to pre-load a session,
// and incognito=true to start in incognito mode.
func New(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, history config.History, initialSession *config.SessionFile, incognito bool) Model {
	ta := textarea.New()

	ta.ShowLineNumbers = false
	ta.SetHeight(4)
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.CharLimit = 0

	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	ta.BlurredStyle.Base = ta.FocusedStyle.Base

	vp := viewport.New(80, 20)

	var sess chatSession
	if initialSession != nil {
		sess.setFrom(*initialSession)
		// Restore session settings
		settings.Model = initialSession.Session.Model
		settings.Provider = initialSession.Session.Provider
		settings.Thinking = initialSession.Session.Thinking
		if initialSession.Session.SystemPromptFile != "" {
			settings.SystemPromptFile = initialSession.Session.SystemPromptFile
		}
	} else {
		sess.clear(settings.Provider, settings.Model, settings.Thinking, settings.SystemPromptFile)
		// Fresh session — clear LastSessionName so auto-save doesn't overwrite the previous session
		if settings.LastSessionName != "" {
			settings.LastSessionName = ""
			_ = config.SaveSettings(paths, settings)
		}
	}

	return Model{
		textarea:   ta,
		viewport:   vp,
		mode:       ModeChat,
		settings:   settings,
		endpoints:  endpoints,
		paths:      paths,
		history:    history,
		session:    sess,
		historyIdx: -1,
		cmdPalette: ui.NewCommandPalette(),
		modelCache: chat.NewModelCache(5 * time.Minute),
		incognito:  incognito,
	}
}

// Init starts the cursor blink command.
func (m Model) Init() tea.Cmd {
	// Call setChatMode to ensure placeholder and focus are properly initialized
	return (&m).setChatMode()
}

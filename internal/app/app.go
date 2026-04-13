package app

import (
	"context"
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

	// Chat state
	messages       []config.DisplayMessage
	streamText     string
	streamThinking string
	inThinking     bool
	streaming      bool
	cancelFn       context.CancelFunc
	streamCh       <-chan chat.StreamEvent
	streamStart    time.Time
	firstTokenTime time.Time
	tokenCount     int

	// Config
	settings  config.Settings
	endpoints config.EndpointsConfig
	paths     config.Paths
	history   config.History
	session   config.SessionFile

	// Model cache
	modelCache *chat.ModelCache

	// Prompt history navigation
	historyIdx int // -1 = draft, 0..n = browsing history
	draft      string

	// Dimensions
	width  int
	height int

	// Image attachment
	attachedImage string

	// Total tokens across session
	totalTokens int

	// Error display
	lastError string

	// Incognito mode: skip history and session saving
	incognito bool

	// Session picker snapshot — restored on Esc
	sessionSnapshot *sessionState
}

type sessionState struct {
	messages    []config.DisplayMessage
	session     config.SessionFile
	totalTokens int
	settings    config.Settings
}

// New creates a new app Model. Pass a non-nil initialSession to pre-load a session,
// and incognito=true to start in incognito mode.
func New(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, history config.History, initialSession *config.SessionFile, incognito bool) Model {
	ta := textarea.New()

	ta.ShowLineNumbers = false
	ta.SetHeight(4)
	ta.Focus()
	ta.CharLimit = 0

	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	ta.BlurredStyle.Base = ta.FocusedStyle.Base

	vp := viewport.New(80, 20)

	var session config.SessionFile
	var messages []config.DisplayMessage

	var totalTokens int
	if initialSession != nil {
		session = *initialSession
		messages = make([]config.DisplayMessage, len(initialSession.Messages))
		for i, msg := range initialSession.Messages {
			messages[i] = config.DisplayMessage{Message: msg}
		}
		totalTokens = initialSession.TotalTokens
		// Restore session settings
		settings.Model = session.Session.Model
		settings.Provider = session.Session.Provider
		settings.Thinking = session.Session.Thinking
		if session.Session.SystemPromptFile != "" {
			settings.SystemPromptFile = session.Session.SystemPromptFile
		}
	} else {
		session = config.NewSessionFile(settings.Provider, settings.Model, settings.Thinking, settings.SystemPromptFile)
		// Fresh session — clear LastSessionName so auto-save doesn't overwrite the previous session
		if settings.LastSessionName != "" {
			settings.LastSessionName = ""
			_ = config.SaveSettings(paths, settings)
		}
	}

	return Model{
		textarea:    ta,
		viewport:    vp,
		mode:        ModeChat,
		settings:    settings,
		endpoints:   endpoints,
		paths:       paths,
		history:     history,
		session:     session,
		messages:    messages,
		totalTokens: totalTokens,
		historyIdx:  -1,
		cmdPalette:  ui.NewCommandPalette(),
		modelCache:  chat.NewModelCache(5 * time.Minute),
		incognito:   incognito,
	}
}

// Init starts the cursor blink command.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

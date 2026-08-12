package app

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/modelcache"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
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

	// Commands
	allCommands []component.PickerItem

	// History search overlay
	historySearch ui.HistorySearchOverlay

	// Pickers
	modelEntries  []provider.ModelEntry
	pickerPayload interface{}

	// Active component (Picker, Prompt, or Question overlay)
	activeComponent component.Component

	// Session + TUI session state
	session         *UISession
	sessionSnapshot *UISession

	// Config
	settings  config.Settings
	endpoints config.EndpointsConfig
	paths     config.Paths
	history   config.History

	// Prompt history navigation
	historyIdx int // -1 = draft, 0..n = browsing history
	draft      string

	// Capability autocomplete. Escape dismisses the current token's suggestion
	// until its text changes. Selection is transient and tied to one completion.
	completionDismissed string
	completionSelected  int
	completionSelectKey string
	completionWindow    int

	// Misc
	attachedImage string
	notification  ui.Notification
	incognito     bool

	// Global expand/collapse state for thinking and tool results (NOT persisted)
	expanded bool
}

// StartupOptions contains resolved values for an interactive session bootstrap.
type StartupOptions struct {
	Session  runtimeconfig.SessionRequest
	Settings config.Settings
	History  config.History
}

// New creates a new app model from explicit startup options.
func New(options StartupOptions) Model {
	paths := options.Session.Paths
	settings := options.Settings
	endpoints := options.Session.Endpoints
	history := options.History
	initialSession := options.Session.ExistingSession
	runtimeConfig := options.Session.Config

	ta := textarea.New()

	ta.ShowLineNumbers = false
	ta.SetHeight(2)
	ta.MaxHeight = 20
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.CharLimit = 0
	if options.Session.Prompt != "" {
		ta.SetValue(options.Session.Prompt)
	}

	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	ta.BlurredStyle.Base = ta.FocusedStyle.Base

	vp := viewport.New(80, 20)

	var sess *UISession
	var notification ui.Notification
	if initialSession != nil {
		sess = LoadRootUISession(*initialSession, options.Session.SessionName, paths, options.Session.Catalog)
		notification = ui.Notification{
			Level:   ui.NotificationInfo,
			Message: fmt.Sprintf("Auto-load on, last session loaded: %s", sess.SessionDir),
		}
	} else {
		sess = NewRootUISession(runtimeConfig, paths, options.Session.Catalog)
		if settings.LastSessionName != "" {
			settings.LastSessionName = ""
			_ = config.SaveSettings(paths, settings)
		}
	}

	return Model{
		textarea:      ta,
		viewport:      vp,
		mode:          ModeChat,
		settings:      settings,
		endpoints:     endpoints,
		paths:         paths,
		history:       history,
		session:       sess,
		historyIdx:    -1,
		allCommands:   buildCommandPickerItems(),
		historySearch: ui.NewHistorySearchOverlay(nil),
		incognito:     false,
		notification:  notification,
	}
}

func (m *Model) setNotification(level ui.NotificationLevel, msg string) {
	m.notification = ui.Notification{Level: level, Message: msg}
}

func (m *Model) clearNotification() { m.notification = ui.Notification{} }

// setComponent replaces the active overlay component and sets mode.
func (m *Model) setComponent(c component.Component) {
	m.activeComponent = c
	m.mode = ModeComponent
	m.textarea.Blur()
	m.recalcLayout()
}

// Init starts the cursor blink command and refreshes the context window.
func (m Model) Init() tea.Cmd {
	chatMode := (&m).setChatMode()

	var resumePending tea.Cmd
	if msgIdx, ok := m.session.lastPendingToolMsgIdx(); ok {
		resumePending = func() tea.Msg { return pendingToolResumeMsg{msgIdx: msgIdx} }
	}

	return tea.Batch(chatMode, resumePending, (&m).refreshContextCmd())
}

// refreshContextCmd scans models in the background and updates the context
// window for the current model without changing the UI mode.
func (m *Model) refreshContextCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := provider.ScanModels(ctx, m.endpoints)
		_ = (modelcache.Store{Dir: m.paths.CacheDir}).Save(m.endpoints, models)
		return contextRefreshMsg{models: models}
	}
}

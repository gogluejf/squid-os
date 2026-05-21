package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/skills"
	"squid-os/internal/tools"
	"squid-os/internal/ui"
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

	// History search overlay
	historySearch ui.HistorySearchOverlay

	// Pickers
	modelEntries     []chat.ModelEntry
	modelPicker      ui.PickerList
	sessionPicker    ui.PickerList
	sessionPickerRaw []config.SessionInfo // parallel raw names for session picker selection
	filePicker       ui.PickerList
	thinkingToggle   ui.ThinkingToggle
	savePrompt       ui.SavePrompt
	filePickerFor    string // "image" or "system"

	// Session + messages (bundled)
	session chatSession

	// Stream state (bundled)
	stream streamState

	// Tools registry (survives across stream resets)
	toolReg *tools.Registry

	// Config
	settings   config.Settings
	endpoints  config.EndpointsConfig
	paths      config.Paths
	history    config.History
	workingDir string // working directory, defaults to home (used for env + tools)

	// Prompt history navigation
	historyIdx int // -1 = draft, 0..n = browsing history
	draft      string

	// Misc
	attachedImage   string
	notification    ui.Notification
	incognito       bool
	sessionSnapshot *chatSession

	// Global expand/collapse state for thinking and tool results (NOT persisted)
	expanded bool
}

// New creates a new app Model. Pass a non-nil initialSession to pre-load a session,
// and incognito=true to start in incognito mode.
func New(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, history config.History, initialSession *config.SessionFile, incognito bool) Model {
	wd, _ := os.Getwd()

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
	var notification ui.Notification
	if initialSession != nil {
		sess.setFrom(*initialSession)
		// Show friendly notification for auto-load
		notification = ui.Notification{
			Level:   ui.NotificationInfo,
			Message: fmt.Sprintf("Auto-load on, last session loaded: %s", config.SessionPath(paths, settings.LastSessionName)),
		}
	} else {
		sess.clear(settings, paths, wd)
		// Fresh session — clear LastSessionName so auto-save doesn't overwrite the previous session
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
		workingDir:    wd, // starts as current working directory
		session:       sess,
		historyIdx:    -1,
		cmdPalette:    ui.NewCommandPalette(),
		historySearch: ui.NewHistorySearchOverlay(nil),
		incognito:     incognito,
		notification:  notification,
	}
}

func (m *Model) setNotification(level ui.NotificationLevel, msg string) {
	m.notification = ui.Notification{Level: level, Message: msg}
}

func (m *Model) clearNotification() { m.notification = ui.Notification{} }

// Init starts the cursor blink command and refreshes the context window.
func (m Model) Init() tea.Cmd {
	chatMode := (&m).setChatMode()

	// Wire tool callbacks
	tools.SetProjectDir(m.paths.ProjectDir)
	tools.SetWorkingDir(m.workingDir)

	// Initialize skill registry
	var skillCmd tea.Cmd
	if err := skills.InitRegistry(m.paths.Skills); err != nil {
		// Non-fatal: log but don't crash
		skillCmd = func() tea.Msg { return nil }
	}

	return tea.Batch(chatMode, (&m).refreshContextCmd(), skillCmd)
}

// refreshContextCmd scans models in the background and updates the context
// window for the current model without changing the UI mode.
func (m *Model) refreshContextCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := chat.ScanModels(ctx, m.endpoints)
		return contextRefreshMsg{models: models}
	}
}

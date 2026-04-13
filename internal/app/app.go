package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rig-chat/internal/chat"
	"rig-chat/internal/config"
	"rig-chat/internal/ui"
)

// streamEventMsg wraps a StreamEvent for the Bubble Tea message loop
type streamEventMsg chat.StreamEvent

// streamTickMsg fires periodically while streaming to keep the live timer
// in the message header animated even when no tokens are arriving yet.
type streamTickMsg struct{}

func streamTickCmd() tea.Cmd {
	return tea.Tick(20*time.Millisecond, func(_ time.Time) tea.Msg {
		return streamTickMsg{}
	})
}

// modelsLoadedMsg signals that model scanning completed
type modelsLoadedMsg struct {
	models []chat.ModelEntry
}

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

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func waitForStreamEvent(ch <-chan chat.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamEventMsg(chat.StreamEvent{Done: true})
		}
		return streamEventMsg(event)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.recalcLayout()
		m.updateViewportContent()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case streamTickMsg:
		if m.streaming {
			m.updateViewportContent()
			return m, streamTickCmd()
		}
		return m, nil

	case streamEventMsg:
		return m.handleStreamEvent(chat.StreamEvent(msg))

	case modelsLoadedMsg:
		ids := chat.ModelIDs(msg.models)
		m.modelPicker = ui.NewPickerList("Select Model", ids)
		m.mode = ModeModelPicker
		m.recalcLayout()
		return m, nil
	}

	if m.mode == ModeChat {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) recalcLayout() {
	const inputHeight = 6
	const headerHeight = 1
	const footerHeight = 2

	overlayHeight := 0
	if m.cmdPalette.Visible {
		overlayHeight = m.cmdPalette.RenderHeight()
	} else {
		switch m.mode {
		case ModeModelPicker:
			overlayHeight = m.modelPicker.RenderHeight()
		case ModeSessionPicker:
			overlayHeight = m.sessionPicker.RenderHeight()
		case ModeFilePicker:
			overlayHeight = m.filePicker.RenderHeight()
		case ModeSavePrompt:
			overlayHeight = 2 // heading + name input line
		}
	}

	attachHeight := 0
	if m.attachedImage != "" {
		attachHeight = 1
	}
	vpHeight := m.height - inputHeight - headerHeight - footerHeight - attachHeight - overlayHeight
	if vpHeight < 3 {
		vpHeight = 3
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
	m.textarea.SetWidth(m.width)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {

	case ModeChat:
		return m.handleChatKey(msg)

	case ModeStreaming:
		return m.handleStreamingKey(msg)

	case ModeHelp:
		if key.Matches(msg, keys.Help) || key.Matches(msg, keys.Cancel) || key.Matches(msg, keys.Escape) {
			m.mode = ModeChat
			m.textarea.Focus()
			return m, nil
		}

	case ModeModelPicker:
		return m.handlePickerKey(msg, "model")

	case ModeSessionPicker:
		return m.handlePickerKey(msg, "session")

	case ModeFilePicker:
		return m.handlePickerKey(msg, m.filePickerFor)

	case ModeSavePrompt:
		return m.handleSavePromptKey(msg)
	}

	return m, nil
}

func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		_ = config.SaveHistory(m.paths, m.history)
		return m, tea.Quit

	case key.Matches(msg, keys.Escape):
		if m.cmdPalette.Visible {
			m.cmdPalette.Visible = false
			m.recalcLayout()
		}
		return m, nil

	case key.Matches(msg, keys.Help):
		m.mode = ModeHelp
		return m, nil

	case key.Matches(msg, keys.ExpandThinking):
		m.toggleLastThinking()
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.Save):
		return m.startManualSave()

	case key.Matches(msg, keys.Load):
		return m.startLoad()

	case key.Matches(msg, keys.Model):
		return m, m.scanModelsCmd()

	case key.Matches(msg, keys.NewSession):
		return m.clearSession()

	case key.Matches(msg, keys.Incognito):
		return m.toggleIncognito()

	case msg.Alt && msg.Type == tea.KeyEnter:
		m.textarea.InsertRune('\n')
		return m, nil

	case key.Matches(msg, keys.Send):
		if m.cmdPalette.Visible && m.cmdPalette.SelectedCommand() != "" {
			return m.executeCommand(m.cmdPalette.SelectedCommand())
		}
		return m.sendMessage()

	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return m, nil

	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return m, nil

	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.cmdPalette.Visible {
			m.cmdPalette.MoveUp()
			return m, nil
		}
		return m.historyUp()

	case key.Matches(msg, keys.Down):
		if m.cmdPalette.Visible {
			m.cmdPalette.MoveDown()
			return m, nil
		}
		return m.historyDown()

	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCommandPalette()
		return m, cmd
	}
}

// updateCommandPalette re-evaluates whether the command palette should be
// shown based on the current textarea content.
func (m *Model) updateCommandPalette() {
	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") {
		filter := val[1:]
		if filter != m.cmdPalette.Filter {
			m.cmdPalette.Filter = filter
			m.cmdPalette.Selected = 0
		}
		if len(m.cmdPalette.FilteredItems()) > 0 {
			m.cmdPalette.Visible = true
		} else {
			m.cmdPalette.Visible = false
		}
	} else {
		m.cmdPalette.Visible = false
	}
	m.recalcLayout()
}

func (m Model) handleStreamingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		if m.cancelFn != nil {
			m.cancelFn()
		}
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "user" {
			m.messages = m.messages[:len(m.messages)-1]
		}
		m.streaming = false
		m.tokenCount = 0 // discard partial stream; don't pollute footer total
		m.mode = ModeChat
		m.textarea.Focus()
		m.streamText = ""
		m.streamThinking = ""
		m.updateViewportContent()
		return m, nil

	case key.Matches(msg, keys.ExpandThinking):
		// Could toggle live thinking visibility in the future
		return m, nil

	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return m, nil

	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return m, nil

	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return m, nil
	}
	return m, nil
}

func (m Model) handlePickerKey(msg tea.KeyMsg, pickerType string) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		if pickerType == "session" && m.sessionSnapshot != nil {
			snap := m.sessionSnapshot
			m.messages = snap.messages
			m.session = snap.session
			m.totalTokens = snap.totalTokens
			m.settings = snap.settings
			m.sessionSnapshot = nil
			m.updateViewportContent()
		}
		m.mode = ModeChat
		m.textarea.Focus()
		(&m).recalcLayout()
		return m, nil

	case key.Matches(msg, keys.ScrollUp):
		m.viewport.ScrollUp(3)
		return m, nil

	case key.Matches(msg, keys.ScrollDown):
		m.viewport.ScrollDown(3)
		return m, nil

	case key.Matches(msg, keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, keys.PageDown):
		m.viewport.PageDown()
		return m, nil

	case key.Matches(msg, keys.Up):
		switch pickerType {
		case "model":
			m.modelPicker.MoveUp()
		case "session":
			m.sessionPicker.MoveUp()
			m = m.previewSession(m.sessionPicker.SelectedItem())
		case "image", "system":
			m.filePicker.MoveUp()
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		switch pickerType {
		case "model":
			m.modelPicker.MoveDown()
		case "session":
			m.sessionPicker.MoveDown()
			m = m.previewSession(m.sessionPicker.SelectedItem())
		case "image", "system":
			m.filePicker.MoveDown()
		}
		return m, nil

	case key.Matches(msg, keys.Send):
		return m.confirmPicker(pickerType)

	case key.Matches(msg, keys.Tab):
		switch pickerType {
		case "model":
			m.modelPicker.MoveDown()
		case "session":
			m.sessionPicker.MoveDown()
			m = m.previewSession(m.sessionPicker.SelectedItem())
		case "image", "system":
			m.filePicker.MoveDown()
		}
		return m, nil

	default:
		// Type to filter
		s := msg.String()
		switch pickerType {
		case "model":
			if len(s) == 1 {
				m.modelPicker.Filter += s
				m.modelPicker.Selected = 0
			} else if s == "backspace" && len(m.modelPicker.Filter) > 0 {
				m.modelPicker.Filter = m.modelPicker.Filter[:len(m.modelPicker.Filter)-1]
				m.modelPicker.Selected = 0
			}
		case "session":
			if len(s) == 1 {
				m.sessionPicker.Filter += s
				m.sessionPicker.Selected = 0
			} else if s == "backspace" && len(m.sessionPicker.Filter) > 0 {
				m.sessionPicker.Filter = m.sessionPicker.Filter[:len(m.sessionPicker.Filter)-1]
				m.sessionPicker.Selected = 0
			}
		case "image", "system":
			if len(s) == 1 {
				m.filePicker.Filter += s
				m.filePicker.Selected = 0
			} else if s == "backspace" && len(m.filePicker.Filter) > 0 {
				m.filePicker.Filter = m.filePicker.Filter[:len(m.filePicker.Filter)-1]
				m.filePicker.Selected = 0
			}
		}
		(&m).recalcLayout()
		return m, nil
	}
}

func (m Model) confirmPicker(pickerType string) (tea.Model, tea.Cmd) {
	switch pickerType {
	case "model":
		selected := m.modelPicker.SelectedItem()
		if selected != "" {
			m.settings.Model = selected
			// Find the provider for this model
			if models, ok := m.modelCache.Get(); ok {
				for _, me := range models {
					if me.ID == selected {
						m.settings.Provider = me.Provider
						break
					}
				}
			}
			_ = config.SaveSettings(m.paths, m.settings)
		}

	case "session":
		// Session is already previewed; just persist the selection and clear snapshot
		selected := m.sessionPicker.SelectedItem()
		if selected != "" {
			m.settings.Model = m.session.Session.Model
			m.settings.Provider = m.session.Session.Provider
			m.settings.Thinking = m.session.Session.Thinking
			if m.incognito != true {
				m.settings.LastSessionName = selected
				_ = config.SaveSettings(m.paths, m.settings)
			}
		}
		m.sessionSnapshot = nil

	case "image":
		selected := m.filePicker.SelectedItem()
		if selected != "" {
			m.attachedImage = selected
			m.recalcLayout()
		}

	case "system":
		selected := m.filePicker.SelectedItem()
		if selected != "" {
			m.settings.SystemPromptFile = selected
			_ = config.SaveSettings(m.paths, m.settings)
		}
	}

	m.mode = ModeChat
	m.textarea.Focus()
	(&m).recalcLayout()
	return m, nil
}

func (m Model) executeCommand(name string) (tea.Model, tea.Cmd) {
	m.cmdPalette.Reset()
	m.textarea.SetValue("")

	switch name {
	case "exit":
		_ = config.SaveHistory(m.paths, m.history)
		return m, tea.Quit

	case "help":
		m.mode = ModeHelp
		return m, nil

	case "model":
		// Scan models asynchronously
		m.mode = ModeChat // temporarily back to chat while loading
		return m, m.scanModelsCmd()

	case "thinking":
		m.thinkingToggle = ui.NewThinkingToggle(m.settings.Thinking)
		m.mode = ModeChat
		// Simple toggle for now
		m.settings.Thinking = !m.settings.Thinking
		_ = config.SaveSettings(m.paths, m.settings)
		m.textarea.Focus()
		return m, nil

	case "image":
		// List image files — for now just let user type a path
		m.filePicker = ui.NewPickerList("Attach Image (type path)", []string{})
		m.filePickerFor = "image"
		m.mode = ModeFilePicker
		(&m).recalcLayout()
		return m, nil

	case "save":
		return m.startManualSave()

	case "load":
		return m.startLoad()

	case "clear":
		return m.clearSession()

	case "system":
		prompts := config.ListSystemPrompts(m.paths)
		m.filePicker = ui.NewPickerList("System Prompt", prompts)
		m.filePickerFor = "system"
		m.mode = ModeFilePicker
		(&m).recalcLayout()
		return m, nil
	}

	m.mode = ModeChat
	m.textarea.Focus()
	return m, nil
}

// startManualSave opens the save prompt for the user to confirm/edit the session name.
func (m Model) startManualSave() (Model, tea.Cmd) {
	if m.incognito {
		return m, nil // no saving in incognito
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	m.savePrompt = ui.NewSavePrompt(name)
	m.mode = ModeSavePrompt
	m.textarea.Blur()
	(&m).recalcLayout()
	return m, nil
}

// saveAs saves the session under the given name and updates LastSessionName.
func (m Model) saveAs(name string) (Model, tea.Cmd) {
	if name == "" || m.incognito {
		return m, nil
	}
	m.session.Messages = m.extractSessionMessages()
	m.session.TotalTokens = m.totalTokens
	err := config.SaveSession(m.paths, name, m.session)
	if err != nil {
		m.lastError = fmt.Sprintf("save: %v", err)
	} else {
		m.settings.LastSessionName = name
		_ = config.SaveSettings(m.paths, m.settings)
		m.lastError = ""
	}
	return m, nil
}

// autoSave saves silently if AutoSave is enabled and we have a session name.
func (m Model) autoSave() (Model, tea.Cmd) {
	if !m.settings.AutoSave || m.incognito {
		return m, nil
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	return m.saveAs(name)
}

// handleSavePromptKey handles key input in ModeSavePrompt.
func (m Model) handleSavePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		m.mode = ModeChat
		m.textarea.Focus()
		(&m).recalcLayout()
		return m, nil

	case key.Matches(msg, keys.Send):
		nm, cmd := m.saveAs(m.savePrompt.Name)
		nm.mode = ModeChat
		nm.textarea.Focus()
		(&nm).recalcLayout()
		return nm, cmd

	default:
		s := msg.String()
		if s == "backspace" {
			if len(m.savePrompt.Name) > 0 {
				m.savePrompt.Name = m.savePrompt.Name[:len(m.savePrompt.Name)-1]
			}
		} else if len(s) == 1 {
			m.savePrompt.Name += s
		}
		return m, nil
	}
}

// clearSession resets all messages and session state to start fresh.
func (m Model) clearSession() (Model, tea.Cmd) {
	m.messages = nil
	m.session = config.NewSessionFile(m.settings.Provider, m.settings.Model, m.settings.Thinking, m.settings.SystemPromptFile)
	if !m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	m.totalTokens = 0
	m.lastError = ""
	m.updateViewportContent()
	m.mode = ModeChat
	m.textarea.Focus()
	return m, nil
}

// toggleIncognito switches incognito mode on/off and clears the chat.
func (m Model) toggleIncognito() (Model, tea.Cmd) {
	m.incognito = !m.incognito
	// Clear messages and session on toggle (fresh start both ways)
	m.messages = nil
	m.session = config.NewSessionFile(m.settings.Provider, m.settings.Model, m.settings.Thinking, m.settings.SystemPromptFile)
	if !m.incognito {
		// Leaving incognito: also reset last session name so auto-save doesn't
		// accidentally write to the previous session.
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	m.totalTokens = 0
	m.lastError = ""
	m.updateViewportContent()
	m.mode = ModeChat
	m.textarea.Focus()
	return m, nil
}

func (m Model) startLoad() (Model, tea.Cmd) {
	sessions := config.ListSessions(m.paths)
	if len(sessions) == 0 {
		return m, nil
	}

	// Snapshot current state so Esc can restore it
	snap := &sessionState{
		messages:    m.messages,
		session:     m.session,
		totalTokens: m.totalTokens,
		settings:    m.settings,
	}
	m.sessionSnapshot = snap

	picker := ui.NewPickerList("Load Session", sessions)

	// Pre-select LastSessionName if it exists in the list
	if m.settings.LastSessionName != "" {
		for i, s := range sessions {
			if s == m.settings.LastSessionName {
				picker.Selected = i
				break
			}
		}
	}

	m.sessionPicker = picker
	m.mode = ModeSessionPicker
	(&m).recalcLayout()

	// Preview the initially selected session immediately
	m = m.previewSession(m.sessionPicker.SelectedItem())
	return m, nil
}

// previewSession loads a session's messages into view without persisting anything.
func (m Model) previewSession(name string) Model {
	if name == "" {
		return m
	}
	sf, err := config.LoadSession(m.paths, name)
	if err != nil {
		return m
	}
	msgs := make([]config.DisplayMessage, len(sf.Messages))
	for i, msg := range sf.Messages {
		msgs[i] = config.DisplayMessage{Message: msg}
	}
	m.messages = msgs
	m.session = sf
	m.totalTokens = sf.TotalTokens
	m.updateViewportContent()
	return m
}

func (m Model) scanModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := chat.ScanModels(ctx, m.endpoints, m.modelCache)
		return modelsLoadedMsg{models: models}
	}
}

func (m Model) sendMessage() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}

	if !m.incognito {
		config.AddHistoryEntry(&m.history, text, m.settings.MaxHistory)
		_ = config.SaveHistory(m.paths, m.history)
	}
	m.historyIdx = -1
	m.draft = ""

	userMsg := config.DisplayMessage{
		Message: config.Message{
			ID:          fmt.Sprintf("msg_%d", len(m.messages)+1),
			Role:        "user",
			CreatedAt:   time.Now(),
			Text:        text,
			ImagePath:   m.attachedImage,
			InputTokens: countTokensApprox(text),
		},
	}
	m.messages = append(m.messages, userMsg)

	m.textarea.SetValue("")
	m.textarea.Blur()

	apiMsgs := m.buildAPIMessages()
	m.attachedImage = ""

	m.streaming = true
	m.mode = ModeStreaming
	m.textarea.Placeholder = "ctrl+c to cancel..."
	m.streamText = ""
	m.streamThinking = ""
	m.inThinking = false
	m.tokenCount = 0
	m.streamStart = time.Now()
	m.firstTokenTime = time.Time{}
	m.lastError = ""

	chatURL := config.ResolveChatURL(m.endpoints, m.settings.Provider)

	engine := chat.NewEngine(chatURL, m.settings.Model, m.settings.Thinking)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFn = cancel

	ch := engine.Stream(ctx, apiMsgs)
	m.streamCh = ch

	m.updateViewportContent()
	return m, tea.Batch(waitForStreamEvent(ch), streamTickCmd())
}

func (m Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd) {
	if event.Error != nil {
		m.lastError = event.Error.Error()
		m.streaming = false
		m.mode = ModeChat
		m.textarea.Placeholder = "Type a message..."
		m.textarea.Focus()
		m.recalcLayout()
		m.updateViewportContent()
		return m, nil
	}

	if event.Done {
		assistantMsg := config.DisplayMessage{
			Message: config.Message{
				ID:              fmt.Sprintf("msg_%d", len(m.messages)+1),
				Role:            "assistant",
				CreatedAt:       m.streamStart,
				Text:            m.streamText,
				ThinkingText:    m.streamThinking,
				OutputTokens:    m.tokenCount,
				TokensPerSecond: m.calcTokPerSec(),
				ResponseTimeMs:  time.Since(m.streamStart).Milliseconds(),
				StopReason:      event.StopReason,
			},
		}
		m.messages = append(m.messages, assistantMsg)
		m.session.Messages = m.extractSessionMessages()
		m.totalTokens += m.tokenCount
		m.tokenCount = 0 // flush so footer (totalTokens + tokenCount) doesn't double-count
		m.streaming = false
		m.mode = ModeChat
		m.textarea.Placeholder = "Type a message..."
		m.textarea.Focus()
		m.recalcLayout()
		m.updateViewportContent()
		nm, cmd := m.autoSave()
		return nm, cmd
	}

	if event.Text != "" {
		m.streamText += event.Text
		m.tokenCount += countTokensApprox(event.Text)
		if m.firstTokenTime.IsZero() {
			m.firstTokenTime = time.Now()
		}
	}
	if event.Thinking != "" {
		m.streamThinking += event.Thinking
		m.tokenCount += countTokensApprox(event.Thinking)
		if m.firstTokenTime.IsZero() {
			m.firstTokenTime = time.Now()
		}
	}
	m.inThinking = event.InThinking
	m.updateViewportContent()
	return m, waitForStreamEvent(m.streamCh)
}

func (m *Model) updateViewportContent() {
	var b strings.Builder

	for _, msg := range m.messages {
		b.WriteString(ui.RenderMessage(msg, m.width, msg.ThinkingExpanded))
	}

	if m.streaming {
		b.WriteString(ui.RenderStreamingMessage(
			m.streamText,
			m.streamThinking,
			m.inThinking,
			m.width,
			m.streamStart,
			m.tokenCount,
			m.calcTokPerSec(),
		))
	}

	if m.lastError != "" {
		b.WriteString(ui.ErrorStyle.Render("Error: " + m.lastError))
		b.WriteString("\n")
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.mode == ModeHelp {
		return m.renderHelp()
	}

	var sections []string
	sections = append(sections, m.renderTopHeader())

	// Viewport (messages)
	sections = append(sections, m.viewport.View())

	// Command palette overlay (between viewport and input)
	if m.cmdPalette.Visible {
		sections = append(sections, m.cmdPalette.Render(m.width))
	} else {
		switch m.mode {
		case ModeModelPicker:
			sections = append(sections, m.modelPicker.Render(m.width))
		case ModeSessionPicker:
			sections = append(sections, m.sessionPicker.Render(m.width))
		case ModeFilePicker:
			sections = append(sections, m.filePicker.Render(m.width))
		case ModeSavePrompt:
			sections = append(sections, m.savePrompt.Render(m.width))
		}
	}

	// Attachment chip
	if m.attachedImage != "" {
		sections = append(sections, ui.AttachmentStyle.Render("  attached: "+m.attachedImage))
	}

	// Textarea
	sections = append(sections, m.textarea.View())

	// Footer
	footerData := ui.FooterData{
		Model:       m.settings.Model,
		Provider:    m.settings.Provider,
		TotalTokens: m.totalTokens + m.tokenCount,
		Streaming:   m.streaming,
		InThinking:  m.inThinking,
		TokPerSec:   m.calcTokPerSec(),
	}
	sections = append(sections, ui.RenderFooter(footerData, m.width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) toggleLastThinking() {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" && m.messages[i].ThinkingText != "" {
			m.messages[i].ThinkingExpanded = !m.messages[i].ThinkingExpanded
			break
		}
	}
}

func (m Model) historyUp() (Model, tea.Cmd) {
	if len(m.history.Entries) == 0 {
		return m, nil
	}
	if m.historyIdx == -1 {
		m.draft = m.textarea.Value()
		m.historyIdx = len(m.history.Entries) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	}
	m.textarea.SetValue(m.history.Entries[m.historyIdx])
	return m, nil
}

func (m Model) historyDown() (Model, tea.Cmd) {
	if m.historyIdx == -1 {
		return m, nil
	}
	if m.historyIdx < len(m.history.Entries)-1 {
		m.historyIdx++
		m.textarea.SetValue(m.history.Entries[m.historyIdx])
	} else {
		m.historyIdx = -1
		m.textarea.SetValue(m.draft)
	}
	return m, nil
}

func (m Model) buildAPIMessages() []chat.ChatMessage {
	var msgs []chat.ChatMessage

	sysPrompt := config.LoadSystemPrompt(m.paths, m.settings.SystemPromptFile)
	msgs = append(msgs, chat.ChatMessage{Role: "system", Content: sysPrompt})

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			if msg.ImagePath != "" {
				parts, err := chat.BuildMultimodalContent(msg.Text, msg.ImagePath)
				if err == nil {
					msgs = append(msgs, chat.ChatMessage{Role: "user", Content: parts})
				} else {
					msgs = append(msgs, chat.ChatMessage{Role: "user", Content: msg.Text})
				}
			} else {
				msgs = append(msgs, chat.ChatMessage{Role: "user", Content: msg.Text})
			}
		case "assistant":
			msgs = append(msgs, chat.ChatMessage{Role: msg.Role, Content: msg.Text})
		}
	}

	return msgs
}

func (m Model) extractSessionMessages() []config.Message {
	msgs := make([]config.Message, len(m.messages))
	for i, dm := range m.messages {
		msgs[i] = dm.Message
	}
	return msgs
}

func (m Model) calcTokPerSec() float64 {
	if m.firstTokenTime.IsZero() || m.tokenCount == 0 {
		return 0
	}
	elapsed := time.Since(m.firstTokenTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(m.tokenCount) / elapsed
}

func countTokensApprox(s string) int {
	n := len(s) / 4
	if n == 0 && len(s) > 0 {
		n = 1
	}
	return n
}

// SetAttachedImage sets the image to attach to the next message (from --image flag)
func (m *Model) SetAttachedImage(path string) {
	m.attachedImage = path
}

// SetInitialPrompt sets the textarea content (from --prompt flag)
func (m *Model) SetInitialPrompt(text string) {
	m.textarea.SetValue(text)
}

func (m Model) renderHelp() string {
	return ui.RenderHelp(m.width, m.height)
}

// renderTopHeader renders the top header bar, with incognito indicator if active.
func (m Model) renderTopHeader() string {
	if !m.incognito {
		return ui.TopHeaderStyle.Width(m.width).Render("rig-chat v0.1")
	}
	headerStyle := ui.IncognitoHeaderStyle.Width(m.width)
	title := "rig-chat v0.1"
	label := "👻 incognito"
	titleWidth := lipgloss.Width(ui.IncognitoHeaderStyle.Render(title))
	labelWidth := lipgloss.Width(ui.IncognitoHeaderStyle.Render(label))
	gap := m.width - titleWidth - labelWidth
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Render(title + strings.Repeat(" ", gap) + label)
}

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
	"squid-os/internal/voice"
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

	// Kill ring (GNU readline): text removed by kill bindings (ctrl+k, ctrl+u,
	// ctrl+w, alt+d) is stored here so ctrl+y can yank it back. Last kill wins.
	killBuffer string

	// Capability autocomplete. Escape dismisses the current token's suggestion
	// until its text changes. Selection is transient and tied to one completion.
	completionDismissed string
	completionSelected  int
	completionSelectKey string
	completionWindow    int

	// Misc
	notification ui.Notification
	incognito    bool

	// voiceOn mirrors settings.Voice.Enabled at runtime; task 1.5 adds the
	// live toggle (alt+v / /voice) that flips it without writing settings.
	voiceOn bool

	// Queued input (design: queued-input). Single admission entry point for
	// keyboard and (later) socket transports; owns durable receipts.
	submissions *SubmissionCoordinator

	// Narrator wires spoken output into the assistant stream (voice mode,
	// task 1.4). Group lifecycle is tied to the message, not the turn ctx.
	narrator *Narrator

	// voiceSpeaking drives the "🔊 speaking…" indicator in the status row
	// (task 1.6). Set by the coordinator's speaking hook on the first chunk
	// start; cleared on player exit or kill. Written only from the Bubble
	// Tea update goroutine (see newVoiceCoordinator), so no extra lock.
	voiceSpeaking bool

	// turnActive is true from startStream until the turn fully ends (setChatMode).
	// Covers the gap between "tool batch done" and "first token of next step"
	// where no stream/tool channel is active yet but a turn is still in flight.
	// Submissions arriving in that window must queue, not deliver immediately.
	turnActive bool
}

// StartupOptions contains resolved values for an interactive session bootstrap.
type StartupOptions struct {
	Session  runtimeconfig.SessionRequest
	Settings config.Settings
	History  config.History
}

// New creates a new app model from explicit startup options.
func New(options StartupOptions) (Model, error) {
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
		var err error
		sess, err = LoadRootUISession(*initialSession, options.Session.SessionName, paths, options.Session.Catalog)
		if err != nil {
			return Model{}, err
		}
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

	m := Model{
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
		voiceOn:       settings.Voice.Enabled,
		notification:  notification,
		submissions:   newModelSubmissionCoordinator(paths, sess, &incognitoFlag{m: nil}),
	}
	m.submissions.flag.m = &m
	// The voice coordinator is built lazily in WireVoiceSpeaking, once the
	// tea program exists (the speaking hook needs it). Until then the
	// narrator is inert: every Narrator method no-ops on a nil coord.
	m.narrator = newNarrator(nil)
	return m, nil
}

// voiceSpeakingMsg is posted by the coordinator's speaking hook (from a
// background goroutine) into the Bubble Tea loop. The indicator state is
// applied only inside Update, so Model writes stay single-goroutine.
type voiceSpeakingMsg struct{ on bool }

// WireVoiceSpeaking builds the voice coordinator now that the tea program
// exists (app.New builds the model before the program does) and installs the
// speaking-indicator hook. The narrator starts inert (nil coord) until this
// call, so no throwaway coordinator is built in New. Safe to call once; a
// no-op when voice is unavailable or already wired.
func (m *Model) WireVoiceSpeaking(p *tea.Program) {
	if m.narrator == nil || p == nil {
		return
	}
	m.narrator.coord = newVoiceCoordinator(m.settings.Voice, p)
}

// newVoiceCoordinator builds the voice coordinator from settings. A missing
// TTS CLI or server is not fatal: it returns nil and the narrator degrades to
// an inert no-op rather than blocking startup (voice is opt-in and
// best-effort). The speaking hook posts a tea.Msg into the Bubble Tea loop so
// the status-row indicator (task 1.6) updates on first chunk start / player
// exit / kill — never by writing Model state from a background goroutine.
func newVoiceCoordinator(vcfg config.Voice, p *tea.Program) *voice.Coordinator {
	sp, err := voice.NewSpeaker(voice.SpeakerOptions{})
	if err != nil {
		return nil
	}
	c := voice.NewCoordinator(sp, &vcfg)
	if p != nil {
		c.SetSpeakingHook(func(on bool) {
			// Safe to call from any goroutine, including inside Bubble Tea's
			// Update: program.Send only pushes onto an internal channel and
			// never blocks on the event loop (it is not a synchronous
			// round-trip into Update), so posting voiceSpeakingMsg here
			// cannot deadlock even when the hook fires from Emit/CancelGroup
			// paths that run inside Update.
			p.Send(voiceSpeakingMsg{on: on})
		})
	}
	return c
}

// incognitoFlag adapts the model's live incognito flag to the coordinator's
// lazy probe (the coordinator is built before the Model value exists).
type incognitoFlag struct {
	m *Model
}

func (f *incognitoFlag) get() bool { return f.m != nil && f.m.incognito }

// newModelSubmissionCoordinator creates queue storage; admission receives the
// current session from the UI loop rather than retaining a Model pointer.
func newModelSubmissionCoordinator(paths config.Paths, sess *UISession, flag *incognitoFlag) *SubmissionCoordinator {
	c := NewSubmissionCoordinator(paths.Root, flag.get)
	c.flag = flag
	if sess != nil && sess.Session != nil {
		id := sess.Doc.Identity.ID
		if id != "" {
			_ = c.LoadSession(id)
		}
	}
	return c
}

func (m *Model) setNotification(level ui.NotificationLevel, msg string) {
	m.notification = ui.Notification{Level: level, Message: msg}
}

func (m *Model) clearNotification() { m.notification = ui.Notification{} }

// quitCmd returns a command that cleans up the session workspace (including
// incognito temp directories) and then quits the TUI.
func (m *Model) quitCmd() tea.Msg {
	// Voice: shutdown kills any in-flight speech group (process groups die
	// with us, but cancel gives a clean kill + state teardown).
	m.narrator.cancelActive()
	if m.session != nil {
		_ = m.session.CleanupWorkspace()
	}
	m.closeSubmissions()
	return tea.Quit()
}

// setComponent replaces the active overlay component and sets mode.
func (m *Model) setComponent(c component.Component) {
	m.activeComponent = c
	m.mode = ModeComponent
	m.textarea.Blur()
	m.recalcLayout()
}

// closeSubmissions stops the queue persistence worker at shutdown.
func (m *Model) closeSubmissions() {
	if m.submissions != nil {
		m.submissions.Close()
	}
}

// Init starts the cursor blink command, the UI tick, and refreshes the context window.
func (m Model) Init() tea.Cmd {
	chatMode := (&m).setChatMode()

	var resumePending tea.Cmd
	if msgIdx, ok := m.session.lastPendingToolMsgIdx(); ok {
		resumePending = func() tea.Msg { return pendingToolResumeMsg{msgIdx: msgIdx} }
	}

	return tea.Batch(chatMode, resumePending, (&m).refreshContextCmd(), uiTickCmd(false))
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

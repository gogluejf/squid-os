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
	"squid-os/internal/log"
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

	// Queued input (design: queued-input). Single admission entry point for
	// keyboard and (later) socket transports; owns durable receipts.
	submissions *SubmissionCoordinator

	// Narrator wires spoken output into the assistant stream (voice mode,
	// task 1.4). Group lifecycle is tied to the message, not the turn ctx.
	narrator *Narrator

	// langStore tracks the last detected user language asynchronously
	// (task 3.1); submissions bump its newest-ID at admission time.
	langStore *voice.LangStore

	// langSeq counts user-message submissions for async detection IDs;
	// written only on the UI goroutine (admission paths), no lock needed.
	langSeq int64

	// langPush is the program.Send used by fireUserLangDetect to post results
	// into the Bubble Tea loop. It lives in voiceWiring (a shared pointer) so
	// the value copied into tea.NewProgram sees the wiring installed by
	// WireVoiceSpeaking on the pre-program copy — a plain Model field would be
	// lost in that copy and fireUserLangDetect would no-op forever in
	// production (finding #1). Set in WireVoiceSpeaking (nil = no-op).

	// voiceSpeaking drives the "🔊 speaking…" indicator in the status row
	// (task 1.6). Set by the coordinator's speaking hook on the first chunk
	// start; cleared on player exit or kill. Written only from the Bubble
	// Tea update goroutine (see newVoiceCoordinator), so no extra lock.
	voiceSpeaking bool

	// lastUserLang is the last detected user-message language ("en"/"fr"),
	// driving bilingual alert lead-ins (task 3.1). Detection runs
	// asynchronously at submission time; the background goroutine posts
	// langDetectedMsg via program.Send, so this field is written ONLY
	// inside Update — same rule as voiceSpeaking.
	lastUserLang string

	// turnActive is true from startStream until the turn fully ends (setChatMode).
	// Covers the gap between "tool batch done" and "first token of next step"
	// where no stream/tool channel is active yet but a turn is still in flight.
	// Submissions arriving in that window must queue, not deliver immediately.
	turnActive bool

	// voiceWiring holds program-side wiring installed by WireVoiceSpeaking
	// AFTER tea.NewProgram copied the Model. The pointer is shared across all
	// copies (Bubble Tea runs the copy; Update returns fresh copies each
	// frame), so langPush set on the pre-program model is visible to the live
	// loop. Finding #1: a plain value field would be lost in the copy.
	voiceWiring *voiceWiringState
}

// voiceWiringState is the mutable, program-side voice wiring shared by
// reference across every Model copy. Written once by WireVoiceSpeaking
// (pre-Run, single goroutine) and read-only afterwards — no lock needed.
type voiceWiringState struct {
	langPush func(tea.Msg)
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
		notification:  notification,
		submissions:   newModelSubmissionCoordinator(paths, sess, &incognitoFlag{m: nil}),
	}
	m.submissions.flag.m = &m
	// The voice coordinator is built lazily in WireVoiceSpeaking, once the
	// tea program exists (the speaking hook needs it). Until then the
	// narrator is inert: every Narrator method no-ops on a nil coord.
	m.narrator = newNarrator(nil)
	// Async user-language detection (task 3.1): always present so any
	// submission path can bump the newest-ID; fireUserLangDetect is a no-op
	// until a speaker + program exist. The wiring state is allocated here so
	// every Model copy shares the same pointer (finding #1).
	m.langStore = voice.NewLangStore()
	m.voiceWiring = &voiceWiringState{}
	return m, nil
}

// voiceSpeakingMsg is posted by the coordinator's speaking hook (from a
// background goroutine) into the Bubble Tea loop. The indicator state is
// applied only inside Update, so Model writes stay single-goroutine.
type voiceSpeakingMsg struct{ on bool }

// langDetectedMsg carries an async user-language detection result from the
// DetectLang goroutine into the Bubble Tea loop. It is posted via
// program.Send and applied only inside Update (m.lastUserLang), keeping all
// Model writes on the update goroutine. subID identifies which submission
// fired this detection; Update applies it only while that submission is still
// the newest (finding #7: a stale snapshot must never overwrite a newer
// language).
type langDetectedMsg struct {
	subID int64
	lang  string
}

// fireUserLangDetect bumps the lang store's newest-submission ID and fires
// one /detect round-trip in a background goroutine with a bounded deadline.
// The bump happens synchronously on the UI goroutine at admission time, so a
// later submission always supersedes an earlier one regardless of when the
// HTTP round-trips resolve. The result is posted back through program.Send as
// langDetectedMsg; failures/timeouts post the unchanged store value.
// No-op when voice or the program wiring is unavailable.
func (m *Model) fireUserLangDetect(text string) {
	// Voice off → nothing will ever speak, so detecting the user's language
	// is pure waste: skip it. The last known value stays intact; flipping
	// /voice on re-detects from the next message.
	if !m.incognito {
		log.LogVoice("submission", "voice_enabled=%t text=%q", m.settings.Voice.VoiceEnabled(), text)
	}
	if !m.settings.Voice.VoiceEnabled() || m.langStore == nil || m.narrator == nil || m.narrator.coord == nil || m.voiceWiring == nil || m.voiceWiring.langPush == nil {
		log.LogVoice("detect.skipped", "voice_enabled=%t reason=disabled_or_unwired", m.settings.Voice.VoiceEnabled())
		return
	}
	sp := m.narrator.coord.Speaker()
	m.langSeq++
	subID := m.langSeq
	m.langStore.Submit(subID)
	go func() {
		voice.DetectLang(sp, text, subID, m.langStore)
		// program.Send may block on Tea's unbuffered event channel. Keep it
		// on this worker, never on the UI goroutine; program exit releases it.
		m.voiceWiring.langPush(langDetectedMsg{subID: subID, lang: m.langStore.Get()})
	}()
}

// WireVoiceSpeaking builds the voice coordinator now that the tea program
// exists (app.New builds the model before the program does) and installs the
// speaking-indicator hook. The narrator starts inert (nil coord) until this
// call, so no throwaway coordinator is built in New. Safe to call once; a
// no-op when voice is unavailable or already wired.
//
// Wiring happens before Run. Preserve the shared narrator pointer so the
// value already copied into Bubble Tea receives the coordinator and hooks.
func (m *Model) WireVoiceSpeaking(p *tea.Program) {
	if m.narrator == nil || p == nil || m.narrator.coord != nil {
		return
	}
	m.narrator.coord = newVoiceCoordinator(m.settings.Voice, p)
	if m.narrator.coord != nil {
		m.narrator.coord.SetGroupDrainedHook(m.narrator.onAnyGroupDrained)
	}
	// Async user-language detection posts its result through the same
	// program.Send channel as the speaking hook (task 3.1). Written through
	// the shared wiring state so the program's Model copy sees it.
	if m.voiceWiring == nil {
		m.voiceWiring = &voiceWiringState{}
	}
	m.voiceWiring.langPush = p.Send
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
	return voiceCoordinatorWithSpeaker(vcfg, p, sp)
}

func voiceCoordinatorWithSpeaker(vcfg config.Voice, p *tea.Program, sp *voice.Speaker) *voice.Coordinator {
	c := voice.NewCoordinator(sp, &vcfg)
	if p != nil {
		c.SetSpeakingHook(func(on bool) {
			// Send blocks on Tea's unbuffered event channel. The coordinator
			// dispatches hooks on one ordered worker, never inside Update.
			// Tea releases a pending Send when the program exits.
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
	m.CloseVoice()
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

// CloseVoice stops narration on every program exit, including external kills.
func (m *Model) CloseVoice() {
	if m.narrator != nil && m.narrator.coord != nil {
		m.narrator.cancelActive()
		m.narrator.coord.Close()
	}
}

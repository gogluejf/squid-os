package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/inputqueue"
	"squid-os/internal/localinput"
	"squid-os/internal/log"
)

// handleMouseClick toggles expand/collapse of the block under the cursor.
// Clicking an expandable block toggles only that block.
func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	line := m.viewport.YOffset + msg.Y - 1
	for _, r := range m.session.blockRanges {
		if line >= r.Start && line < r.End {
			msgID, block, _ := strings.Cut(r.Key, "\x00")
			m.session.expand.Toggle(msgID, block)
			m.refreshViewportAnchored(m.session.invalidateRenderAll)
			return m, nil
		}
	}
	return m, nil
}

// Update is the top-level Bubble Tea update function — routes every incoming
// message to the appropriate handler based on its type and current mode.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.refreshViewportFollowing()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.viewport.ScrollUp(3)
		case tea.MouseButtonWheelDown:
			m.viewport.ScrollDown(3)
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress { // ignore release — a click is one toggle
				return m.handleMouseClick(msg)
			}
		}
		return m, nil

	case uiTickMsg:
		m.refreshViewportFollowing()
		return m, uiTickCmd(m.session.Stream.Active)

	case voiceSpeakingMsg:
		// Voice speaking indicator (task 1.6): set on first chunk start,
		// cleared on player exit or kill. The coordinator's generation guard
		// guarantees a stale callback from a superseded group can never clear
		// a newer group's indicator (see voice.Coordinator.setSpeakingLocked).
		m.voiceSpeaking = msg.on
		return m, nil

	case langDetectedMsg:
		// Async user-language detection result (task 3.1). The store already
		// dropped stale results via SetIfNewest, but the posted value is a
		// SNAPSHOT taken when THIS submission's round-trip resolved — a newer
		// submission may have landed since. Apply only while this result still
		// belongs to the newest submission; otherwise keep the current value
		// (a later msg for the newer submission carries its language). Finding
		// #7. Written only here (Update), per the Bubble Tea single-goroutine
		// rule.
		log.LogVoice("detect.ui", "submission=%d newest=%d applied=%t language=%q", msg.subID, m.langSeq, msg.lang != "" && msg.subID >= m.langSeq, msg.lang)
		if msg.lang != "" && msg.subID >= m.langSeq {
			m.lastUserLang = msg.lang
		}
		return m, nil

	case streamEventMsg:
		return m.handleStreamEvent(chat.StreamEvent(msg))

	case toolEventMsg:
		return m.handleToolEvent(chat.ToolEvent(msg))

	case pendingToolResumeMsg:
		m.session.UIStream.MsgIdx = msg.msgIdx
		return (&m).resumeToolExecution()

	case localSessionQuery:
		msg.reply <- m.liveSessionID()
		return m, nil
	case localVoiceStopMsg:
		// External voice stop (squid-os voice stop): the identical CancelGroup
		// path as alt+x — one frame silences the whole active speech group.
		// The reply is three-valued so a wedged UI loop (reported by the
		// callback wrapper on timeout) can never masquerade as nothing_playing.
		status := localinput.VoiceStopNothingPlaying
		if m.narrator.cancelActive() {
			status = localinput.VoiceStopCancelled
		}
		msg.reply <- status
		return m, nil
	case localAdmissionMsg:
		res := m.submissions.AdmitSubmission(msg.req, m.liveSessionID())
		info := localinput.ReceiptInfo{OK: res.OK, Code: res.Code, Message: res.Msg}
		if res.OK {
			counts := m.submissions.Counts(msg.req.SessionID)
			info.Durability = string(res.Receipt.Durability)
			info.Sequence = res.Receipt.Sequence
			info.Duplicate = res.Receipt.Duplicate
			info.Counts = localinput.Counts{Step: counts.Step, Turn: counts.Turn}
			if !res.Receipt.Duplicate {
				// Voice barge-in (task 2.2): a NEW submitted message cancels
				// the active speech group NOW, at admission — a busy
				// submission pushes no UI event until its boundary, so hooking
				// delivery would miss the immediate cancellation. A duplicate
				// receipt is an idempotent retry of an already-admitted
				// request: no new message was submitted, so it must NOT mute
				// in-progress speech.
				m.narrator.finishActiveChunk()
				// Async user-language detection (task 3.1): fire at ADMISSION
				// for every path. Idle deliveries run it inside
				// sendLocalSocketMessage, but busy/after-turn admissions park
				// in the queue and are drained at the boundary WITHOUT any
				// other detect call — the alert lead-in must follow their
				// language too (finding #6). The newest-subID guard drops
				// stale results; duplicates are excluded above.
				(&m).fireUserLangDetect(msg.req.Text)
			}
		}
		msg.reply <- info
		if res.OK && !res.Receipt.Duplicate && msg.req.Boundary == inputqueue.BoundaryStep && !m.isBusy() {
			return m.sendLocalSocketMessage(localSocketMsg{requestID: msg.req.RequestID, sessionID: msg.req.SessionID, text: msg.req.Text, source: msg.req.Source})
		}
		return m, nil
	case localSocketMsg:
		// Socket submission that arrived while idle: send it NOW as a normal
		// user message (the admission already parked it pending; mark it
		// injected after the real send so the queue never double-fires it).
		return (&m).sendLocalSocketMessage(msg)

	case modelsLoadedMsg:
		m = m.onModelsLoaded(msg)
		return m, nil

	case contextRefreshMsg:
		// Silent background refresh — just update context window, don't change mode
		if len(msg.models) > 0 {
			m.modelEntries = msg.models
			(&m).refreshContextWindow(msg.models)
		}
		return m, nil
	}

	if m.mode == ModeChat {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Non-key messages: update active component if present (e.g., blink ticks)
	if m.mode == ModeComponent && m.activeComponent != nil {
		if cmd := m.activeComponent.Update(msg, &m); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// recalcLayout recomputes the viewport height based on current mode and terminal size.
// It syncs the textarea's cached Height with its actual content so multi-line
// input (Alt+Enter) and restored text grow/shrink the textarea naturally.
func (m *Model) recalcLayout() {
	// Set width first because visual row count depends on the content width.
	m.textarea.SetWidth(m.width)
	actualRows := textareaVisualRows(m.textarea.Value(), m.textarea.Width())
	if actualRows < 2 {
		actualRows = 2
	}
	if actualRows > m.textarea.MaxHeight {
		actualRows = m.textarea.MaxHeight
	}
	m.textarea.SetHeight(actualRows)

	inputHeight := 2 + m.textarea.Height() // textarea itself + padding/border around it
	const headerHeight = 1
	const footerHeight = 2

	overlayHeight := 0
	switch m.mode {
	case ModeComponent:
		if m.activeComponent != nil {
			overlayHeight = m.activeComponent.RenderHeight()
			m.activeComponent.Init(m)
		}
	case ModeHistorySearch:
		overlayHeight = m.historySearch.RenderHeight()
	case ModeChat:
		if _, ok := m.activeCapabilityCompletion(); ok {
			overlayHeight = 1
		}
	}

	const statusLineHeight = 1
	vpHeight := m.height - inputHeight - headerHeight - footerHeight - statusLineHeight - overlayHeight
	if vpHeight < 6 {
		vpHeight = 6
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
}

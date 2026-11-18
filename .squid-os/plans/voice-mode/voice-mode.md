# EPIC: Voice mode — the assistant speaks
Why: Give Squid-OS a spoken channel: responses, errors, and system events are read aloud via the local squid-tts server, toggleable live, interruptible at any time.
Outcomes: /voice + alt+v toggle; per-language voice settings; sentence-chunk streaming with ~2s time-to-first-audio; universal alt+x shut-up scoped to speech groups; sponge key-down cancellation via 'squid-os voice stop'; bilingual alert lead-ins driven by last-user-language detection.

## MILESTONE: 1 - Core voice pipeline
Pattern: Single-owner voice coordinator with group bumping; chunking strategy behind an interface
Objective: Assistant responses are spoken in sentence chunks while streaming, with per-language voice selection, group-scoped cancellation, and clean process teardown.
Success: With voice on, first audio arrives ~2s into a response; alt+x silences the whole group instantly including not-yet-emitted chunks; no orphaned aplay processes; kill switch verified under race.
Diagram: flowchart LR
Stream[Assistant stream] --> Chunker
Chunker --> Coord[Coordinator]
Coord --> Detect[/detect]
Detect --> Lookup[Voice lookup]
Lookup --> CLI[squid-tts-say]
CLI --> Aplay[aplay]
AltX[alt+x] -->|CancelGroup| Coord

### TASK: 1.1 - Add voice settings
Type: feature
What: Add nested voice config (enabled, voices.en, voices.fr) with defaults enabled=false, en=jane, fr=jane; normalize missing/partial sections at load time in internal/config/settings.go.
Why: Everything else reads from this structure; defaults must exist before the wrapper.
Files: ~ internal/config/settings.go
Snippet: type Voice struct {\n    Enabled bool\n    Voices  map[string]string // lang -> voice name\n}
Acceptance: Missing voice section loads as disabled with jane/jane
Acceptance: Partial sections (only voices.en set) fill fr from default
Acceptance: Invalid voice names pass through (server validates at request time)
Verification: go test ./internal/config

### TASK: 1.2 - Build voice wrapper and coordinator
Type: feature
What: New internal/voice package: Speak(text, cfg) spawns squid-tts-say in its own process group (detect → voice lookup → pipe stdin); Coordinator owns group state (StartGroup/Emit/EndGroup/CancelGroup) with muted-drop, back-to-back within group, and bump-across-groups via an onPreempt policy hook.
Why: Single owner of detect → lookup → spawn → teardown AND group arbitration; every caller goes through it; the onPreempt hook keeps the door open to stacking later.
Files: + internal/voice/voice.go
Files: + internal/voice/coordinator.go
Files: + internal/voice/voice_test.go
Snippet: type Coordinator struct {\n    current  int            // playing group ID\n    muted    map[int]bool   // cancelled groups\n    gen      int            // generation counter for stale-callback guards\n    onPreempt func(oldGroup int) // default: CancelGroup; future: enqueue\n}
Snippet: // Spawn: exec.Command("squid-tts-say", "-v", name, "-q", "--stdin")\n// SysProcAttr{Setpgid: true}; kill(-pgid, SIGKILL) on cancel or replacement
Acceptance: Resolves voice per detected language; unknown language falls back without failing
Acceptance: Same-group chunks play back-to-back without bumping each other (ordering test)
Acceptance: Cross-group Emit bumps the playing group (default policy) and starts immediately
Acceptance: Cancellation mid-playback kills the whole process group; no orphaned aplay — deterministic subprocess test plus go test -race
Acceptance: Emit for a muted groupID is dropped even if it arrives after cancellation (late-chunk test)
Acceptance: Server down returns error, never panics, bounded timeout; CLI resolved from PATH
Verification: go test ./internal/voice
Verification: go test -race ./internal/voice

### TASK: 1.3 - Implement the Chunker
Type: feature
What: Define Chunker { Feed; Flush; ArmHoldTimer } in internal/voice with a sentence-boundary strategy: split on . ! ? \n, hold fragments under ~3 words and merge, max hold 1.5s via owned timer, max chunk ~40 words force-flush.
Why: Measured TTS floor is 2-4s per call — single-word chunks sound robotic; merging fixes bullet lists; the interface keeps the algorithm swappable without touching wrapper or cancellation code.
Files: + internal/voice/chunker.go
Files: + internal/voice/chunker_test.go
Snippet: type Chunker interface {\n    Feed(text string) []string // emit complete chunks\n    Flush() string             // drain remainder at stream end\n    ArmHoldTimer(cb func())    // fire held fragment when input stalls (1.5s)\n}
Acceptance: Prose emits per sentence; one-word bullets merge into groups
Acceptance: 40-word run-on without periods force-flushes
Acceptance: Held fragment emits via timer when input stalls (injected ticker test)
Acceptance: Flush drains buffer exactly once; Feed after Flush starts fresh; timer cancelled after Flush
Acceptance: Strategy params (min words, max hold, max size, boundaries) are constructor args
Verification: go test ./internal/voice

### TASK: 1.4 - Wire narration into the assistant stream
Type: feature
What: Allocate a group ID at assistant message start; feed rendered text deltas into the Chunker and Emit chunks to the coordinator; EndGroup at stream end; group lifecycle tied to the message, surviving UIStreamState.reset() around tools.
Why: This is the core UX — first audio ~2s, not after full generation; already-emitted chunks keep playing during tool calls (no pause concept exists).
Files: ~ internal/app/stream.go
Files: ~ internal/app/app.go
Snippet: // at message start:  gid := coord.StartGroup()\n// per delta:         for _, chunk := range chunker.Feed(delta) { coord.Emit(gid, chunk) }\n// at stream end:     coord.Emit(gid, chunker.Flush()); coord.EndGroup(gid)
Acceptance: First chunk speaks while later sentences are still generating
Acceptance: Stream end sends EndGroup; a muted group stops receiving feeds (producer checks coordinator before emitting)
Acceptance: Group survives UIStreamState.reset() between tool phases; no duplicate or lost chunks across the reset
Acceptance: Voice mute never cancels the model stream or tool execution (independent contexts verified)
Acceptance: Session switch / shutdown tears down active groups (kill + indicator cleared)
Verification: go test ./internal/app

### TASK: 1.5 - Toggle: /voice command, alt+v, help entry
Type: feature
What: Register /voice on|off in internal/app/command.go (add argument handling to the name-only callbacks); bind alt+v mirroring thinking; add both keys to the hard-coded help list in internal/ui/help.go; print feedback line on toggle.
Why: Live control without restarting; parity with thinking UX; WithHelp alone will not show the entry because help.go is hard-coded.
Files: ~ internal/app/command.go
Files: ~ internal/app/keymap.go
Files: ~ internal/ui/help.go
Files: ~ internal/app/update.go
Snippet: Thinking: key.NewBinding(key.WithKeys("alt+t"), key.WithHelp("alt+t", "toggle thinking")),\nVoice:    key.NewBinding(key.WithKeys("alt+v"), key.WithHelp("alt+v", "toggle voice"))
Acceptance: alt+v and /voice both flip the same runtime state; settings value is the startup source of truth; runtime toggle does not write the settings file
Acceptance: Help overlay lists alt+v
Acceptance: Toggling off mid-speech kills current playback immediately (CancelGroup + disable)
Verification: go test ./internal/app

### TASK: 1.6 - Speaking indicator
Type: feature
What: Show a speaker glyph / 'speaking…' in the existing status row (internal/ui/notification.go:98, called from render.go:78) while any audio plays; cleared on completion or cancellation.
Why: Shut-up needs a visible target; users must see that voice is active and that alt+x did something.
Files: ~ internal/ui/notification.go
Files: ~ internal/app/render.go
Files: ~ internal/app/app.go
Acceptance: Indicator appears on first chunk start, disappears on player exit or kill
Acceptance: Never stuck on after cancellation (kill path clears it)
Acceptance: Stale callbacks from superseded groups cannot clear a NEWER group's indicator (generation-ID guard)
Verification: manual TUI run

## MILESTONE: 2 - Cancellation and external control
Pattern: Group-scoped cancellation; external commands route into the same coordinator
Objective: alt+x, new messages, and sponge key-down all trigger the identical CancelGroup path; 'squid-os voice stop' works from outside the TUI.
Success: Every cancel source silences the whole group within one frame and leaves no orphans; socket command round-trips like input ops.
Diagram: flowchart LR
AltX[alt+x] --> Coord[Coordinator]
NewMsg[Submitted message] --> Coord
Sponge[Sponge key-down] --> CLI2[squid-os voice stop]
CLI2 --> Socket[Unix socket]
Socket --> Coord
Coord --> KillPG[kill -pgid SIGKILL]
AltX -. CancelGroup .-> Coord
NewMsg -. CancelGroup .-> Coord
Socket -. CancelGroup .-> Coord

### TASK: 2.1 - alt+x universal shut-up
Type: feature
What: Bind alt+x (verified free); handler calls coordinator.CancelGroup(activeGroup) — kills running chunk, drops buffered chunks, mutes all future chunks of that group. Handler must run BEFORE mode dispatch (internal/app/input.go:15) so authorization and help overlays cannot swallow the key.
Why: Per-group mute with zero exception rules; applies to narration AND alerts identically; one press silences a multi-chunk error completely. Not sticky: next group speaks normally.
Files: ~ internal/app/keymap.go
Files: ~ internal/app/input.go
Files: ~ internal/app/update.go
Snippet: ShutUp: key.NewBinding(key.WithKeys("alt+x"), key.WithHelp("alt+x", "shut up voice"))
Acceptance: alt+x during narration mutes rest of current message only — including phrases not yet emitted
Acceptance: alt+x during an error lead-in silences the upcoming detail chunk too (one press, whole group dead)
Acceptance: alt+x works while the authorization overlay or help overlay is open
Acceptance: Next assistant message/alert speaks normally without re-enabling anything
Verification: go test ./internal/app

### TASK: 2.2 - New submitted message cancels speech
Type: feature
What: Submitting a user message cancels the active group before processing begins, hooked at ALL THREE admission paths: idle keyboard (stream.go:86), queued keyboard (queued_input.go:39), and socket admission (local_input.go:42) — at admission, not eventual delivery.
Why: User talking/typing a new instruction = barge-in; busy socket submissions currently push no UI event (local_input.go:59-62), so hooking only delivery misses immediate cancellation.
Files: ~ internal/app/stream.go
Files: ~ internal/app/queued_input.go
Files: ~ internal/app/local_input.go
Acceptance: Sending a message while Jane speaks silences her whole group before the new turn starts, from all three paths
Acceptance: Plain typing WITHOUT submitting does NOT cancel (listen-while-drafting preserved)
Verification: go test ./internal/app

### TASK: 2.3 - squid-os voice stop CLI command
Type: feature
What: New internal/cli/voice.go following the input.go pattern: 'squid-os voice stop' sends a socket op to the live TUI triggering the same CancelGroup as alt+x; register in cli.go, add op to server dispatch (internal/localinput/server.go:188) and app-side handler (internal/app/local_input.go). Scaffold for future on/off subcommands.
Why: Sponge needs key-down cancellation before any transcript exists; same Unix socket protocol as input, zero new infrastructure. Use existing --instance/--session flags (input.go:46-47).
Files: + internal/cli/voice.go
Files: + internal/cli/voice_test.go
Files: ~ internal/localinput/server.go
Files: ~ internal/app/local_input.go
Files: ~ internal/cli/cli.go
Snippet: // voice stop = localinput.Request{Op: "voice_stop"}\n// reply: {status: "cancelled" | "nothing_playing"}, exit 0 either way
Acceptance: 'squid-os voice stop' against a live instance silences the current group; reply acknowledges actual cancellation or explicit nothing-playing no-op, exit 0
Acceptance: No live instance gives clean error, non-zero exit, same style as input commands
Acceptance: --instance/--session flags behave like input's
Verification: go test ./internal/cli ./internal/localinput

### TASK: 2.4 - Sponge key-down integration
Type: feature
What: In the sponge client, on shortcut key-DOWN (before transcription), invoke 'squid-os voice stop'. LOCATE the sponge source first (check ~/src/squid-stack/squid-companion and the sponge repo) and record the real path here before implementing.
Why: Voice must die the instant the user starts talking into the mic — not when the transcript arrives seconds later; speaking over the assistant would be unusable.
Files: ~ sponge client source - LOCATE FIRST
Acceptance: Pressing the sponge shortcut while voice plays silences it within ~100ms, before any audio from the user is captured
Acceptance: Works even when no voice is playing (no-op, no error spam)
Verification: manual test with mic

## MILESTONE: 3 - Bilingual alerts
Pattern: Event hooks push preformatted alert groups through the same coordinator
Objective: Errors, authorization prompts, context bust, skill loads, sub-agent calls, and thinking-start are announced in the user's language with English detail where relevant.
Success: Each event speaks its lead-in in lastUserLang as its own group; all alerts share universal cancellation; alert map lives in one table of ~12 strings.
Diagram: flowchart LR
Events[App events] --> Format[Alert formatter]
LastLang[lastUserLang] --> Format
Format -->|StartGroup| Coord[Coordinator]

### TASK: 3.1 - Track lastUserLang asynchronously
Type: feature
What: On every user message submission, fire /detect asynchronously (goroutine, ~500ms deadline) with a submission ID; store the result in session state only if that ID is still the newest. Fallback: previous value, then en.
Why: Drives alert lead-in language (~75ms locally, cheap enough to run always) without serializing any of the three submission paths; older results must never overwrite newer language state.
Files: + internal/voice/detect.go
Files: ~ internal/app/stream.go
Files: ~ internal/app/queued_input.go
Files: ~ internal/app/local_input.go
Snippet: func DetectLang(text string, subID int64, store *LangStore) {\n    // POST /detect with 500ms deadline\n    // store.SetIfNewest(subID, lang) // stale results dropped\n}
Acceptance: French user message sets lastUserLang=fr for subsequent alerts
Acceptance: Rapid-fire two messages (FR then EN): only the EN result lands, even if FR resolves second (ordering test)
Acceptance: Detect failure/timeout leaves previous value intact; turn latency unaffected
Verification: go test ./internal/voice ./internal/app

### TASK: 3.2 - Alert map and formatting
Type: feature
What: Single table of bilingual alert templates in internal/voice/alerts.go: error, authorization, context-bust, skill-loaded with name, sub-agent-called, thinking-started; formatter picks language from lastUserLang and interpolates details.
Why: ~12 strings total — maintainable; the user hears their own language immediately while technical detail stays English (auto-detected by the server).
Files: + internal/voice/alerts.go
Files: + internal/voice/alerts_test.go
Snippet: var alerts = map[Event]map[string]string{\n    Error:        {en: "An error occurred.", fr: "Une erreur est survenue."},\n    Authorization:{en: "A command requires your authorization.", fr: "Une commande requiert votre autorisation."},\n    ContextBust:  {en: "Warning, no context left.", fr: "Attention, plus de contexte disponible."},\n    SkillLoaded:  {en: "I loaded the skill {name}.", fr: "Jai charge la competence {name}."},\n    SubAgent:     {en: "I sent a sub-agent working on it.", fr: "Jai lance un sous-agent qui va effectuer le travail."},\n    ThinkingStart:{en: "Let me think about it now.", fr: "Laisse-moi y penser."},\n}
Acceptance: Each event renders the exact EN/FR strings from the table
Acceptance: Error alerts append raw English detail after the lead-in (same group)
Acceptance: Unknown language falls back to EN templates
Verification: go test ./internal/voice

### TASK: 3.3 - Hook events into the voice coordinator
Type: feature
What: Wire all six alert call sites, each starting its own group: ToolEventNeedAuth (stream.go:293), LoadedSkill (stream.go:284/310), a NEW pre-execution agent event in tool_worker.go (ToolEventRunning fires too late, after ExecuteTools returns), error paths with distinct context-bust classification, and thinking-start (once per thinking session when thinking mode is active).
Why: These are the 'get my eyes here' moments — the highest-value voice use; alerts land between messages in practice, so bumping narration is the rare overlap case.
Files: ~ internal/app/stream.go
Files: ~ internal/chat/tool_worker.go
Snippet: // guard: coordinator suppresses new alerts while an alert is playing\n// (prevents TTS-failure -> spoken-error recursion)
Acceptance: With voice on each of the six events speaks its alert as its own group; with voice off silence and no behavior change
Acceptance: Thinking-start speaks exactly once per thinking session, only when thinking mode is active
Acceptance: Alert landing while the tail of an answer is still playing bumps the tail (intended behavior) and speaks
Acceptance: alt+x during an alert behaves identically to alt+x during narration (whole group dies including pending detail chunk)
Acceptance: TTS failure during an alert produces no further speech (recursion guard tested)
Verification: go test ./internal/app

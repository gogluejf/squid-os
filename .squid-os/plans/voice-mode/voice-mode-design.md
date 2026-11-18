# EPIC: Voice mode — the assistant speaks
Why: Give Squid-OS a spoken channel: responses, errors, and system events are read aloud via the local squid-tts server, toggleable live, interruptible at any time.
Outcomes: /voice + alt+v toggle; per-language voice settings; sentence-chunk streaming with ~2 s time-to-first-audio; universal alt+x shut-up (group-scoped); sponge key-down cancellation via `squid-os voice stop`; bilingual alert lead-ins driven by last-user-language detection.
Contract: none (self-contained). Depends on: squid-stack squid-tts server (port 8811) + squid-tts-say CLI (committed 9908891).

## Scope decisions (brainstorm 2026-11-18)
- Jane is the default voice for BOTH en and fr.
- Voice is OFF by default; user opts in.
- One voice at a time. ONE verb only: **bump**. A new group cancels the old group (no pause/resume — see "Speech groups"). Bump-vs-stack is a one-function policy hook so stacking can be enabled later without rework.
- alt+x kills the current GROUP (playing chunk + all buffered/future chunks of that group), no matter what called it (narration or alert). No exception rules. Not sticky: next group speaks normally.
- Typing does NOT interrupt speech (screen-reader model: listen while drafting). Only alt+x, new submitted message, and sponge key-down cancel.
- Language is auto-detected by the TTS server per request; Squid-OS also detects each user message (~75 ms, free) to drive alert lead-in language. Revisit if cost ever matters.
- No TTS server changes. One voice name per request, always. The server has NO pause/stream concept — each CLI call is one complete sentence WAV; "pausing" would mean holding off spawning the next process, which we don't need because there is no pause.
- Long responses: full speech, no truncation (deferred).
- Concurrent sessions: two TUIs = two Janes, accepted (deferred).
- Code vocabulary: `cancel`/`CancelGroup` in code (matches existing context.Cancel / ToolEventCancelled usage); user-facing help text says "shut up".
- Alerts (errors, skill load, agent call, auth, context bust) occur BETWEEN messages or when token generation has stopped — not mid-stream of a live answer. So in practice groups play back-to-back; bumping only matters in the rare overlap (alert fires while the tail of an answer is still playing → tail dies, alert speaks; text is on screen anyway).

## Speech groups (core coordinator semantics)
A **group** = one logical utterance, identified by a monotonically increasing group ID.
- An assistant message = 1 group containing N sentence chunks (fed as they stream).
- An error alert = 1 group containing 2 chunks (localized lead-in + raw English detail).
- Skill/agent/auth/context-bust alerts = 1 group, 1 chunk each.

Coordinator rule:
```
on chunk(groupID, text):
    if groupID is muted → drop silently
    else if no group playing → speak this chunk
    else if playing group == groupID → speak after current chunk finishes (back-to-back continuation)
    else → onPreempt(playingGroup)   // policy hook, see below
           then speak this chunk
when a group finishes (producer marks end-of-group):
    nothing to resume — previous groups were bumped (cancelled), not held
```

**The single verb — bump:**
- `onPreempt(oldGroup)` default policy = `CancelGroup(oldGroup)`: kill the playing chunk's process group, mute the old group (buffered + future chunks dropped on arrival). The new group starts immediately.
- **Policy hook for the future:** `onPreempt` is ONE small function. Today its body is "cancel". If UX later prefers stacking (finish current, then play the new group), its body becomes "enqueue" — no other coordinator change. This is the kept door; do not build the queue now.

**User cancel vs bump (same mechanics, different trigger):**
- `CancelGroup(groupID)` — user-triggered (alt+x, new submitted message, sponge key-down, `voice stop`): mutes the group. Playing chunk killed, buffered chunks dropped, every FUTURE chunk arriving for that groupID dropped on arrival (chunker keeps emitting; coordinator discards). Producer receives explicit end-of-group signal and stops feeding a muted group.
- Bump — system-triggered (a newer group arrives): calls the same CancelGroup on the older group via the policy hook.

Invariants:
- Consecutive chunks of the SAME group never bump each other — they play back-to-back. Bumping applies only across group boundaries.
- Generation/group IDs guard against stale callbacks: a player-exit callback, detect result, or chunk timer belonging to a superseded group must not clear a newer speaking indicator or restart speech.
- Voice cancellation is SEPARATE from the existing tool/turn context (stream.go turn ctx). Muting voice never cancels the model stream or tool execution, and vice versa.
- During long tool calls: already-emitted narration chunks keep playing (nothing to pause); the chunker simply emits nothing new while tokens stop. No special handling.

## Settings shape
```
voice:
  enabled: false          # off by default
  voices:
    en: jane              # defaults: jane / jane
    fr: jane
```
Settings are JSON loaded into zero-valued structs (internal/config/settings.go:113) — explicitly seed/normalize Jane defaults for missing and partial sections. Decision: runtime toggles do NOT persist to the settings file (mirrors thinking's mode-changing behavior); settings value is the startup source of truth.

## Alert map (bilingual lead-ins, ~10 strings total)
Picked from lastUserLang; raw English detail follows when present.
| Event | EN | FR |
|---|---|---|
| Error | "An error occurred." | "Une erreur est survenue." |
| Authorization | "A command requires your authorization." | "Une commande requiert votre autorisation." |
| Context bust | "Warning, no context left." | "Attention, plus de contexte disponible." |
| Skill loaded | "I loaded the skill {name}." | "J'ai chargé la compétence {name}." |
| Sub-agent called | "I sent a sub-agent working on it." | "J'ai lancé un sous-agent qui va effectuer le travail." |
| Thinking started | "Let me think about it now." | "Laisse-moi y penser." |

Error alerts: lead-in + raw English error text spoken second (auto-detected as EN by server) — ONE group, so one alt+x silences both parts.
Skill/agent alerts: single sentence, no extra detail (start small, no task labels).
TTS failures must NOT recursively generate spoken error alerts (guard flag in coordinator).

## MILESTONE 1 - Core voice pipeline
Pattern: Single-owner voice coordinator with group bumping; chunking strategy behind an interface
Objective: Assistant responses are spoken in sentence chunks while streaming, with correct per-language voice selection, group-scoped cancellation, and clean process teardown.
Success: With voice on, first audio arrives ~2 s into a response; alt+x silences the whole group instantly (including not-yet-emitted chunks); no orphaned aplay processes; kill switch verified under race.
Diagram: flowchart LR
Stream --> Chunker
Chunker --> Coord[Coordinator]
Coord --> Detect
Detect --> Lookup
Lookup --> CLI
CLI --> Aplay
AltX -->|CancelGroup| Coord

### TASK 1.1 - Add voice settings
Type: feature
What: Add nested voice config (enabled, voices.en, voices.fr) with defaults enabled=false, en=jane, fr=jane; normalize missing/partial sections at load time.
Why: Everything else reads from this structure; defaults must exist before the wrapper.
Files: ~ internal/config/settings.go (struct + normalization near the loader at :113)
Acceptance: Missing voice section loads as disabled with jane/jane.
Acceptance: Partial sections (only voices.en set) fill fr from default.
Acceptance: Invalid voice names pass through (server validates at request time).
Verification: go test ./internal/config

### TASK 1.2 - Build the voice wrapper and coordinator
Type: feature
What: New internal package internal/voice. Two layers:
(a) Wrapper: Speak(text string, cfg *config.Voice) — POST /detect on text → lang (fallback chain: detected → previous session lang → "en"); voiceName = cfg.Voices[lang]; spawn `squid-tts-say -v <name> -q --stdin` in its OWN process group (SysProcAttr{Setpgid:true}); pipe text to stdin; verify aplay stays in the group. Each call = one sentence = one short-lived process; no long-lived stream state.
(b) Coordinator: owns group state — StartGroup() int, Emit(groupID, text), EndGroup(groupID), CancelGroup(groupID); enforces the group invariants (muted-drop, back-to-back within group, bump-across-groups via the onPreempt policy hook); exactly one Wait per spawned process; kill(-pgid, SIGKILL) on cancel and before replacement playback; watcher retired on normal exit so late kills can't hit reused PIDs.
Why: Single owner of detect → lookup → spawn → teardown AND of group arbitration; every caller goes through it.
Files: + internal/voice/voice.go
Files: + internal/voice/coordinator.go
Files: + internal/voice/voice_test.go
Snippet: // coordinator holds: currentGroup, muted set, gen counter, onPreempt func
// onPreempt default: CancelGroup(old)  // future: enqueue (stacking) — one-line swap
// Emit: if muted(gen) drop; if current==other group → onPreempt(current), spawn this
// CancelGroup: kill pgid, add to muted, drop buffer, signal producer end
Acceptance: Resolves voice per detected language; unknown language falls back without failing.
Acceptance: Same-group chunks play back-to-back without bumping each other (ordering test).
Acceptance: Cross-group Emit bumps the playing group (default policy) and starts immediately.
Acceptance: Cancellation mid-playback kills the whole process group; no orphaned aplay — deterministic subprocess test (spawn real CLI with short text, cancel, assert process gone) plus go test -race, not just a ps spot check.
Acceptance: Emit for a muted groupID is dropped even if it arrives after cancellation (late-chunk test).
Acceptance: Server down → returns error, never panics, bounded timeout.
Acceptance: CLI path resolved from $PATH (squid-tts-say installed in ~/.local/bin).
Verification: go test ./internal/voice
Verification: go test -race ./internal/voice

### TASK 1.3 - Implement the Chunker interface
Type: feature
What: Define Chunker { Feed(text string) []string; Flush() string; ArmHoldTimer(cb func()) } — the timer contract is REQUIRED: max-hold (1.5 s) must fire even when tokens stop arriving, so the chunker needs an owned timer/tick whose callback emits the held fragment; timer cancelled on cancel/Flush. Strategy: split on . ! ? \n; hold fragments under ~3 words and merge with next; max hold 1.5 s; max chunk ~40 words force-flush; Flush returns remainder.
Why: Measured TTS floor is 2-4 s per call — single-word chunks sound robotic; merging fixes bullet lists. Interface keeps the algorithm swappable without touching wrapper or cancellation code.
Files: + internal/voice/chunker.go
Files: + internal/voice/chunker_test.go
Acceptance: Prose emits per sentence; one-word bullets merge into groups.
Acceptance: 40-word run-on without periods force-flushes.
Acceptance: Held fragment emits via timer when input stalls (test with manual clock or injected ticker).
Acceptance: Flush drains buffer exactly once; Feed after Flush starts fresh; timer cancelled after Flush.
Acceptance: Strategy params (min words, max hold, max size, boundaries) are constructor args, not constants scattered in logic.
Verification: go test ./internal/voice

### TASK 1.4 - Wire narration into the assistant stream
Type: feature
What: While an assistant message streams and voice.enabled: allocate a group ID at message start; feed rendered text deltas into the Chunker; each emitted chunk → coordinator.Emit(groupID, chunk); mark EndGroup at stream end (explicit "no more coming" signal, even if the buffer drains early); handle stream reset/restart around tools (stream.go:216, 329–332; ui_stream_state.go:25 reset) — group ownership must survive UIStreamState.reset(); final flush at true message completion. Already-emitted chunks keep playing during tool calls (no pause concept exists); the chunker simply emits nothing while tokens stop.
Why: This is the core UX — first audio ~2 s, not after full generation.
Files: ~ internal/app/stream.go (delta sites; auth at :293, LoadedSkill at :284/:310, cancelled at :334)
Files: ~ internal/app/app.go (group lifecycle tied to message, NOT to the tool/turn ctx at stream.go:264–271)
Acceptance: First chunk speaks while later sentences are still generating.
Acceptance: Stream end sends EndGroup; a muted group stops receiving feeds (producer checks coordinator before emitting).
Acceptance: Group survives UIStreamState.reset() between tool phases; no duplicate or lost chunks across the reset.
Acceptance: Voice mute never cancels the model stream or tool execution (independent contexts verified).
Acceptance: Session switch / shutdown tears down active groups (kill + indicator cleared).
Verification: go test ./internal/app + manual TUI run

### TASK 1.5 - Toggle: /voice command, alt+v, help entry
Type: feature
What: /voice on|off slash command registered in internal/app/command.go:23 (name-only callbacks today — add deliberate argument handling for on/off); alt+v binding mirroring thinking (keymap entry, WithHelp "alt+v", "toggle voice"); ALSO add the line to the hard-coded help list in internal/ui/help.go:77 (WithHelp alone will not show it); feedback line printed on toggle ("voice on" / "voice off"). Toggling off = CancelGroup(current) + disable until re-enabled.
Why: Live control without restarting; parity with thinking UX.
Files: ~ internal/app/command.go
Files: ~ internal/app/keymap.go
Files: ~ internal/ui/help.go
Files: ~ internal/app/update.go
Acceptance: alt+v and /voice both flip the same runtime state; settings value is the source of truth on startup; runtime toggle does not write the settings file.
Acceptance: Help overlay lists alt+v (and alt+x once 2.1 lands).
Acceptance: Toggling off mid-speech kills current playback immediately.
Verification: go test ./internal/app + manual TUI run

### TASK 1.6 - Speaking indicator
Type: feature
What: Visible status via the EXISTING status row: internal/ui/notification.go:98, called from internal/app/render.go:78 (not ui/component). Show speaker glyph / "speaking…" while any audio plays; cleared on completion or cancellation.
Why: Shut-up needs a visible target; users must see that voice is active.
Files: ~ internal/ui/notification.go
Files: ~ internal/app/render.go
Files: ~ internal/app/app.go
Acceptance: Indicator appears on first chunk start, disappears on player exit or kill.
Acceptance: Never stuck on after cancellation (kill path clears it); stale callbacks from superseded groups cannot clear a NEWER group's indicator (generation-ID guard).
Verification: manual TUI run

## MILESTONE 2 - Cancellation and external control
Pattern: Group-scoped cancellation; external commands route into the same coordinator
Objective: alt+x, new messages, and sponge key-down all trigger the identical CancelGroup path; `squid-os voice stop` works from outside the TUI.
Success: Every cancel source verified to silence the whole group within one frame and leave no orphans; socket command round-trips like input ops.
Diagram: flowchart LR
AltX -->|CancelGroup| Coord
NewMsg -->|CancelGroup| Coord
SpongeDown --> CLI2[voice stop]
CLI2 --> Socket
Socket -->|CancelGroup| Coord
Coord --> KillPG

### TASK 2.1 - alt+x universal shut-up
Type: feature
What: Bind alt+x (verified free); handler calls coordinator.CancelGroup(activeGroup) — kills running chunk, drops buffered chunks, mutes all future chunks of that group. CRITICAL PLACEMENT: the handler must run BEFORE mode dispatch (internal/app/input.go:15) so authorization overlays and the help overlay cannot swallow the key. Not sticky: next group speaks normally.
Why: Per-group mute with zero exception rules; applies to narration AND alerts identically. One alt+x silences a multi-chunk error (lead-in + detail) completely.
Files: ~ internal/app/keymap.go
Files: ~ internal/app/input.go (before mode dispatch at :15)
Files: ~ internal/app/update.go
Acceptance: alt+x during narration mutes rest of current message only — including phrases not yet emitted.
Acceptance: alt+x during an error lead-in silences the upcoming detail chunk too (one press, whole group dead).
Acceptance: alt+x works while the authorization overlay or help overlay is open.
Acceptance: Next assistant message/alert speaks normally without re-enabling anything.
Verification: go test ./internal/app + manual TUI run

### TASK 2.2 - New submitted message cancels speech
Type: feature
What: Submitting a user message cancels the active group before processing begins. Hook ALL THREE admission paths: idle keyboard (stream.go:86), queued keyboard (queued_input.go:39), and socket admission (local_input.go:42) — note busy socket submissions currently do NOT push a UI event (local_input.go:59–62), so hook at admission, not at eventual delivery.
Why: User talking/typing a new instruction = barge-in; matches interrupt-immediately decision.
Files: ~ internal/app/stream.go
Files: ~ internal/app/queued_input.go
Files: ~ internal/app/local_input.go
Acceptance: Sending a message while Jane speaks silences her (whole group) before the new turn starts, from all three paths.
Acceptance: Plain typing WITHOUT submitting does NOT cancel (listen-while-drafting preserved).
Verification: go test ./internal/app + manual TUI run

### TASK 2.3 - squid-os voice stop CLI command
Type: feature
What: New internal/cli/voice.go following the input.go pattern: `squid-os voice stop` sends a socket op to the live TUI triggering the same CancelGroup as alt+x. Scaffold for future `on`/`off` subcommands. Use the EXISTING flags --instance/--session (input.go:46–47), not -i/-s. Register in cli.go; add the op to server dispatch (internal/localinput/server.go:188) and the app-side handler (internal/app/local_input.go + UI-loop control delivery).
Why: Sponge needs key-down cancellation before any transcript exists; same Unix socket protocol, zero new infrastructure.
Files: + internal/cli/voice.go
Files: + internal/cli/voice_test.go
Files: ~ internal/localinput/server.go (op dispatch)
Files: ~ internal/app/local_input.go (handler + UI-loop delivery)
Files: ~ internal/cli/cli.go (register command tree)
Acceptance: `squid-os voice stop` against a live instance silences the current group; reply acknowledges actual cancellation (or explicit "nothing playing" no-op), exit 0.
Acceptance: No live instance → clean error, non-zero exit, same style as input commands.
Acceptance: --instance/--session flags behave like input's.
Verification: go test ./internal/cli ./internal/localinput

### TASK 2.4 - Sponge key-down integration
Type: feature
What: In the sponge client, on shortcut key-DOWN (before transcription), invoke `squid-os voice stop`.
Why: Voice must die the instant the user starts talking into the mic — not when the transcript arrives seconds later.
Files: ~ (sponge client source — LOCATE FIRST: check ~/src/squid-stack/squid-companion and sponge repo; update this plan with the real path before implementing)
Acceptance: Pressing the sponge shortcut while voice plays silences it within ~100 ms, before any audio from the user is captured.
Acceptance: Works even when no voice is playing (no-op, no error spam).
Verification: manual test with mic

## MILESTONE 3 - Alerts (bilingual event speech)
Pattern: Event hooks push preformatted alert groups through the same coordinator
Objective: Errors, authorization prompts, context bust, skill loads, and sub-agent calls are announced in the user's language with English detail where relevant.
Success: Each event speaks its lead-in in lastUserLang as its own group; all alerts share universal cancellation; alert map lives in one table.
Diagram: flowchart LR
Events --> Format
LastLang --> Format
Format -->|StartGroup| Coord

### TASK 3.1 - Track lastUserLang (asynchronous)
Type: feature
What: On every user message submission, fire /detect ASYNCHRONOUSLY (goroutine, bounded deadline ~500 ms) with a submission ID attached; store result in session state (lastUserLang) ONLY IF the submission ID is still the newest — older results must never overwrite newer language state. While detection is pending, alerts use the previous value (fallback "en" if none). Failure/timeout keeps the previous value; never blocks the turn.
Why: Drives alert lead-in language; cheap enough to run always (revisit later if needed) but must not serialize the submission path.
Files: + internal/voice/detect.go (thin /detect HTTP helper with deadline + submission-ID guard)
Files: ~ internal/app/*.go (fire at all three submission paths from 2.2; session state field)
Acceptance: French user message → lastUserLang=fr for subsequent alerts.
Acceptance: Rapid-fire two messages (FR then EN): only the EN result lands, even if FR resolves second (ordering test).
Acceptance: Detect failure/timeout leaves previous value intact; turn latency unaffected.
Verification: go test ./internal/voice ./internal/app

### TASK 3.2 - Alert map and formatting
Type: feature
What: Single table of bilingual alert templates (error, authorization, context-bust, skill-loaded{name}, sub-agent-called) per the alert-map table; formatter picks language from lastUserLang and interpolates details.
Why: ~10 strings total — maintainable; critical part (user hears their own language) covered without translating technical detail.
Files: + internal/voice/alerts.go
Files: + internal/voice/alerts_test.go
Acceptance: Each event renders the exact EN/FR strings from the table.
Acceptance: Error alerts append raw English detail after the lead-in (same group).
Acceptance: Unknown language falls back to EN templates.
Verification: go test ./internal/voice

### TASK 3.3 - Hook events into the voice coordinator
Type: feature
What: Call sites, each starting its OWN group (bumps whatever is playing via the default policy — in practice alerts land between messages, so bumping is the rare case):
- Authorization prompt: ToolEventNeedAuth at stream.go:293 (hook exists).
- Skill loaded: LoadedSkill events at stream.go:284/:310 (hooks exist).
- Sub-agent called: ToolEventRunning is NOT reliable — it fires AFTER ExecuteTools returns (internal/chat/tool_worker.go:66–81). ADD a real pre-execution event (e.g. ToolEventStarting) in tool_worker.go for agent invocations.
- Errors incl. context bust: classify context-bust distinctly (its own template); route through the same hook; GUARD against recursion — a TTS failure must not spawn a spoken error alert (coordinator flag: suppress alerts while an alert is playing).
- Thinking started: fire when thinking mode is on AND thinking tokens begin streaming (locate the thinking-delta site in stream.go during implementation). Once per thinking session — not per token, not repeated.
Why: These are the "get my eyes here" moments — the highest-value voice use.
Files: ~ internal/app/stream.go
Files: ~ internal/chat/tool_worker.go (new pre-execution event)
Files: ~ skill loader / agent invocation sites as found
Acceptance: With voice on: each of the six events speaks its alert as its own group; with voice off: silence, no behavior change.
Acceptance: Thinking-start speaks exactly once per thinking session ("Let me think about it now." / "Laisse-moi y penser."), only when thinking mode is active.
Acceptance: Alert landing while the tail of an answer is still playing: tail is bumped (cancelled), alert speaks — verified, and accepted as intended behavior.
Acceptance: Back-to-back sequence (message done → error speaks) plays naturally with no gap logic needed.
Acceptance: alt+x during an alert behaves identically to alt+x during narration (whole group dies, including pending detail chunk).
Acceptance: TTS failure during an alert produces no further speech (recursion guard tested).
Verification: go test ./internal/app + manual TUI run for each event

## Deferred (explicitly out of scope)
- Long-thinking / long-waiting encouragement speech (timer-driven "still thinking…" nudge) — deliberately deferred: first timer-driven speech, needs feel-first feedback; door is clean (one timer + one map entry + one hook, all inside phase-1 patterns)
- Stacking policy for onPreempt (speak-then-continue instead of bump) — the hook exists; body swap later if UX prefers it
- Long-response truncation (first-N-sentences mode)
- Concurrent-session arbitration
- Low-confidence sponge confirm-before-send for destructive patterns
- Plan-milestone-done / session-resume announcements (alert map absorbs them trivially when wanted)

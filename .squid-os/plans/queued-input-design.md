# Queued input, local submission, and Hex Sponge

Status: implementation design, not implemented. Reviewed 2026-11-17.
Repositories: `<squid-repo>` = squid-os; `<hex-repo>` = sibling stt-tts.
Start here, then execute `queued-input/queued-input.md` incrementally. No agents or commits were used to prepare this plan.

## 1. Goal and decisions

One input pipeline for keyboard and local clients. Users can type while an assistant is streaming or executing tools, submit multiple messages, and trust that accepted input is neither silently discarded nor duplicated. External input must never simulate keyboard events, change window focus, execute slash commands, or answer authorization questions.

Two FIFO queues: **next step** and **after turn**. A drain appends every eligible message separately, in admission order, then makes ONE model request for that batch. It does not make one request per queued message. There is no artificial small message-count cap: enforce documented byte/count safety limits with explicit rejection, never eviction.

### Terminology — important

- **Step:** one provider inference and the complete tool batch it requested, including authorization pauses and all results. The safe boundary is after the batch is finalized, before the next inference. A tool-free final answer is also a step boundary.
- **Turn:** the logical assistant response chain from a submitted batch through tool continuations to a terminal answer. Next-step steering extends that chain. After-turn messages wait until it really finishes.
- Not a token boundary, not each tool call, not `Stream.Active == false`, and not a UI mode. Current code resets contexts per inference; do NOT use that as the logical turn identity.
- Already-authorized tool work is not interrupted by a queued instruction. This v1 does not cancel remaining tools in a batch to steer sooner.

### Keyboard and display

| Context | Enter | Alt+Q | Ctrl+C |
|---|---|---|---|
| Idle, ordinary composer | Submit normally | Submit normally | Clear nonempty draft; else flush queues; else existing idle no-op |
| Assistant streaming/tool running | Queue next-step draft | Queue after-turn draft | Clear nonempty draft; else flush both queues if nonempty; else abort current turn |
| Authorization/question component | Existing answer semantics | Do not steal component input | Existing component cancel semantics |

Alt+Enter ALREADY inserts a newline; preserve it. Alt+Q has no binding in the inspected app/keymap/help/textarea sources. Confirm terminal delivery in tests. Preserve Ctrl+H help, editing, paste, wrapping, and scrolling while busy. Empty submissions do nothing. Clear a draft only after admission succeeds. A rejected enqueue leaves the draft intact.

Queue flush marks pending entries `discarded`, does not abort the current turn, and cannot retract a batch already committed at a boundary. Announce how many were cleared. A subsequent Ctrl+C with empty composer and empty queues retains turn cancellation. In component mode, do not reinterpret an approval as chat; external requests can still enqueue safely.

Rightmost end of the existing status row, above the composer: `3 next step · 2 after turn`. Hide zero groups, abbreviate to `S:3 T:2` at narrow widths. Reserve right-side space, truncate left notification, never exceed terminal width. Counters count pending messages, not batches. Preserve the row during history search/components; do not displace existing model/token footer. Update help and key binding descriptions together.

## 2. Verified code map and hazards

Reviewed working tree at Squid commit `b6397f0`, WITH existing uncommitted changes. Do not overwrite or revert those changes: `internal/chat/{context.go,context_test.go,loop.go,stream_state.go}`, `internal/run/run.go`, untracked nudge/final tests and thinking-only plan, plus the session-config-migrator skill edit. Recheck status before coding.

| Current code | Evidence and implementation seam |
|---|---|
| `internal/app/input.go` | `handleStreamingKey` drops ordinary typing; Ctrl+C aborts. `handleChatKey` edits, pastes, handles completions and Alt+Enter newline. Extract/reuse composer handling without enabling destructive session commands while busy. |
| `internal/app/stream.go` | `sendMessage` appends user input, calls `PrepareTurn`, clears/blurs composer. `setStreamMode` resets UI stream on every request. Keep composer focus and draft across continuations; do not reset the queue here. |
| `internal/app/stream.go` | `handleToolEvent(ToolEventDone)` calls `startStream`; this is the tool-batch steering boundary. `handleStreamEvent(LoopDone)` is the terminal boundary after final assistant append. Error/cancel branches are NOT success boundaries. |
| `internal/chat/tool_worker.go` | Worker owns session mutation until Done/NeedAuth/Error/Cancelled. Running events do not mean worker yielded. NEVER append queue messages to Session from socket/UI while worker runs. |
| `internal/chat/tool_exec.go` | Tool batches track pending/running/results; authorization instructions can finalize remaining tools and return CapturedUserText. Preserve this behavior. |
| `internal/app/update.go`, `events.go` | Bubble Tea message loop + channel wait commands. Add submission/boundary messages here, with session identity and run generation to reject stale events. Non-key textarea updates currently only occur in ModeChat. |
| `internal/app/app.go`, `ui_session.go`, `tui_session.go` | Model, session owner, startup, session switching. Queue belongs to session identity, not to the whole terminal globally. |
| `internal/chat/session.go` | `Append` resolves file refs and updates tokens; may perform I/O. `BuildContext` takes the next request snapshot. `BeginTurnCtx` replaces context each inference. |
| `internal/chat/loop.go` | TUI does NOT use RunLoop; headless run does. Preserve headless behavior and uncommitted thinking-only nudge work. Share ingestion helpers, not a forced full loop rewrite. |
| `internal/ui/notification.go`, `internal/app/render.go` | RenderStatusLine already accepts a right-hand chip, passed empty today. Its overflow logic must be fixed when adding counters. |
| `internal/ui/help.go`, `app/keymap.go`, `app/command.go` | Help is separately authored; update both. Match busy-command guards against execution state, not just visible overlay mode. |
| `internal/cli/tui.go`, `cli.go` | launchTUI constructs tea.Program then Run. No existing running-TUI send endpoint found. `--prompt` prefills a NEW TUI; it is not an IPC API. |

### Confirmed authorization duplication (bonus bug is real)

`chat.StartToolExec` appends `CapturedUserText` on ToolExecDone; `app.handleToolEvent` appends the SAME text again on ToolEventDone. `chat.RunLoop` has its own single append for headless execution. This is not merely rendering.

Read-only inspection of `2026-10-16_14-30-15/chat.json` found adjacent identical stored user messages `msg_94/msg_95` and `msg_419/msg_420`. `chat/context.go` projects every RoleUser message, so both copies can reach the model. Do not modify historical sessions or deduplicate by text: repeated text can be legitimate.

Fix first: preserve the worker's single append and remove TUI's second append, with integration tests through worker + app handler. Prefer carrying appended message ID/index for render invalidation instead of a misleading appendable text payload. Cover approved+instruction, rejected+instruction, plain yes/no, session serialization and provider context. Preserve RunLoop's once-only path. Verify autosave captures the single instruction at the boundary, not only the earlier tool checkpoint.

## 3. Queue ownership and scheduling

Create a small transport-independent `internal/inputqueue` domain package (new) for entries, FIFO selection, stable request IDs, dedup receipts, limits, and state transitions. It must not import GTK, X11, Bubble Tea, or provider implementations. `internal/app/submission.go` (new) coordinates it with the TUI/session. Both keyboard and socket call the same application admission function.

Entry fields: request ID, server sequence, session ID, source, text, boundary step/turn, admitted timestamp, state, injected message ID when known. Generate IDs for keyboard submissions too. Use a logical run generation independent of UIStream.ID. On admission, capture target session identity and working directory; never retarget old requests when session changes.

Execution states: idle, streaming, tools-running, awaiting-auth, paused-error, paused-cancel, draining. An overlay is separate presentation state, not proof of idle. ToolEventNeedAuth holds the logical turn open. Socket input is accepted while waiting for auth, but NEVER approves or dismisses it. Pending entries remain visible. Keyboard answers stay in the component; composer drafting resumes after it closes.

Boundary rules:
1. Tool batch done: worker finished all mutations; persist/checkpoint final tool result; snapshot/drain pending next-step entries; append each exactly once; refresh tokens/render; issue one continuation. After-turn entries stay pending.
2. Normal terminal inference: append/finalize assistant first. If step queue nonempty, drain it and continue the logical turn (after-turn queue still waits). Otherwise end logical turn, drain ALL after-turn entries into one new turn. This prioritizes steering, not global FIFO across both queues.
3. Idle: accepted request of either boundary begins normal submission, with no artificial wait for a future step. Never overwrite an existing typed draft when a socket request starts work.
4. Arrivals after a drain's admission sequence cutoff wait for the next eligible boundary. Admission and selection are serialized by the application coordinator.
5. Cancel/error/limits: retain pending queues and mark scheduling paused; do NOT silently run them after failure/cancel. Add explicit `/queue-resume` and `/queue-clear`; new prompts while paused join pending work, not implicitly bypass the pause. Resume at idle admits the pending step group first, with after-turn group waiting for its completion. Display paused status.
6. Load/new/clear/incognito/fork: never move queued instructions to another session. For v1 block identity-changing actions while pending entries exist and explain how to flush/resume. On quit persist pending work; reopening that session shows it paused until explicit resume. Switching with empty queues is allowed; socket requests carrying old identity are rejected, not redirected.

Keep acceptance fast. Socket parsing and disk I/O cannot occupy the Bubble Tea loop. Persistence runs in a serialized queue-owned worker; completion returns a Tea message. Session mutation at injection waits until no tool worker owns it. File/media resolution is staged off the UI thread from captured session inputs, then committed at a safe boundary. Preserve existing @file/capability processing; don't introduce a separate stripped-down voice prompt path. External text is USER input, never a slash-command execution or authorization answer.

## 4. Durability and acknowledgements

Normal sessions: queue journal/receipt state is durable independently of optional chat autosave. Store per immutable session identity under Squid's configured state area, directories 0700, files 0600. Persist text only there (not to debug logs). Design and test the crash transaction before claiming reliability:

- `accepted` only after durable admission; transport receipt alone is not accepted.
- States: pending → prepared-for-injection → injected; or pending → discarded. Paused is scheduler state. Preserve receipts for retry deduplication across restarts.
- A boundary transaction assigns stable message IDs and records the selected IDs/text before mutating history. Checkpoint session history with those request IDs BEFORE marking injected or starting inference. Persisting admitted queue messages requires an explicit minimal queue checkpoint even when chat autosave is off; document this behavior in help/settings. No automatic wiping of unsaved conversation.
- Recovery reconciles prepared IDs against the persisted session checkpoint: present means already injected, absent means still pending. Recover paused, never run tools automatically after a crash. Test crash before append, after append/checkpoint, before receipt update.
- Crash-safe queue snapshot/journal updates use atomic replace and fsync as needed. Disk full/permission errors return rejection or paused-error, never false accepted. Keep the typed draft/client outbox.
- Incognito: memory-only queue and receipts, response includes `durability: memory`; no disk transcript copy. Client must not claim durable delivery or silently spool incognito text. Normal Sponge targets non-incognito by default.
- Same request ID + same target/text/boundary returns the existing receipt. Same ID + different payload is `id_conflict`. Identical text with DIFFERENT IDs remains two intentional messages.
- Exactly-once HISTORY insertion within retained receipt state, NOT an exactly-once guarantee for model requests/tool side effects across crashes. Publish receipt retention (default session lifetime; explicit pruning only). A discarded request stays discarded on retry.

Do not grow two competing stores that each think they own pending state. Queue owns admission/receipts; persisted chat owns injected user messages; the explicit transaction/reconciliation connects them.

## 5. Local socket and CLI — proposed API, not existing commands

Unix-domain stream socket, no TCP listener. Default runtime discovery under `$XDG_RUNTIME_DIR/squid-os/`, private to UID. Validate ownership, permissions, socket type, and Linux peer UID; reject symlinks/insecure runtime directory. Use short instance IDs in socket filenames to respect Unix path limits. One socket per TUI process, with a descriptor containing instance ID, current session ID, cwd, pid and protocol version. Publish only after event loop is ready; remove own descriptor/socket on exit. Never unlink another live process's socket. Bind an acceptance generation to the session descriptor.

Proposed commands:
- `squid-os input list --json`
- `squid-os input send --instance ID --session ID --id UUID --when step --stdin --json`
- `squid-os input status --instance ID --id UUID --json`
- `squid-os input clear --instance ID --session ID --json` (explicit user action only)
- `squid-os input resume --instance ID --session ID --json`

Implement through the existing typed CLI command pattern. JSON protocol v1, newline-delimited frames, escaped newlines inside text. Example:

```json
{"version":1,"op":"submit","request_id":"uuid","session_id":"session-uuid","when":"step","source":"hex-sponge","text":"Please check the test first."}
```

```json
{"version":1,"request_id":"uuid","status":"accepted","durability":"disk","session_id":"session-uuid","when":"step","sequence":42,"queue_counts":{"step":3,"turn":2}}
```

Status reports accepted/pending/prepared/injected/discarded/paused and injected message IDs where applicable; accepted is NOT a completed assistant answer. Reject malformed/empty/oversize payloads, unsupported version, wrong session, ambiguous target, full queue and unavailable receiver with structured codes. Set frame limits (proposed 256 KiB/request, 16 MiB pending/session), deadlines and bounded connection concurrency; reject excess explicitly. No clipboard/shell interpolation. Read transcript on stdin, not argv. Keep protocol errors off the model transcript.

The socket handler sends an application request to Bubble Tea and awaits a bounded acceptance result; it never touches chat.Session directly or uses key emulation. Retrying after a timeout reuses the SAME ID. Durable accepted messages stay owned by the server even if client disconnects. Uncertain replies are resolved using status/retry, not a second ID.

Discovery: if exactly one compatible normal TUI exists, use it; if multiple, use explicit saved instance/session selection or return `ambiguous_target`. Never guess by terminal title, directory substring, newest pid or first socket. Revalidate session identity immediately before submit. No agent child sessions auto-selected.

## 6. Hex Sponge integration

Current files in `<hex-repo>/hex-enter`: `hex_sponge.py`, `hex_enter.py`, `hex_enter_tray.py`, `install.sh`, service units and squid icon. Existing working tree is uncommitted; don't overwrite unrelated `config/opencode.jsonc`.

**Verified locally:** `hex dictation start`; `hex dictation stop --print` waits for transcript and prints INSTEAD OF PASTING; exit 4 means no speech. This supersedes ALL earlier claims that Linux cannot suppress paste. Do not use `labs.copy_only`, clipboard interception, focus stealing or history polling to reconstruct SQUID transcripts.

Tray remains OFF / ENTER / SQUID. Native Hex Ctrl+Alt+Space untouched. SQUID uses its own Ctrl+Shift+Space: key-down shows center squid badge and starts Hex, key-up stops with --print. Badge is a non-focusable GTK window; Gtk.main must run in main thread. Subprocesses/socket waits in background workers. No sleeps in tool-driven live tests. Current query_keymap polling detects this chord; no need for guessed XRecord/xinput APIs.

Replace only SQUID `deliver()` (currently xdotool --window, whose successful exit did NOT deliver to TUI) with socket CLI submission `when=step`. Keep ENTER-mode xdotool Return behavior separate. Never reintroduce keyboard synthesis for SQUID. One stable UUID per recording, minted before send, retained across retries. No-speech sends nothing. Do not fall back to native paste on failures. A mode change during recording cancels or finishes the owned CLI capture without pasting, according to explicit state; default cancel. On stop failure don't claim idle while Hex might still record: cancel/check status and surface failure.

Delivery states displayed on badge/tray: Listening → Transcribing → Sending → Queued/Submitted; errors remain visible and transcript is retained in a private durable outbox. Close normal badge after acceptance, NOT merely subprocess exit. Accepted/injected/discarded receipts are meaningful; preserve request ID in UI/log metadata. Bound retries, stop on permanent rejection, and expose manual retry for pending entries. Pending outbox entries from old prototype lack stable IDs/receipts: migrate as MANUAL-REVIEW, never automatically replay them.

### Start Squid when absent

Use discovery first. If no compatible instance exists, obtain per-user launch lock, recheck, then launch a normal interactive TUI in a terminal via configured argument array (GNOME default: `gnome-terminal -- squid-os tui`). Add a proposed `--input-instance` launch token flag so readiness can identify OUR new process deterministically. Do not use `run`, `-p`, shell eval, or put transcript in process arguments. Child inherits normal user configuration; use explicit configured cwd if needed, not a guessed repo. Poll discovery with bounded backoff in a worker (e.g. 15s configurable), then submit with that instance's session identity. Readiness means socket + live TUI accept path, not process existence. If launch fails, times out, or another ambiguous instance appears: retain outbox, visible error, no typing. Existing TUI in auth/error/stream state is not 'absent' and must not spawn another terminal.

Setup remains `hex-enter/install.sh`, called by monolith. Verify dependencies BEFORE service startup; no silent sudo. Install client code alongside app, restart changed services (enable --now does not reload running code), leave source uncommitted until explicit user instruction. Fix tray refresh returning False (it disables periodic polling), old toggle helper ignoring mode file, and outdated left-click claims while touching integration. Right-click mode picker is the verified control.

## 7. Execution and test gates

Implement in this order: authorization regression → queue/admission + durable receipts → busy composer + boundaries → socket/CLI → Sponge delivery/launch → end-to-end hardening. Do not refactor all of Squid or alter provider internals unnecessarily. Generated tasks point back here for contract details.

Use fake provider/tool channels and temp socket directories, no paid model calls. Automated tests must cover:
- typing during streamed tokens AND a blocked tool; event loop still accepts many submissions;
- FIFO batch of 3 step messages before next inference; 2 turn messages only after logical turn ends; arrival during drain cutoff;
- stream, tool-running and awaiting-auth busy states; no user message inserted between unresolved tool calls/results;
- terminal with pending step AND turn queues; no lost drafts when remote submission begins/ends;
- Ctrl+C clear/flush/abort precedence; overlays keep authorization behavior; narrow counter layout and help;
- errors/cancel/stale events retain and pause queues; session load/fork/clear/incognito policy;
- duplicate auth instructions stored and projected once, using a synthetic regression fixture (no private session text in tests);
- duplicate delivery ID, conflicting ID, ack loss, reconnect, queue limit, invalid UTF-8/JSON/version, wrong UID/session, stale socket, two instances;
- disk error and crash windows, idempotent recovery, accepted vs injected status, discarded retry;
- Sponge fake Hex CLI yields Unicode/newlines/leading hyphens, empty/no-speech, failure, repeated key-down/release, mode switch, unavailable/ambiguous TUI, simultaneous launch;
- email/editor focus and clipboard unchanged through SQUID capture/delivery, no duplicated text; real terminal is not activated;
- one manual test while Squid is producing a response and another while awaiting tool approval; voice goes to queue, not approval input.

Suggested automated checks: `go test ./internal/inputqueue ./internal/app ./internal/chat ./internal/ui ./internal/cli ./internal/localinput`; targeted `go test -race` on new queue/transport and worker integration; `go test ./...`; Python unittest + py_compile + bash -n in Hex repo. New package names are proposed, not current APIs. Avoid running unrelated expensive suites until targeted tests pass. Separate actual baseline failures from new regressions.

Definition of done: shared queue works without socket first; socket acceptance proves durable ownership, not xdotool success; voice capture is acknowledged in running TUI with no focus changes; no-startup path opens a normal terminal; repeated sends neither vanish nor duplicate; all shortcuts/help/counters match. No auto commits or live transcript replay.

# EPIC: Reliable queued input and Hex Sponge delivery
Why: Allow keyboard and voice messages during assistant work without lost input, duplicate history, focus stealing, or transport-specific hacks.
Outcomes: Shared step/turn queues; durable receipts; responsive TUI; private local socket; acknowledged voice delivery and normal-TUI startup. Contract: ../queued-input-design.md.

## MILESTONE: 1 - Reliable in-turn user input
Pattern: Single-owner application coordinator and transport-independent queue domain
Objective: Fix duplicate authorization input and implement durable step/turn scheduling plus busy composer UX.
Success: Keyboard queue behavior works independently of IPC, with crash recovery and no session-worker mutation races.
Diagram: flowchart LR
Composer --> Admission
Admission --> Pending
Pending --> SafeBoundary
SafeBoundary --> Session
Session --> Inference

### TASK: 1.1 - Fix authorization instruction double insertion
Type: bug
What: Remove the second TUI append of worker-owned authorization instructions and add a worker-to-TUI regression test.
Why: Stored duplicate messages already reach provider context; fix ownership before extending ingestion.
Files: ~ internal/app/stream.go
Files: ~ internal/chat/tool_worker.go
Files: + internal/app/authorization_instructions_test.go
Files: + internal/chat/tool_worker_test.go
Snippet: // Follow ../queued-input-design.md; existing symbols are documented there.
Acceptance: Approved and rejected decisions with instructions produce one stored user message and one provider projection.
Acceptance: Plain yes/no adds no instruction; remaining-tool cancellation semantics stay unchanged.
Acceptance: Headless RunLoop still appends once; checkpoint and render invalidation include the instruction.
Acceptance: Do not rewrite historical sessions; use synthetic fixture inspired by msg_94/msg_95 and msg_419/msg_420 in the referenced session.
Verification: go test ./internal/app ./internal/chat

### TASK: 1.2 - Add shared admission and durable queue receipts
Type: feature
What: Introduce session-bound step/turn queues and an application submission coordinator with durable admission and recovery.
Why: Keyboard and IPC must share ordering, validation, deduplication and persistence semantics.
Files: + internal/inputqueue/queue.go
Files: + internal/inputqueue/store.go
Files: + internal/inputqueue/queue_test.go
Files: + internal/inputqueue/store_test.go
Files: + internal/app/submission.go
Files: ~ internal/app/ui_session.go
Files: ~ internal/config/session.go
Files: ~ internal/chat/session.go
Snippet: // Follow ../queued-input-design.md; existing symbols are documented there.
Acceptance: Implement entries, server sequence, request IDs, boundary selection, state receipts and limits per design sections 3-4.
Acceptance: Same ID/same payload is idempotent; changed payload rejects; repeated text with new IDs remains distinct.
Acceptance: Acceptance waits for durable journal write; errors retain client draft/outbox; incognito explicitly reports memory durability.
Acceptance: Crash-window tests reconcile prepared/injected stable IDs with session checkpoint; recover paused with no automatic inference.
Acceptance: Session document backward compatibility and normal autosave-off queue checkpoint behavior are tested.
Acceptance: Queue store owns receipt state; no background socket mutation of chat.Session; I/O does not freeze UI.
Verification: go test ./internal/inputqueue ./internal/chat ./internal/config
Verification: go test -race ./internal/inputqueue

### TASK: 1.3 - Enable busy composer, queues, counters and help
Type: feature
What: Route keyboard submissions through shared admission while streaming and running tools, and display both queue counters.
Why: Typing must remain responsive and share the exact semantics later used by voice.
Files: ~ internal/app/input.go
Files: ~ internal/app/update.go
Files: ~ internal/app/stream.go
Files: ~ internal/app/keymap.go
Files: ~ internal/app/render.go
Files: ~ internal/ui/notification.go
Files: ~ internal/ui/help.go
Files: + internal/app/queued_input_test.go
Files: + internal/ui/notification_test.go
Snippet: // Follow ../queued-input-design.md; existing symbols are documented there.
Acceptance: Enter queues next step when busy and submits when idle; Alt+Q queues after turn; Alt+Enter still inserts newline.
Acceptance: Draft, editing, paste, wrap, blink and scroll work while tokens/tools run; accepted enqueue clears only its own draft.
Acceptance: Ctrl+C precedence is clear draft then flush pending queues then cancel active turn; auth component keys are unchanged.
Acceptance: Rightmost status counters reflect pending entries and paused state; narrow widths truncate safely without shifting footer.
Acceptance: Remote-style admission preserves a partially typed draft; help describes all shortcuts and auth-overlay exception.
Acceptance: Admission rejection leaves text and shows error; empty submission is no-op.
Verification: go test ./internal/app ./internal/ui

### TASK: 1.4 - Drain batches only at safe lifecycle boundaries
Type: feature
What: Connect the scheduler at tool-batch completion and normal terminal completion, with explicit busy/paused state and generation checks.
Why: Avoid inserting prompts into unresolved tool calls or accidentally launching queued work after cancellation.
Files: ~ internal/app/submission.go
Files: ~ internal/app/stream.go
Files: ~ internal/app/events.go
Files: ~ internal/app/app.go
Files: ~ internal/app/tui_session.go
Files: ~ internal/app/command.go
Files: + internal/app/queue_boundary_test.go
Snippet: // Follow ../queued-input-design.md; existing symbols are documented there.
Acceptance: Drain all step entries FIFO once after entire tool batch, before one continuation; never append while worker owns session.
Acceptance: Step entries at terminal boundary extend current logical turn; after-turn batch waits for terminal with no pending step entries.
Acceptance: Arrivals after cutoff wait for next boundary; no duplicate inference when admissions and Done arrive together.
Acceptance: Errors/cancel/limits pause queues; /queue-resume and /queue-clear are explicit and documented.
Acceptance: Awaiting authorization stays busy; incoming prompts never approve a tool or replace instructions.
Acceptance: Stale generation/session events cannot drain another session; session-switch/new/clear/incognito with pending input is blocked clearly.
Acceptance: Preserve existing dirty thinking-only/headless code; fake channels exercise slow tool and streaming responsiveness.
Verification: go test ./internal/app ./internal/chat
Verification: go test -race ./internal/app ./internal/inputqueue

## MILESTONE: 2 - Local input gateway
Pattern: Ports and adapters with idempotent request-reply
Objective: Expose the shared queue to local clients without key simulation or direct session mutation.
Success: A client receives durable receipts while TUI is idle, streaming, executing tools or awaiting authorization.
Diagram: flowchart LR
Client --> PrivateSocket
PrivateSocket --> ApplicationAdmission
ApplicationAdmission --> Queue
Queue --> Receipt
Receipt --> Client

### TASK: 2.1 - Serve private local submissions through the TUI coordinator
Type: feature
What: Add versioned Unix-socket request handling and bridge validated submissions into Bubble Tea admission.
Why: A transport must enqueue safely, not synthesize Enter or bypass authorization.
Files: + internal/localinput/protocol.go
Files: + internal/localinput/server.go
Files: + internal/localinput/server_test.go
Files: ~ internal/cli/tui.go
Files: ~ internal/app/events.go
Files: ~ internal/app/update.go
Files: ~ internal/app/submission.go
Snippet: // Proposed protocol and lifecycle contracts: ../queued-input-design.md sections 4-5.
Acceptance: Private runtime directory, 0600 socket, peer UID validation, symlink refusal and bounded frames/connections/deadlines.
Acceptance: Submit/status/clear/resume use the common queue and session identity; status distinguishes accepted from injected.
Acceptance: Ack occurs only after persistence result; dropped reply plus same-ID retry cannot duplicate or resurrect discarded input.
Acceptance: Malformed, oversized, wrong-version, wrong-session, full and closing receiver errors are structured.
Acceptance: Auth/picker visibility does not bypass queue rules; socket thread never appends or reads mutable session state directly.
Acceptance: No paid API calls in tests; fake program admission proves responsiveness while a tool worker blocks.
Verification: go test ./internal/localinput ./internal/app
Verification: go test -race ./internal/localinput ./internal/inputqueue

### TASK: 2.2 - Add input CLI, instance discovery and lifecycle cleanup
Type: feature
What: Implement input list/send/status/clear/resume commands and publish live per-instance descriptors from normal TUI startup.
Why: Sponge needs reliable selection and receipts instead of matching terminal titles.
Files: + internal/cli/input.go
Files: + internal/cli/input_test.go
Files: + internal/localinput/discovery.go
Files: + internal/localinput/discovery_test.go
Files: ~ internal/cli/cli.go
Files: ~ internal/cli/tui.go
Snippet: // Proposed protocol and lifecycle contracts: ../queued-input-design.md sections 4-5.
Acceptance: CLI follows typed Command pattern; --stdin preserves Unicode/newlines and never exposes transcript in argv.
Acceptance: Default only when one compatible normal instance exists; multiple instances require explicit selection.
Acceptance: Descriptors published after accept path ready, contain live session identity; stale files never masquerade as ready.
Acceptance: Proposed --input-instance launch token identifies a newly opened normal TUI; no prompt/autonomous-run misuse.
Acceptance: Exit cleans only owned socket/descriptor; session switch rejects stale target; no path-length overflow.
Acceptance: CLI exit codes and JSON results distinguish permanent rejection, timeout/uncertainty, unavailable and accepted.
Verification: go test ./internal/cli ./internal/localinput

### TASK: 2.3 - Prove durable local-input delivery across failures
Type: feature
What: Add queue/socket/TUI integration coverage and document the client contract and recovery behavior.
Why: Resolve crash and disconnect semantics before enabling hands-free submission.
Files: + internal/localinput/integration_test.go
Files: + internal/app/input_delivery_test.go
Files: + docs/local-input.md
Files: ~ internal/ui/help.go
Snippet: // Proposed protocol and lifecycle contracts: ../queued-input-design.md sections 4-5.
Acceptance: Disconnect before/after acceptance, retry across server restart and prepared/checkpoint crashes preserve at-most-one history insertion.
Acceptance: Pending queues reload paused; receipt state survives clear; incognito never persists queued text.
Acceptance: Two instances, session changes, auth pending, disk failure and queue overflow produce explicit non-destructive outcomes.
Acceptance: Document accepted versus injected versus assistant completion; no claim of exactly-once model/tool execution.
Acceptance: Document limits, privacy, queue commands, startup and recovery; run targeted race tests and full Go suite without altering unrelated dirty work.
Verification: go test ./internal/localinput ./internal/app ./internal/inputqueue
Verification: go test -race ./internal/localinput ./internal/inputqueue
Verification: go test ./...

## MILESTONE: 3 - Hands-free Hex Sponge delivery
Pattern: Acknowledged local client with durable outbox and explicit capture state machine
Objective: Replace unreliable X11 delivery with shared-queue submissions and launch a normal TUI when absent.
Success: Voice is captured without pasting or focus changes, accepted by the intended queue, and safely retained on failure.
Diagram: flowchart LR
Shortcut --> HexCLI
HexCLI --> Outbox
Outbox --> Discovery
Discovery --> InputCLI
InputCLI --> QueueReceipt
QueueReceipt --> Badge

### TASK: 3.1 - Deliver captured voice through acknowledged queue requests
Type: feature
What: Replace SQUID-mode xdotool delivery with the new input CLI and a request-ID-based outbox.
Why: Successful synthetic key events did not reach the TUI; only an application receipt proves admission.
Files: ~ <hex-repo>/hex-enter/hex_sponge.py
Files: + <hex-repo>/hex-enter/squid_client.py
Files: + <hex-repo>/hex-enter/test_squid_client.py
Files: + <hex-repo>/hex-enter/test_hex_sponge.py
Snippet: // In <hex-repo>; follow ../queued-input-design.md section 6. No xdotool SQUID delivery.
Acceptance: Keep native Hex shortcut untouched; SQUID Ctrl+Shift+Space controls start and stop --print, no global history replay.
Acceptance: Badge is non-focusable and GTK main loop remains responsive; subprocess and IPC work stays outside UI thread.
Acceptance: One stable UUID per capture persists before send; requests use when=step and intended session identity.
Acceptance: Retry timeout/uncertain reply with same ID; query receipt; permanent rejection and ambiguity retain outbox visibly.
Acceptance: Show Listening/Transcribing/Sending/Queued or Submitted; never claim delivery from process exit alone.
Acceptance: Handle no-speech exit 4, Unicode/newlines, mode changes during capture, stop failure and repeated recordings.
Acceptance: Legacy outbox entries require manual review; never replay old x-events-sent or pending records automatically.
Acceptance: No clipboard changes, terminal activation or simulated typing in SQUID; no model subprocess spawned for delivery.
Verification: cd <hex-repo> && python3 -m unittest discover -s hex-enter -p "test_*.py"
Verification: cd <hex-repo> && python3 -m py_compile hex-enter/hex_sponge.py hex-enter/squid_client.py

### TASK: 3.2 - Launch normal Squid TUI when discovery is empty
Type: feature
What: Add serialized terminal launch and bounded readiness discovery to the client.
Why: Hands-free messages must work when Squid is absent without opening duplicate or autonomous sessions.
Files: ~ <hex-repo>/hex-enter/squid_client.py
Files: ~ <hex-repo>/hex-enter/test_squid_client.py
Files: ~ <hex-repo>/README.md
Snippet: // In <hex-repo>; follow ../queued-input-design.md section 6. No xdotool SQUID delivery.
Acceptance: No compatible live instance: acquire per-user launch lock, recheck, launch configured terminal argv with normal squid-os tui and unique --input-instance token.
Acceptance: No transcript in argv; no run or prompt-prefill workaround; preserve standard auth/configuration.
Acceptance: Poll readiness in background with bounded backoff and timeout; socket publication alone is not proof of admission readiness.
Acceptance: Two simultaneous senders launch once; multiple pre-existing instances require explicit target rather than guessing.
Acceptance: Existing busy/auth-paused TUI is not absent; launch failure/timeout retains original UUID and transcript with visible error.
Acceptance: Selected session identity is revalidated before submit; a changed session rejects rather than silently rerouting.
Verification: cd <hex-repo> && python3 -m unittest discover -s hex-enter -p "test_*.py"

### TASK: 3.3 - Integrate setup, tray controls and end-to-end acceptance
Type: feature
What: Finish dependency checks, service refresh and tray state consistency, then document and test the complete workflow.
Why: The implementation must remain self-contained and repeatable after install or restart.
Files: ~ <hex-repo>/hex-enter/install.sh
Files: ~ <hex-repo>/hex-enter/hex_enter_tray.py
Files: ~ <hex-repo>/hex-enter/hex-enter-toggle
Files: ~ <hex-repo>/README.md
Files: + <hex-repo>/hex-enter/test_integration.py
Files: ~ docs/local-input.md
Snippet: // In <hex-repo>; follow ../queued-input-design.md section 6. No xdotool SQUID delivery.
Acceptance: Dependency checks precede startup; setup deploys new client module and explicitly restarts updated services without silently invoking sudo.
Acceptance: OFF/ENTER/SQUID remain the only modes; fix GLib refresh False return and legacy enabled-file-only toggle; remove incorrect left-click claims.
Acceptance: Fake Hex and fake input CLI tests cover capture-to-queue and errors without paid model calls or live keystroke injection.
Acceptance: Manual acceptance: editor content/focus/clipboard unchanged; real TUI receives one message while streaming and while tool executes.
Acceptance: Batch three step and two turn submissions; verify counters, order, one request per eligible batch and correct Ctrl+C flush behavior.
Acceptance: Test approval pending, repeated voice captures, no TUI launch, two-instance refusal, restart/retry and queue persistence.
Acceptance: Run focused tests before broad suites; record actual results, known limitations and baseline failures; no automatic commit.
Verification: cd <hex-repo> && bash -n hex-enter/install.sh
Verification: cd <hex-repo> && python3 -m unittest discover -s hex-enter -p "test_*.py"
Verification: go test ./...

# queued-input reviews

## 1.1 Fix authorization instruction double insertion
- A (ninfer/qwen3.8-27b): PASS — 3 minor notes: (1) RunLoop once-only path tested via ExecuteTools copy, not the real loop; (2) checkpoint test relies on undocumented persistAutoSave skip with empty Paths; (3) appendedIdx derived after Append rather than returned from mutation site
- B: skipped by user (no codex/openai review for this plan)
- Resolution: all 3 findings minor, none accepted as blockers; deferred to final user review. No fix round needed.

## 1.2 Add shared admission and durable queue receipts
- A (ninfer/qwen3.8-27b): FAIL — 9 findings: dir fsync swallowed (major), weak crash-window test (major), sync I/O on caller goroutine (major), non-UUID session IDs accepted (minor), string-based error codes (minor), Discard ignoring errors (minor), Pending aliasing (minor), rollbackAdmit swallowing Load errors (minor), unwired coordinator/scope deviation (minor)
- B: skipped by user (no codex/openai review for this plan)
- Resolution: all findings accepted and fixed in fix rounds. During fix rounds a self-deadlock was found in AdmitSubmission (caller held c.mu while waiting on the worker that needs c.mu) — restructured so the lock is released before the worker handoff; Close() made idempotent via sync.Once + channel close. ReconcileInjected had a value-copy bug (mutating a copy of the entries slice, journal never updated) — now mutates the loaded slice directly. Final state: go test ./internal/inputqueue ok (incl -race), targeted app submission tests ok (incl -race). Note: full `go test ./internal/app` avoided during dev because it is heavy and blocked the machine repeatedly; remaining app-package tests to be run once at milestone end.

## 1.3 Enable busy composer, queues, counters and help
- A (ninfer/qwen3.8-27b): PASS — 10 minor notes: idle Alt+Q whitespace strip (fixed), dead itoaUI helper, unused QueueCounts export, ToolCh proxy in isBusy (verified niled on drain), weak idle-send test assertion, turn-path coverage gap, partial Ctrl+C abort coverage, narrow chip overflow (fixed), tautological glob line, busy typing skips kill-ring capture (deliberate scope)
- B: skipped by user (no codex/openai review for this plan)
- Resolution: fixed the two substantive minors (idle Alt+Q now sends raw draft like Enter; RenderQueueChip hard-caps at reserved width). Remaining minors deferred to final user review. Note: executor agents burned budget twice on this task; wiring was completed inline by the orchestrator against the design contract. Targeted tests green (app + ui); full app suite deferred to milestone end due to machine load.

## 1.4 Drain batches only at safe lifecycle boundaries
- A (ninfer/qwen3.8-27b): FAIL — 7 findings: pause-before-autosave ordering on error/cancel (major, deferred: journal+pause are both durable, worst case = one extra paused state after crash, recovery reconciles), /queue-resume idle drain stranded without inference (major, FIXED: now calls startStream directly), resume test didn't verify stream start (major, FIXED: asserts ModeStreaming), prepared-without-IDs linger pre-reconciliation (minor, by-design crash window covered by ReconcileInjected), LoopDone-cancel path untested (minor, FIXED: added TestLoopDoneCancelledPausesQueues), bare ToolEventDone can't catch worker-ownership regression (minor, noted), block notification suggested /queue-resume which doesn't unblock (minor, FIXED: message now says /queue-clear first)
- B: skipped by user (no codex/openai review for this plan)
- Resolution: 3 majors fixed in fix round; remaining minors documented and deferred to final user review. Executor burned budget; boundary wiring + tests completed inline by orchestrator against the design contract. All targeted tests green (app/ui/inputqueue); full app suite still deferred to milestone end due to machine load.

## 2.1 Serve private local submissions through the TUI coordinator
- A (ninfer/qwen3.8-27b): NOT RUN — budget constraint; verified mechanically instead: unit tests (accept/reject/oversize/status/list/clear/resume/concurrent, -race) + LIVE E2E: real binary launched detached with --input-instance, socket published, CLI send→accepted(disk,seq,counts), status→pending, list, clear→"1 entry affected", duplicate-ID idempotent. All exit codes correct.
- B: skipped by user (no codex/openai review for this plan)
- Resolution: completed inline by orchestrator (user-approved exception after executor failures burned budget). Files: internal/localinput/{protocol,server,server_test}.go, internal/app/local_input.go, cli wiring in tui.go (--input-instance flag). Design deltas: peer-UID validation deferred (SO_PEERCRED needs raw syscall; socket is 0600 per-UID runtime dir which already isolates); frame limit 256KiB enforced; stale-socket steal guarded by dial probe.

## 2.2 Add input CLI, instance discovery and lifecycle cleanup
- A (ninfer/qwen3.8-27b): NOT RUN — same budget exception as 2.1.
- B: skipped by user
- Resolution: completed inline. Files: internal/cli/input.go (+tests, helper). Commands: input list/send/status/clear/resume; --stdin text (never argv); auto-discovery (single live instance default, ambiguous_target on multiple, explicit --instance); exit codes 0 accepted / 2 permanent rejection / 1 uncertain-unavailable; timeout → "uncertain: retry with same --id". E2E-verified live.

## 2.3 Prove durable local-input delivery across failures
- A (ninfer/qwen3.8-27b): NOT RUN — budget exception (same as 2.1/2.2).
- B: skipped by user
- Resolution: crash-window/dedup/persistence proofs already exist in inputqueue + app coordinator tests (TestCrashWindows, TestCrashRecoveryReconcilesPreparedAgainstCheckpoint, discards-stay-discarded, incognito memory-only) and were re-run green; live E2E covered disconnect/retry semantics via the CLI. Documented client contract + recovery behavior in docs/local-input.md (accepted vs injected, no exactly-once model guarantee, limits, privacy, known limitations). Full targeted suites green incl. -race on localinput + inputqueue.

## 3.1 Deliver captured voice through acknowledged queue requests
- A (ninfer/qwen3.8-27b): FAIL (round 1) — 7 findings: multi-instance ambiguity unhandled in deliver() (major), dead discovery code (minor), dead RETRYABLE_STATUSES constant (minor), defensive receipt-trust gap (minor), tautological retry test (minor), hardcoded /home/goglue fallback bin path (minor), duplicated mode-changed persistence in Sponge.stopped (minor)
- A (ninfer/qwen3.8-27b): PASS (round 2, re-review of fix diff) — 3 minor notes: zero-instance path session probe unscoped (deliberate legacy behavior), retry-test "3" comment slightly confusing, FakeClient.resolve_instance duplicates client logic (controller contract tested, client-side covered by test_squid_client.py)
- B: skipped by user (no codex/openai review for this plan)
- Resolution: accepted #1 (multi-instance) + #5 (tautology). Fix round: deliver() now resolves instance explicitly via find_instances/resolve_instance — multi → visible rejected:ambiguous_target outbox entry, no send; single → instance threaded to current_session/send/status; zero → legacy. Retry test de-tautologized (asserts recorded ids == outbox id); 3 new tests added (single/multi/zero). Deferred minors: dead discovery helpers kept as task 3.2 seam (launch/find_instances gain consumers there), RETRYABLE_STATUSES removal deferred, hardcoded bin fallback deferred, stopped() duplication deferred (Sponge GTK path only, controller is the tested unit). Verify: 33/33 tests green, py_compile clean.

## 3.2 Launch normal Squid TUI when discovery is empty
- A (ninfer/qwen3.8-27b): PASS — 6 minor notes: dead LaunchTimeout class (controller distinguishes timeout via wait_for_ready False), double status roundtrip on launch path, session-changed test stubs client detection (end-to-end detection only indirect), race test proves recheck-under-lock not true concurrent flock contention, terminal_argv fallback to x-terminal-emulator when no candidate found, receipt query parity with 3.1 single-instance path (informational)
- B: skipped by user (no codex/openai review for this plan)
- Resolution: PASS first round; all minors deferred (none blocking). Verify: 49/49 tests green, py_compile clean.

## 3.3 Integrate setup, tray controls and end-to-end acceptance
- A (ninfer/qwen3.8-27b): PASS — 4 minor notes: hardcoded /home/goglue squid-os fallback in install.sh (overridable via SQUID_OS_BIN), finish_capture runtime_dir param is test-only wiring (production defaults preserve 3.1/3.2 behavior exactly), launch tests monkeypatch module-level OS-boundary seams (real lock/recheck/backoff/session-revalidation logic still exercised), legacy enabled-file → ENTER default preserved (pre-existing semantics)
- B: skipped by user (no codex/openai review for this plan)
- Resolution: PASS first round; all minors deferred. Executor burned budget once (harness written against wrong interface — discovery pointed at real runtime dir, no launch seams, fake CLI contract bugs); fix round rewrote the harness to drive real controller + real client against a fake CLI shim with temp runtime dir. docs/local-input.md edit DROPPED per user scope rule (stt-tts only). Verify: 60/60 tests green, bash -n clean, py_compile clean. Manual acceptance checklist (8 real-TUI items) recorded in README.md — outstanding for user.

# Handoff — queued-input milestone 3 (fresh session start here)

## FIRST: load the plan-runner skill and follow it
Load @skill:plan-runner, resume this plan (queued-input). It is the orchestrator:
dispatch executors via inline_agent with the task-executor skill — do NOT write
code inline in the main session. Verify mechanically, review, record, commit
(only on explicit user instruction), mark-done.

## State
- M1 + M2: done, uncommitted in squid-os tree (see git status; includes stray
  internal/app/settings.json and zz_dbg_test.go to clean before committing).
- Task 3.1: CODE COMPLETE + VERIFIED (30/30 tests green), NOT reviewed, NOT committed.
- Tasks 3.2, 3.3: not started.
- Reviewer B (codex/openai): DISABLED for this plan (.plan.json reviewers.b = null).
  Single reviewer A (ninfer/qwen3.8-27b); record "B: skipped by user" in reviews.md.

## HARD SCOPE RULE
Milestone 3 touches ONLY /home/goglue/src/stt-tts. ZERO writes to squid-os
(user enforced: "dont touch squid here !!! zero!!!"). The docs/local-input.md
edit listed in task 3.3 files is DROPPED.

## Task 3.1 remaining cycle steps
1. Review A on the stt-tts diff (or skip if user accepts test proof).
2. Record verdict in .squid-os/plans/queued-input/reviews.md.
3. Commit in stt-tts ONLY after explicit user instruction (standing rule: no
   auto-commit; ideally after user tested).
4. progress.py mark-done --task-id 3.1

## What 3.1 built (all in ~/src/stt-tts/hex-enter)
- squid_client.py: gateway client (discovery dial-probe of
  $XDG_RUNTIME_DIR/squid-os/*.sock or ~/.run/squid-os; send/status via
  `squid-os input ...` CLI, text on stdin only; exit codes 0 ok / 1 uncertain
  retry-same-id / 2 permanent reject; env overrides SQUID_OS_BIN,
  SQUID_RUNTIME_DIR; launch() seam already exists for 3.2).
- hex_sponge.py: xdotool delivery REMOVED. DeliveryController state machine:
  UUID per capture persisted to outbox BEFORE send; when=step; bounded retries
  (MAX_ATTEMPTS=3) with same id; receipt query ONLY on uncertain (never after
  permanent rejection — resurrection hole fixed); badge states; no-speech
  exit 4 → quiet close, no outbox entry; mode-changed-during-capture →
  persisted as 'mode-changed', not delivered; legacy statuses
  {x-events-sent, pending-squid-delivery} never replayed.
- test_squid_client.py (13) + test_hex_sponge.py (17) — all green, headless-safe.
- install.sh deploys squid_client.py.

### Bugs found/fixed during 3.1 (do not regress)
- update_outbox infinite loop (appended to iterated list) → builds new list.
- deliver(): whitespace-only transcript must be idle + no outbox entry
  (if not text.strip()).
- _finalize: receipt query only when code != OK and code != REJECTED.

## Task 3.2 spec (medium, ~100-150 LOC + tests)
Launch normal TUI when discovery empty. In stt-tts only:
- squid_client.py: per-user launch lock (flock), recheck live instances after
  acquiring lock, spawn configured terminal argv detached with
  `squid-os tui --input-instance <unique-token>` (NO transcript in argv, no
  run/prompt-prefill), bounded readiness polling with backoff+timeout
  (socket publication alone ≠ admission ready — probe with a status roundtrip).
- Two simultaneous senders → one launch (lock proves it). Multiple pre-existing
  instances → explicit target, never guess. Launch failure/timeout keeps
  original UUID + transcript with visible error. Session identity revalidated
  before submit; changed session rejects.
- Tests: fake binary/temp runtime dir; no real TUI, no paid calls.
- Terminal on this box: gnome-terminal (/usr/bin/gnome-terminal). Binary:
  /home/goglue/src/squid-os/bin/squid-os (not on PATH).

## Task 3.3 spec (small code, mostly polish + manual acceptance)
- install.sh: dependency checks before startup; deploy client module; explicit
  service restart without silent sudo (partly done).
- hex_enter_tray.py: fix GLib refresh returning False (kills timeout), legacy
  enabled-file-only toggle; remove incorrect left-click claims from docstring.
- hex-enter-toggle: align with mode file semantics.
- test_integration.py: fake Hex + fake input CLI cover capture-to-queue and
  errors; no model calls, no keystroke injection.
- README.md (stt-tts) update. NO squid-os docs changes.
- Manual acceptance (user's time): editor focus/clipboard unchanged; real TUI
  receives one message while streaming AND while tool executes; batch 3 step +
  2 turn submissions, counters/order/Ctrl+C flush; approval-pending, repeated
  captures, no-TUI launch, two-instance refusal, restart/retry, persistence.

## Verification commands
  cd /home/goglue/src/stt-tts && python3 -m unittest discover -s hex-enter -p "test_*.py"
  cd /home/goglue/src/stt-tts && python3 -m py_compile hex-enter/hex_sponge.py hex-enter/squid_client.py
  bash -n hex-enter/install.sh

## Progress tracking
  python3 ~/.config/squid-os/skills/plan-runner/scripts/progress.py next-task \
    --progress ~/src/squid-os/.squid-os/plans/queued-input/.plan-progress.json
(3.1 is in_progress; resume at its verify→review→record→commit steps.)

## Post-mortem note
Previous session burned tokens looping identical debug commands twice
(orchestrator grep-loop; then ~60 identical 20s python repro runs while
diagnosing the update_outbox hang). If a command fails the same way twice,
STOP and change approach — never re-run an identical failing command a third time.

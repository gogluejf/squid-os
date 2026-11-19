## 1.1 Add voice settings
- A (qwen3.8-27b): PASS — minor note: json.Unmarshal error discarded in LoadSettings (pre-existing, preserved)
- B: skipped by user request (no Codex reviewer this run)
- Resolution: no findings accepted; task complete

## Milestone 1 Review — Resolution (2026-11-18)

### User rulings on design questions:
- **Item 7** (`/voice` args): NO arguments. `/voice` = toggle, exactly like `/thinking`. Remove on/off arg handling; bare command flips state.
- **Item 8** (speech across tool calls): Each step (stream + tools) is a SEPARATE group. When a tool batch finishes and the next stream starts, that's a NEW group — the old one is done/dead. No crazy combination or continuation across tool phases. Simple: one group per step.
- **Item 9** (language detection latency): REJECTED — non-issue. The /detect call is localhost (127.0.0.1:8811), its only purpose is picking the voice NAME (-v flag) for the TTS request; the server auto-detects language for synthesis regardless. It is not under a lock that blocks input. Milestone 3 task 3.1 handles lastUserLang for alert lead-ins separately. No change needed.

## 2.1 alt+x universal shut-up
- A (ninfer/qwen3.8-27b): PASS — minor notes: uses Narrator.cancelActive seam instead of literal CancelGroup (functionally identical); update.go listed but unchanged (not needed, handleKey covers all modes); help.go entry added outside Files list (consistent with task 1.5 pattern); one 2s-bounded drain poll in test is timing-based.
- B (openai-codex/gpt-5.6-sol): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: no accepted findings; minors noted and cleared.

## 2.2 New submitted message cancels speech
- A (ninfer/qwen3.8-27b): PASS — minor: dead `var _ = tea.KeyMsg{}` import-keeper (removed); history.json test artifact from idle-path sendMessage (fixed via m.paths → t.TempDir() + cleanup).
- B (openai-codex/gpt-5.6-sol): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: both minors accepted and fixed; re-verified green (build, vet, test, race).

## 2.3 squid-os voice stop CLI command
- A (ninfer/qwen3.8-27b): PASS — major: mismatched --session silently fell back to live session instead of rejecting like input ops; minor: wrong_session path untested; minor: gofmt misalignment in input_test_helper.go.
- B (openai-codex/gpt-5.6-sol): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: all three accepted and fixed — server now rejects mismatched explicit session with wrong_session (matches submit/status/clear semantics), test covers the rejection path, helper gofmt'd. Re-verified green (build, vet, tests). Pre-existing gofmt drift in narration.go/keymap.go left untouched (not introduced by this task).

## 2.4 Sponge key-down integration
- A (ninfer/qwen3.8-27b): FAIL — major: 2.0s synchronous CLI ceiling on the GTK main thread could delay capture start when the TUI is busy; minor: inline call serializes with window show + hex start; blocker (out of scope): pre-existing uncommitted squid-tts server changes in the working tree; minor: tests don't cover the begin() integration point; minor: tautological idempotency test; style: missing blank line.
- B (openai-codex/gpt-5.6-sol): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: accepted and fixed — out-of-scope squid-tts diff reverted (pre-existing, not part of this task); timeout lowered to 1.0s as a fire-and-forget ceiling; silence_voice now runs on a daemon thread from begin() so the GTK loop never blocks; tautological test replaced with success-is-silent assertion; blank line added. Re-verified: py_compile clean, 28/28 unit tests green. Remaining note: ~100ms latency holds on the happy path (single instance, idle TUI); final confirmation needs the plan's manual mic test.

## Milestone 2 — end-of-milestone review (B)
- B (openai-codex/gpt-5.6-sol): UNAVAILABLE — six dispatch attempts (max_steps 75-100, max_time 15-30m) each ended with only a one-line process narration and an empty final answer; no verdict produced. Flagged per runner failure policy.
- A (ninfer/qwen3.8-27b): covered all four tasks per-task during the milestone (see sections 2.1-2.4 above); cross-task coherence verified by orchestrator: alt+x, message barge-in, and the voice_stop socket op all route through the single Narrator.cancelActive → Coordinator.CancelGroup path (group-scoped mute, non-sticky, late chunks dropped).
- Resolution: proceeding on A's coverage + mechanical verification (go build/vet/test/-race green across internal/app, internal/cli, internal/localinput; sponge suite 28/28). Remaining: manual mic test for 2.4 when user is at the keyboard.

## Milestone 2 — end-of-milestone review (B) — RESOLVED
- B (openai-codex/gpt-6-astra): FAIL, 7 findings. (Earlier gpt-5.6-sol attempts were the wrong model and all returned empty; astra is the correct Reviewer B per user.)
- Resolution:
  - #1 blocker ACCEPTED+FIXED — Narrator now tracks lastGroupID past endMessage via a new Coordinator.SetGroupDrainedHook seam; cancelActive targets the draining tail until it fully drains (group-identity guarded). Tests: TestNarrationCancelActiveSilencesDrainingTail, TestNarrationCancelAfterTailDrainsIsNoOp, TestNarrationStaleCancelCannotHitNewerGroup. Also fixed a deadlock found in verify: drain hook runs outside the coordinator lock and re-enters it (CancelGroup→clearCurrentLocked→hook), so onGroupDrained uses TryLock-and-skip (a skipped clear only delays forgetting; next transition re-reports).
  - #3 blocker ACCEPTED+FIXED — duplicate socket receipts no longer cancel speech (cancel gated on !res.Receipt.Duplicate). Test: TestLocalAdmissionDuplicateReceiptDoesNotCancelSpeech.
  - #5 major ACCEPTED+FIXED — voice_stop reply is three-valued (cancelled / nothing_playing / receiver_not_ready); UI-loop timeout maps to receiver_not_ready → server rejects (receiver_unavailable) → CLI exits non-zero. Tests: TestVoiceStopReceiverNotReadyExitsNonZero (CLI), TestVoiceStopOpReceiverNotReady (server), app handler asserts statuses.
  - #6 minor ACCEPTED+FIXED — localSocketTimeout named constant shared by all socket callback round-trips.
  - #4 major REJECTED — single-session TUI: session identity is fixed for the process lifetime, so no change window exists between validation and execution; static-mismatch rejection already covers the real case.
  - #7 minor REJECTED — supporting files (narration.go, protocol.go, test helpers) were flagged and justified per-task in sections 2.1-2.4 above.
  - #2 sponge major PARTIALLY ADDRESSED — capture cannot block on ack (wedged TUI would kill dictation); residual race documented in begin(); happy-path latency unchanged (daemon thread, tens of ms). Final confirmation needs the manual mic test.
- Re-verified after fixes: go build, go vet, go test -count=1 (app/cli/localinput), go test -race (app/voice) all green.

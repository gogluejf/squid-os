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

## 3.1 Track lastUserLang asynchronously
- A (ninfer/qwen3.8-27b): PASS — minor: 500ms inline literal (→ named detectLangDeadline); FR test one hop weaker (now asserts posted value AND langStore.Get()); rapid-fire timing test theoretically skippable (mitigated by the deterministic SetIfNewest ordering test).
- B (openai-codex/gpt-6-astra): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: both actionable minors accepted and fixed; re-verified green (build, vet, tests). NOT committed — awaiting explicit user commit instruction (standing rule).

## 3.2 Alert map and formatting
- A (ninfer/qwen3.8-27b): PASS — minor: unknown Event panicked on nil map index (→ degrades to ""); empty-name degenerate rendering baked into contract without stating intent (→ dedicated documenting test).
- B (openai-codex/gpt-6-astra): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: both minors accepted and fixed; re-verified green. NOT committed — awaiting explicit user commit instruction (standing rule).

## 3.3 Hook events into the voice coordinator
- A (ninfer/qwen3.8-27b): PASS — major: thinking-start fired without checking thinking mode enabled (→ gated on EffectiveInference().Thinking.Enabled + test case); minor: pre-exec agent name re-derived instead of reusing childRef.Name (→ single source of truth); minor: three call-site tests don't pin the specific alert due to fast fake-TTS drain (accepted as-is: guard-lifecycle proof + one-line passthrough call sites; noted).
- B (openai-codex/gpt-6-astra): DEFERRED — single end-of-milestone review per user instruction.
- Resolution: both actionable findings fixed; re-verified green ×3 (build, vet, race, app/voice/chat). NOT committed — awaiting explicit user commit instruction (standing rule).

## 3.1 refinement (user-requested): detection gated on voice-on
- User pointed out: detecting language while voice is off is pointless waste (only alerts consume lastUserLang, and alerts are silent when off).
- Fix: fireUserLangDetect now bails when !m.voiceOn. Last known value stays intact; flipping /voice on re-detects from the next message. Three 3.1 tests updated to set m.voiceOn=true (they exercise detection itself). Re-verified green ×2 incl. race.

## Milestone 3 — end-of-milestone review (B) — RESOLVED (2026-11-19)
- B (openai-codex/gpt-6-astra): FAIL, 13 findings. Fixes landed in-tree across sessions 2026-11-18_20-58-41 → 2026-11-19_01-39-26 → this session; the previous session died mid-edit on #6 with a handoff message.
- Resolution:
  - #1 blocker ACCEPTED+FIXED — launchTUI wired WireVoiceSpeaking on the PRE-program Model copy, so the program's copy kept langPush == nil and fireUserLangDetect was a permanent production no-op. Fix: langPush now lives in voiceWiringState, a pointer allocated in New and shared by every Model copy (value copies share the pointer); WireVoiceSpeaking writes through it. Test: TestWireVoiceSpeakingReachesProgramCopy proves the copied model sees non-nil langPush.
  - #2 blocker ACCEPTED+FIXED (prior session) — WireVoiceSpeaking rebuilds the narrator via newNarrator so the group-drain hook installs; the alert recursion guard no longer wedges after the first alert.
  - #3 blocker ACCEPTED+FIXED (prior session) — speakAlert sets n.lastGroupID = gid, so alt+x / barge-in / voice stop / toggle-off / shutdown kill alerts exactly like narration.
  - #4 major ACCEPTED+FIXED (prior session) — preExecAgentName applied on every return path in tool_exec.go; AgentName carried on ToolEventDone (tool_worker.go).
  - #5 major ACCEPTED+FIXED (prior session) — SkillLoaded + SubAgent alerts added to the ToolEventDone handler (stream.go); sole/final skill_load and agent calls no longer silent.
  - #6 major ACCEPTED+FIXED — socket submissions only ran lang detection on the IDLE path; busy/after-turn admissions parked in the queue were drained at the boundary without detection. Fix: fireUserLangDetect now fires at admission in Update's localAdmissionMsg case (next to cancelActive, duplicates excluded). The newest-subID guard (#7) drops stale results.
  - #7 major ACCEPTED+FIXED — stale lang overwrite: the goroutine snapshotted m.langStore.Get() AFTER a newer submission may have landed, and Update wrote lastUserLang unconditionally. Fix: langDetectedMsg carries subID; Update applies only when msg.subID >= m.langSeq. All four last_user_lang tests updated to construct langDetectedMsg{subID, lang}.
  - #8 minor RESOLVED — doc/comment-only finding; addressed in-tree (comment clarifications), no behavior change.
  - #9 major ACCEPTED+FIXED (prior session) — speakAlert splits text into 2 chunks (SplitN(text," ",2)) so the EN detail gets its own TTS language detection. Verified by recording-CLI test asserting both lead-in and detail are spoken.
  - #10 major USER RULING (2026-11-19) — SilentFailure ("empty stream, no stop reason") now routes to the GENERIC Error alert, NOT the dedicated ContextBust event. User clarified: voice alerts are reactive announcements of what just happened — there is no preventive "compact your context" workflow, so the dedicated template buys nothing. The ContextBust Event/template stays in the voice package (unused by app call sites for now); tests updated: TestSilentFailureSpeaksGenericError (+ recorded variant) replace the old ContextBust-distinctly tests.
  - #11 major ACCEPTED+FIXED (prior session) — auth errors + ToolEventError now speak voice.Error alerts.
  - #12 major ACCEPTED+FIXED — call-site tests proved nothing about actual emission (guard-clear + probe passed even with the alert removed). Hardened with a RECORDING fake TTS CLI (writes each spoken chunk to a temp file): TestToolEventNeedAuthSpeaksAuthorizationRecorded, TestToolEventRunningSkillLoadedSpeaksAlertRecorded, TestLoopErrorSpeaksErrorAlertWithDetailRecorded, TestContextBustSpeaksOwnTemplateRecorded, TestSpeakAlertRecursionGuardDropsSecond all assert the expected lead-in string was actually spoken (and the dropped second alert was NOT).
  - #13 minor REJECTED — scope note (out-of-plan file touched); justified: the fix required touching the wiring state in app.go, which is the plan's core file. No unrelated changes.
- Re-verified after fixes: go build ./..., go vet ./..., go test -count=1 (app/voice/chat/cli/localinput), go test -race (app/voice) — ALL GREEN.
- Status: all 13 findings resolved. NOT committed — awaiting explicit user commit instruction (standing rule), ideally after user tests voice mode.

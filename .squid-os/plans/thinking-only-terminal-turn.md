# Thinking-only terminal turn → empty child-agent result

**Date:** 2026-11-03 · **Severity:** medium (silent data loss in autonomous sub-agents) · **Status:** unpatched

## Symptom
`inline_agent` (and any autonomous child run) finishes with `status: success` and an
**empty result** when the model's final turn is thinking-only: no visible text, no tool
call. The actual answer (often long and complete) is stranded in the message's
`thinking_text` field.

## Evidence (repro session)
Session `~/.config/squid-os/sessions/2026-11-03_13-31-52/`, children under `agents/`:

| child | final turn | thinking_text | visible text |
|---|---|---|---|
| `inline-call_176c4137d70ae8a2` (reviewer A) | 8,785 out-tok, `stop_reason=None` | 34,776 chars (full review, findings #1–#12) | 0 |
| `inline-call_fd36ebf146d59651` (A retry) | 7,749 out-tok, `stop_reason=None` | 30,713 chars | 0 |
| `inline-call_e67b39225b4501a2` (B retry) | 8,494 out-tok, `stop_reason=None` | 33,630 chars (findings #1–#17 + severity) | 0 (mid-run turns DID emit text: msg_8) |

Same model+config in the MAIN interactive thread shows the same terminal-turn shape
(13:44:00 and 14:01:16 assistant messages: text="", 27k/5k thinking chars) but it is
harmless there — the user just re-prompts.

Notes:
- `stop_reason=None` is the NORMAL finish state for this provider (ninfer/
  qwen3.8-27b) — normal text turns also end `None`. Not a reliable abort signal by itself.
- `thinking: {enabled: true, parse_reasoning_from_text: false}` — the parse flag is
  irrelevant to this bug. `enabled: true` is what creates thinking blocks.
- Model is capable of visible text: mid-run turns emitted text fine (B-retry msg_8:
  "Reading the remaining four critical files..."). The failure is specifically the
  FINAL turn ending inside/after the thinking block.

## Mechanism
1. Model (qwen3.8-27b, thinking on) ends a turn with reasoning only — no text, no tool call.
   Happens randomly; more likely when the prompt demands a strict short final format after
   long deliberation, or when the upstream stream ends mid-think (tails found cut
   mid-sentence).
2. In a child loop, a turn with no tool call is TERMINAL → final answer = turn text = `""`.
3. Engine records `status: success`, no result. Parent sees an empty string.

## Proposed fix (agent loop, NOT provider-side)
In the code that resolves a child/inline run's final answer, before returning success:

```
if finalTurn.text == "" && finalTurn has no tool calls:
    1. if finalTurn.thinking_text != "":
         nudge ONE synthetic user message:
         "Your final answer must be visible text. Output it now — no reasoning."
         take the nudge turn's text as the result if non-empty.
    2. if still empty: return thinking_text as the result
       (or surface an explicit "empty final answer" error — decide),
       never a silent success with "".
```

Budget: max 1 nudge (avoid loops). Log which path fired for debuggability.

## Where to look
- The inline/child agent run executor: grep for `child_session`, `thinking_text`,
  `stop_reason` in the runtime loop (see sibling plans `agent-runtime-cli`,
  `agent-subsession`, `subagent-session-tree` for architecture context).
- Existing precedent for similar stream glitches: `ttft-buffered-provider-bug.md`.

## Repro recipe
Model `ninfer/qwen3.8-27b`, thinking enabled. Dispatch `inline_agent` with a reviewer-style
prompt: strict verdict format after ~9 tool calls on a large diff (the prompt is preserved
in the evidence session above). Run it a few times — thinking-only terminal turns appear
intermittently.

## Out of scope / known-good
- TUI vs CLI: NOT a display bug — `text: ""` is already in the persisted `chat.json`.
- Config propagation: settings changes DO reach new child launches (verified:
  `parse_reasoning_from_text` flipped old `true` → new `false` between runs).

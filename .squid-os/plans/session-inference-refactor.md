# Session Inference Refactor + Session Migration Plan

## Goal
Refactor Squid-OS session inference tracking so model/provider/thinking changes are represented truthfully over time, with a stable initial config snapshot and effective current inference state.

This plan is intended as a **full handoff document** for an agent with **no access to the current chat context**.

---

## Core Problem
Today the app mutates session model/config too early:
- model picker immediately updates `session.model`
- `Model Switched` is logged at picker time instead of when the new model is actually used
- `config0` is mutated in place, so it no longer represents the original session config
- loading old sessions and switching model before the next prompt can create misleading metadata
- analytics becomes ambiguous because the session top-level model is not clearly either the initial or effective current model

We want session history to reflect **when a model/thinking/skill change actually takes effect**.

---

## Final Design

### 1. Session inference state
Replace the current flat `provider/model/thinking` session semantics with an explicit inference object:

```go
type InferenceConfig struct {
    Provider string `json:"provider"`
    Model    string `json:"model"`
    Thinking bool   `json:"thinking"`
}

type SessionInference struct {
    Initial InferenceConfig `json:"initial"`
    Current InferenceConfig `json:"current"`
}
```

The session should contain:
- `Inference.Initial` = the config at the moment the session truly began
- `Inference.Current` = the currently effective config for that session

Important:
- do **not** add `next_model`, `next_provider`, etc.
- `settings` serve as the pending desired state
- the session stores only **initial** and **current**

---

## 2. Settings are the pending desired state
When the user changes model/provider/thinking from the UI:
- save it to `settings`
- do **not** immediately mutate the session current inference if the session already has a user message
- do **not** immediately emit `Model Switched`
- the change becomes effective only on the **next user turn**

This differs from skill behavior:
- skill is session/context-based and uses explicit pending session state
- model/provider/thinking should use global settings as the next desired state for upcoming work

### Effective rule
At send time, after user message append and before assistant generation:
- compare `session.Inference.Current` vs current settings
- if different, inject the corresponding transition message(s)
- then update `session.Inference.Current`
- then continue with generation

---

## 3. Transition message placement
This is critical.

### Required order inside a turn
A turn must look like:
1. user message
2. transition messages that became effective for this turn
3. assistant/tool loop messages

This applies to:
- model switch
- thinking switch
- skill-load / skill-unload activation messages

### Why
Because then `ctrl+d` / destroy last sequence remains truthful:
- removing the last user turn also removes the transition messages that were part of that turn
- no stray switch events remain detached from the user input that caused them to take effect

---

## 4. `config0` becomes `Init Config`
The internal config message currently called `config0` must be treated as an immutable initial snapshot.

### Changes
- rename label from `Config` to `Init Config`
- it represents `session.Inference.Initial`
- once the session has a user message, it must no longer be updated in place for model/provider/thinking changes

### Allowed mutation rule
`config0` may only be updated if:
- there is **no user message yet**, and
- the session is not yet effectively started

That means:
- new empty session: OK to update in place while the user is still configuring
- after first user message: freeze forever

### Consequence
After the first user message:
- later model changes must be represented only by `Model Switched`
- later thinking changes must be represented only by `Thinking Switched`
- `config0` remains the original snapshot

---

## 5. Transition messages from now on

### Model switch
Inject when `Inference.Current.Provider/Model` differs from settings at the start of a user turn.

Message shape:
- `Label: "Model Switched"`
- `Params: {"from": "<old model>", "to": "<new model>"}`

Provider does not need to be included in params unless useful later. The minimum requirement is the model transition label already used by analytics.

### Thinking switch
Start emitting a new internal message **from now on**.

Message shape:
- `Label: "Thinking Switched"`
- `Params: {"to": "on"}` or `{"to": "off"}`

Important:
- do **not** include `from`
- only `to` is required
- migration does **not** need to reconstruct old thinking switch history

### Skill switch
Keep same semantics as discussed:
- skill activation/unload message should also be positioned immediately after the user message when it becomes effective for that turn

---

## 6. Migration policy
There is **no backward compatibility requirement**.
We are allowed to rewrite all saved sessions to the new shape.

### Before migration
Create a full folder backup:
- duplicate `sessions` -> `sessions.bck`

This must happen before modifying any session files.

---

## 7. Migration rules for old sessions

### A. Top-level inference reconstruction
Old sessions do not have the new inference object.
We must create:
- `session.inference.initial`
- `session.inference.current`

### B. Provider inference rule
Old sessions do not reliably store provider correctly for migration purposes.
Use this exact rule:
- if model starts with `gpt` -> provider = `openai-codex`
- otherwise -> provider = `vllm`

This rule is accepted for migration simplicity.

### C. Reconstruct `current`
Use the old session model as the old effective model baseline, unless a replay of switch events yields a more accurate final current state.

Recommended reconstruction:
1. determine fallback current from old top-level session model
2. infer provider using the rule above
3. replay `Model Switched` messages in chronological order to determine final effective model
4. final replay result becomes `Inference.Current`

### D. Reconstruct `initial`
Use the first model switch if one exists:
- first `Model Switched` params `from=X to=Y`
- then `Inference.Initial.Model = X`
- provider inferred from X using the migration rule

If there is no model switch message:
- `Initial == Current`

### E. Thinking reconstruction
Do **not** attempt to reconstruct historical thinking switch events.
For migration:
- initialize both `Inference.Initial.Thinking` and `Inference.Current.Thinking` from the old top-level session thinking value
- do not synthesize old `Thinking Switched` messages

---

## 8. Reordering historical messages
We also want historical consistency for switch events and skill-load events.

### Goal
For each turn, reorder so it becomes:
1. user
2. model/skill transition messages associated with that turn
3. assistant/tool loop

### Specifically
Try to migrate these classes of messages so they sit immediately after the relevant user message:
- `Model Switched`
- skill load/unload activation messages

### Important note
This is allowed to be heuristic.
The objective is to make old sessions consistent with the new timeline model, not to preserve byte-for-byte ordering.

A practical migration strategy is:
- identify user-turn blocks
- move transition/internal messages into the beginning of the block that they logically affect

Do **not** try to migrate thinking switch history.

---

## 9. App behavior after refactor

### New session creation
Initialize:
- `Inference.Initial = settings(provider, model, thinking)`
- `Inference.Current = settings(provider, model, thinking)`
- create `config0` as `Init Config`

### Model/provider/thinking changes before first user message
Allowed behavior:
- update settings
- update session current
- update session initial
- update `Init Config`

Reason:
The session has not really started yet.

### Model/provider/thinking changes after first user message exists
Do **not** mutate `Init Config`.
Do not immediately mutate effective session current at picker time.

Instead:
- save new desired state to settings
- on next user message append, compare effective current vs settings
- inject `Model Switched` and/or `Thinking Switched`
- update `Inference.Current`
- continue generation

---

## 10. Files/code areas likely affected
At minimum inspect and update:
- `internal/config/session.go`
- `internal/app/chat_session.go`
- `internal/app/model.go`
- `internal/app/thinking.go`
- `internal/app/stream.go`
- any render/UI code showing config/model details
- analytics code reading `session.model/provider/thinking`

Also add a migration utility/script, likely easiest as a temp Python script or Go utility.

---

## 11. Required session JSON migration result
After migration, each session should have:
- new `session.inference.initial`
- new `session.inference.current`
- `config0` label renamed to `Init Config`
- `config0.params` patched to match `Inference.Initial`
- old ordering of model/skill transition messages improved so they appear after user message when possible

The old mutable interpretation of `config0` must be eliminated.

---

## 12. Explicit non-goals
Do **not**:
- introduce backward compatibility shims
- preserve the old flat model semantics
- add `next_model` / `next_provider` fields
- synthesize old thinking switch history during migration

---

## 13. Suggested execution order
1. duplicate `sessions` -> `sessions.bck`
2. implement new session inference types
3. update runtime behavior for fresh sessions and future turns
4. freeze `Init Config` after first user message
5. implement migration script
6. run migration on saved sessions
7. validate a few representative sessions manually
8. update analytics / UI consumers if needed

---

## 14. Validation checklist
After implementation:
- switching model before first user message updates `Init Config`
- switching model after first user message does **not** mutate `Init Config`
- next user send injects `Model Switched` right after the user message
- next user send injects `Thinking Switched` right after the user message when needed
- `ctrl+d` removes user + switch events + generated assistant work together
- migrated sessions have correct `Inference.Initial`
- migrated sessions have correct `Inference.Current`
- `config0` is renamed `Init Config`
- backup folder `sessions.bck` exists before migration

---

## 15. Notes for the implementing agent
This plan intentionally prefers **truthful session history** over minimal code churn.
If a choice is ambiguous, prefer behavior that makes the transcript reflect the exact moment a configuration became effective.

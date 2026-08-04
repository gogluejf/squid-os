# Runtime Resolution

## Purpose
Define how Squid-OS resolves effective runtime configuration from multiple sources.
This document is a preparation artifact for planning. It is not the implementation plan.

## Why this document exists
The codebase now has multiple runtime input sources:
- global settings
- session metadata
- CLI flags
- future agent definitions
- future sub-agent calls

Today, runtime behavior is partially resolved in different places:
- `main.go`
- TUI startup
- TUI send-time inference updates
- headless/run session creation

That is manageable now, but it will become messy once agents and `run` are first-class.

So the system needs one clean resolution concept.

## Core idea
Runtime resolution is the process of building one effective execution spec from:
1. global settings
2. optional selected agent
3. CLI overrides

This resolved spec should then be used to create `SessionConfig` and any related session metadata/bootstrap state.

## Source precedence
Resolved runtime precedence is:

1. CLI flags
2. selected agent definition
3. global settings

No exceptions are intended at this point.

## Inputs

### Global settings
Provides default values such as:
- provider
- model
- thinking
- base system prompt file
- max tool result tokens
- project/memory/temp/doc paths
- debug mode
- existing autosave-like defaults

### Agent definition
Provides preset runtime values such as:
- agent identity
- agent system text
- model
- thinking
- tools
- skills
- auth mode
- autosave behavior
- limits
- working directory
- memory config
- execution mode

### CLI overrides
Provides explicit per-run overrides such as:
- `--model`
- `--thinking`
- `--system`
- `--tools`
- `--skills`
- `--prompt`
- maybe save/auth/mode/working-dir flags later

## Output
The runtime resolver should produce one effective runtime object.
Exact struct naming can be decided later, but conceptually it should include:
- selected agent identity
- provider
- model
- thinking config
- base system prompt file
- agent system text
- tools
- skills
- active skill
- available agents
- working directory
- autosave config
- auth mode
- memory config
- execution mode
- runtime limits
- max tool result tokens
- debug flag
- prompt payload

## Why this must be centralized
Without a central runtime-resolution layer, the same rules will be duplicated across:
- TUI startup
- `run`
- future `call_agent`
- future server mode
- future nested agent execution

That duplication would create drift in:
- precedence
- defaulting
- autosave/auth semantics
- model parsing
- agent bootstrap behavior

## Relation to session bootstrap
The resolved runtime object should feed `SessionConfig` and related session metadata/bootstrap creation.
It should not bypass session creation with ad hoc behavior.

This keeps the architecture aligned with the current direction:
- runtime resolved once
- session created from it
- loop runs from session

## Turn preparation
Transition injection should not live only in TUI `sendMessage`.
It should move into a shared turn-preparation step used by TUI, CLI run, session continuation, and future agent execution.

Conceptual flow:
1. append user message
2. prepare turn from desired runtime state
3. inject transition messages immediately after the user message
4. update current session runtime state
5. start stream

This shared step owns:
- model switch injection
- thinking switch injection
- active skill switch/load injection
- allowed tools/skills/agents changes where appropriate

Important rule:
- transition messages must remain after the user turn they apply to

`StartStream` should not hide these mutations.
It should start streaming from an already-prepared session.

## Selected agent persistence
Even though a session becomes its own evolving transcript after start, the selected agent should still persist in session metadata.
Reason:
- analytics
- provenance
- session interpretation

So the resolved runtime object should include explicit selected-agent identity, not just flatten agent values away.

## Output mode resolution
Output mode belongs to the resolved runtime object.
Current intended modes:
- `final_message`
- `stream`
- `silent`
- `structured`

Resolution decides the intended mode.
Execution/rendering then implements or rejects accordingly.

For this phase:
- `structured` should likely resolve successfully but fail at execution with explicit not-implemented behavior
- `silent` and `stream` should be treated as realistic phase targets

## Auth mode resolution
Auth mode belongs to the resolved runtime object.
It is a policy mapping layer.

Important distinction:
- TUI can support interactive ask behavior
- non-TUI execution cannot rely on user interaction mid-run

So the runtime resolver may normalize agent auth mode into an effective run policy compatible with the execution environment.

## Autosave resolution
Autosave belongs to the resolved runtime object.
This is not the same as manual session saving.

Distinction:
- autosave = whether the run/session auto-persists steps
- manual save = explicit user/session management action

Current code already supports in-memory sessions; the unresolved part is when and how runloop-owned execution autosaves automatically.

## Working directory resolution
Working directory should resolve through existing app concepts, not a new parallel abstraction.
If not explicitly set:
- current working directory remains the natural default

This is especially important because memory namespace `workspace` depends on working directory semantics.

## Model parsing
Agent and CLI may use combined provider/model strings such as:

```text
openai/gpt-5.5
```

Runtime resolution must parse and normalize these consistently into:
- provider
- model

That parsing should not be reimplemented independently in multiple layers.

## Session metadata consequences
Because more runtime concerns are becoming explicit, session metadata will likely need to persist more than today.
Conceptually relevant session metadata families now include:
- inference
- selected agent
- available agents
- available tools
- available skills
- memory config
- autosave config
- auth mode
- limits

Exact final struct layout should be decided carefully to keep metadata coherent.

## TUI vs run
The resolved runtime layer should serve both:
- TUI boot
- `run`

This matters because the new CLI model changes behavior:
- bare `squid-os --prompt` now means TUI with prefilled textarea
- explicit `run` becomes the non-TUI path

So the same runtime concepts must survive across both, even if they are consumed differently.

## Sub-agent implications
Phase 1 clarified direction:
- `call_agent` is phase 1 as a blocking self-call to the CLI
- not full child-session orchestration yet

That makes the runtime resolution layer even more important, because sub-agent execution can simply reuse the same resolved execution contract rather than inventing a second one.

## Planning consequences
This layer is not optional.
It is the clean boundary that prevents spaghetti between:
- settings
- agent definitions
- CLI flags
- session creation
- loop execution

It should be treated as one of the key preparation artifacts before generating the implementation plan.

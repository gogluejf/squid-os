# Memory Foundation

## Purpose
Define the first-phase memory foundation for Squid-OS.
This document is a preparation artifact for planning. It is not the implementation plan.

## Scope
This phase is about structure and configuration, not a full memory engine.

In scope:
- memory config concepts
- session/runtime metadata support
- namespace/path semantics
- environment exposure
- internal package scaffolding

Out of scope as primary work:
- advanced recall engine behavior
- retrieval heuristics
- summarization engine logic
- autonomous memory writes beyond the needed structural hooks

## Current code reality
Today memory already exists in a minimal form:
- settings define `memory_dir`
- paths resolve a global memory directory
- environment loader reads `memory/index.md`
- environment displays `[Memory]`

What does **not** exist yet:
- session-level memory config
- agent-level memory config in runtime/session objects
- internal memory package as a domain layer
- workspace/agent namespace semantics in session bootstrap

## Core direction
Memory becomes a first-class runtime/session configuration concern.
For this phase it is mostly about:
- where memory belongs
- how it is named
- how it is exposed to the environment/runtime
- how it persists in session config

## Namespace model
Current intended namespace concept includes:
- `workspace`
- `global`
- `agent`

### `workspace`
- tied to working directory
- always based on the current working directory concept
- should reuse the app’s existing working-directory behavior
- if no explicit working directory is set, the current folder default should apply as the app already does
- resolved memory path should live under the workspace/project itself

Path shape:

```text
<working-directory>/memory
```

This is the most important namespace rule clarified so far.

### `global`
- tied to the configured global memory directory
- conceptually independent of a workspace path

Path shape:

```text
<settings.memory_dir>
```

Resolved today through the existing configured memory path, e.g. `~/memory`.

### `agent`
- scoped to the selected/root agent
- should live under Squid-OS config, not under the workspace
- should be represented in config now even if engine behavior is deferred

Path shape:

```text
~/.config/squid-os/agents/<agent-name>/memory
```

This keeps agent memory near the agent definition and distinct from both workspace and global memory.

## Agent memory config
Agent definitions may include memory configuration such as:
- namespace
- instructions
- journal settings
- summary settings

This should flow into runtime/session config even if engine behavior is shallow in phase 1.

## Session-level memory config
Memory should live in session configuration / session metadata as part of the resolved runtime state.
This matters because:
- environment generation may need resolved memory paths
- later memory behavior should be session-aware
- runs should be reproducible and analyzable

## Internal package
A dedicated internal package/folder should exist now.

Proposed concept:

```text
internal/memory
```

Purpose in phase 1:
- memory config types
- namespace/path helpers
- any light helper logic needed for resolution/bootstrap

This package should not try to become the full engine immediately.

## Environment role
Memory must be reflected correctly in the generated environment.
That includes:
- resolved memory namespace
- resolved memory path
- memory instructions
- memory visibility as part of runtime context
- compatibility with current environment injection model

The environment should make memory config explicit enough for the model to understand the intended memory behavior, even before the full memory engine exists.

Expected environment content should include:
- namespace, e.g. `workspace`, `global`, or `agent`
- resolved path, e.g. `<working-directory>/memory`
- instructions, if configured
- existing memory index content when available

The current environment already includes memory index content.
The new design should extend this coherently rather than replacing it.

## Working directory dependency
Memory foundation depends heavily on working directory semantics.
That is a good thing, because the app already has a strong working-directory concept.
The memory layer should consume that existing concept rather than inventing a second workspace abstraction.

## Relation to runtime resolution
Memory config is not standalone.
It must be part of the resolved runtime object built from:
- settings
- optional selected agent
- CLI overrides

That resolved config then feeds session bootstrap and environment rendering.

## Relation to session bootstrap
Memory should be present in the runtime/session config such that environment generation can expose the right paths and mode information.
This does not require the full memory engine to be implemented yet.

## Non-goals for this phase
This phase should explicitly avoid scope explosion into:
- advanced memory retrieval
- memory ranking/search pipeline
- agentic writeback behavior beyond basic structure
- large persistence strategies not required for the foundation

## Planning consequences
The memory work should be treated as a clean foundation layer:
- config types
- namespace semantics
- helper package
- environment integration
- session/runtime persistence

Not as a full feature-complete memory engine.

# Agent Tools

## Purpose
Define the first-phase tool surface related to agents.
This document is a preparation artifact for planning. It is not the implementation plan.

## Scope
This document focuses on tool-level agent interactions, not the full agent registry or CLI model.

Relevant phase-1 tools/concepts:
- `list_agents`
- `call_agent`
- `inline_agent`

## Why this doc exists
Agent execution can happen through:
- direct CLI usage
- future scheduler usage
- tool-driven delegation from inside a session

So the tool contracts should be explicit rather than improvised later.

## `list_agents`
Purpose:
- return the list of available installed agents
- mirror the spirit of `skill_list`

Expected behavior:
- lightweight
- always safe
- returns name + description for each available agent

Important distinction:
- installed/global agents come from the registry
- callable agents may be restricted by the current session/agent `Agents` scope

The final implementation should return the callable subset — agents allowed by the current session/agent `Agents` scope.
This mirrors how `skill_list` and tool availability work: the tool shows what is actually usable in the current context, not just everything installed.
If the callable set is empty, the tool can optionally list all installed agents with a clear label indicating they are not callable.

Conceptual output shape:
- human-readable compact list
- same spirit as the current skill registry listing

Why it matters:
- the model must be able to discover available reusable agents before calling one
- this keeps agent discovery explicit rather than hidden in environment only

## `call_agent`
Purpose:
- execute a named installed agent
- blocking in phase 1
- return the final answer to the caller

Phase-1 direction:
- simple implementation
- call the CLI and wait for the response
- do not build deep child-session orchestration yet

Access rule:
- `call_agent` should respect the session/agent available `Agents` scope
- installed agents and callable agents are not always the same thing
- `call_agent` must also check the current agent call depth (inherited from parent limits)
- if depth reaches 0, `call_agent` returns an error instead of executing
- depth decreases by 1 for each nested `call_agent` invocation

This is intentionally a pragmatic bridge.

### Conceptual contract
Inputs:
- agent name
- prompt
- limits

The rest is set internally by the tool/runtime from the called agent and caller context.
Limits are explicit because the caller decides how constrained the delegated subtask should be.

The core simplicity rule:
- it should feel like calling `squid-os run <agent> ...` from inside the system

### Internal behavior for tool-call context
`call_agent` is not a free-form CLI escape hatch.
It should force the pieces that make sense for a blocking tool call:
- output mode should be final answer style
- no stream-mode output to the parent
- prompt comes from the tool argument
- working directory defaults to caller working directory unless overridden
- save behavior follows normal runtime/session config or explicit save override
- called agent uses its own configured tools, skills, agents, memory, system, and model unless normal overrides are intentionally exposed

Important segmentation rule:
- tool/skill/agent scopes should come from the called agent definition, not be implicitly inherited from the caller
- this keeps agent boundaries clean

Access rule:
- the named called agent itself must be allowed by the caller/session available `Agents` scope

### Output
Phase-1 output should be:
- final answer only

Not required yet:
- rich streamed child events
- interactive child control
- multi-session child UX

## `inline_agent`
Purpose:
- run an agent-like profile defined directly in the tool arguments
- avoid requiring registry installation for ad hoc delegation

This is the preferred naming over earlier temporary-agent wording.

### Conceptual contract
Likely inputs:
- prompt
- system
- model
- tools
- skills
- limits
- auth mode
- maybe working directory

Conceptual meaning:
- the agent definition source is inline arguments
- it is not an installed preset from the agents registry
- run it blocking
- return final answer

### Why it exists
`call_agent` is for reusable installed agents.
`inline_agent` is for one-off, ad hoc, local delegation.

Both `call_agent` and `inline_agent` are bounded by the same agent call depth rules
(inherited from parent, decremented by 1, returns error at 0).

That distinction keeps the model clean.

## Relation between the three
- `list_agents`
  - discover reusable installed agents
- `call_agent`
  - execute a reusable installed agent
- `inline_agent`
  - execute an ephemeral inline-defined agent preset

This is a clean and easy-to-teach trio.

## Registry dependency
- `list_agents` depends directly on the global agents registry
- `call_agent` depends on the global agents registry
- `inline_agent` does not require registry lookup for its core definition

## Runtime reuse
All agent tools should reuse the same runtime/CLI/agent execution model as much as possible.
They should not invent a parallel execution path.

This is especially important for:
- model parsing
- auth policy mapping
- save behavior
- working directory semantics
- memory config semantics

## Auth behavior
Agent tools run as blocking tool calls in phase 1.
They should use the same mapped auth behavior designed for non-TUI `run` execution.
No special separate auth system should be invented here.

## Save behavior
Save behavior should map through the same session/runtime configuration logic.
These tools should not invent their own persistence semantics.

## Session behavior
Phase 1 should avoid overcomplicating tool-driven agents with child-session orchestration.
For now:
- block
- execute
- return final answer

Child-session visibility and orchestration are later concerns.

## Environment relation
Even with `list_agents`, the environment should still list available agents the same way it lists skills.
The tool is not a replacement for environment visibility.
It is an executable discovery primitive.

## Planning consequences
This doc implies explicit planning work for:
- agent tool registration
- list output contract
- blocking CLI-backed execution path for `call_agent`
- inline runtime contract for `inline_agent`
- reuse of runtime resolution and save/auth behavior

## Non-goals for phase 1
- streamed child-agent event protocol
- deep child-session TUI routing
- concurrent child agent orchestration
- full structured child result handling

# Sub-Agent Execution

## Purpose
Define the first-phase sub-agent execution direction.
This document is a preparation artifact for planning. It is not the implementation plan.

## Important scope clarification
Full child-session orchestration is **later**.
For this phase, sub-agent execution should prioritize simplicity and reuse.

## Core direction
The first implementation of sub-agent calling should be blocking and simple.
It should reuse the CLI/agent/runtime design rather than introducing full in-process multi-session orchestration immediately.

In your clarified direction:
- sub-agent process child orchestration is later
- for now `call_agent` should call the CLI, effectively calling the system itself in a blocking way

## Tool concepts
Two related concepts exist in the notes:
- `inline_agent`
- `call_agent`

### `inline_agent`
Purpose:
- run an agent-like profile defined directly in the tool arguments
- avoid requiring registry installation for ad hoc delegation

Conceptually:
- the agent definition source is inline arguments
- not an installed preset from the agents registry
- good for ad hoc delegation

### `call_agent`
Purpose:
- execute a named installed agent
- blocking for phase 1
- return final answer to caller

Phase 1 direction:
- call CLI rather than implementing deep child in-process orchestration immediately

## Why CLI self-call is acceptable in phase 1
This approach is acceptable because it:
- reuses the same command grammar and runtime resolution model
- reduces early architectural branching
- avoids prematurely building child-session multiplexing
- still provides the user-visible value of sub-agent delegation

It is a simplicity-first bridge, not the final architecture.

## Relation to agent registry
`call_agent` depends on the global agent registry.
It should resolve the named agent exactly through the same source of truth as `squid-os run <agent>`.

## Relation to runtime resolution
Sub-agent execution should consume the same resolved runtime concepts as `run`.
That means it should inherit or map:
- model
- tools
- skills
- auth policy
- save behavior
- working directory
- memory config
- execution mode

This prevents sub-agents from becoming an unrelated execution path.

## Auth behavior
Sub-agent execution is non-interactive in this phase.
So auth behavior must be deterministic and based on mapped runtime policy.
No TUI-style mid-run user approval should be assumed.

## Save behavior
Save should map through session/runtime config.
This is not the same as manual save.
For phase 1, save behavior should follow the caller/runtime configuration rather than invent a separate sub-agent persistence model.

## Output contract
For phase 1, `call_agent` should return the final answer cleanly.
This aligns with the simplicity goal.

Stream-mode output for sub-agents can come later if needed once the CLI stream boundary is formalized more deeply.

## Why old child-session plans are not phase-1 drivers
There are older plans around:
- child sessions
- multi-session routing
- viewport switching
- in-process session multiplexing

Those remain useful background, but they should not force complexity into the first delivery.
The clarified direction now is:
- blocking
- CLI self-call
- final answer return

## Planning consequences
This means phase 1 sub-agent planning should focus on:
- clean `call_agent` contract
- clean `inline_agent` contract
- reuse of runtime resolution
- deterministic auth/save behavior
- blocking execution path

And defer:
- full child session lifecycle in-process
- concurrent session orchestration
- TUI child-session switching UX
- deeper stream multiplexing

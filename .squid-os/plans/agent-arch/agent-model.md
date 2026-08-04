# Agent Model

## Purpose
Define the first-class agent concept for Squid-OS.
This document is a preparation artifact for planning. It is not the implementation plan.

## Core idea
An agent is a reusable runtime preset.
It is not a skill, not a tool, and not a replacement for the base system.

Conceptual stack:
- **tool** = execution primitive
- **skill** = workflow competence / instructions
- **agent** = role / preset that groups scope, runtime policy, and access

In practice:
- an agent groups model behavior, tool scope, skill scope, limits, auth behavior, save behavior, memory settings, working directory defaults, and agent-specific system instructions
- an agent may expose skills and tools to the runtime
- an agent is closer to an execution profile than to a simple prompt fragment

## Agent is a first-class app concept
Agent should become its own domain concept, similar in importance to skills.
It should not be implemented as:
- a special skill
- a one-off CLI alias
- a prompt hack inside `run`

## Location
Agents live globally in config, using the same folder-based pattern as skills.

Proposed structure:

```text
~/.config/squid-os/agents/
  code-review/
    agent.yaml
    memory/          # agent-scoped memory (later)
  trader/
    agent.yaml
    memory/
```

Each agent is a folder containing an `agent.yaml` definition file.
This mirrors the skills architecture (folder with SKILL.md) and allows
agent-scoped subdirectories (memory, references, etc.) to coexist cleanly.

For this phase:
- agents are global
- project-local agents are out of scope

## Registry model
Agents should mirror the skills architecture pattern as much as practical.

Expected architecture:
- global agents registry
- discovery from config directory
- lightweight list entries
- full load of agent definition by name
- environment listing from registry

Registry concerns likely include:
- name
- description
- file path
- parsed config

## Relation to skills
Agents and skills are different layers.

### Skill
- markdown instruction workflow
- user-extensible
- loaded into session context
- focuses on execution method / competence

### Agent
- runtime preset
- selects or constrains tools/skills/model/limits/etc.
- focuses on role, scope, and runtime behavior

The intended relation is:
- agent groups skills
- skills invoke tools

## Relation to tools
Tools remain app-defined executable primitives.
Agents do not replace tools.
Agents restrict or scope tool access.

## Agent definition format
Each agent is defined in a folder containing `agent.yaml`.
The YAML file carries the full agent preset configuration.
This mirrors the skills pattern (folder with SKILL.md).

Illustrative shape:

```yaml
name: trader
description: Researches financial markets and proposes trades.

mode: final_message

auth_mode: auto

save:
  enabled: true
  name:

limits:
  steps: 100
  tools: 300
  time: 2h
  max_tool_result_tokens: 15000
  max_agent_depth: 5

thinking: true
working_directory: .

system: |
  Research thoroughly before making recommendations.
  Explain the reasoning behind every trade.
  Never invent market data.

model: openai/gpt-5.5

tools:
  - web_search
  - browser

skills:
  - trade
  - portfolio
  - news

agents:
  - researcher
  - code-review

memory:
  namespace: workspace
  instructions: |
    Remember portfolio decisions, investment thesis,
    watchlist, open positions, mistakes and lessons learned.
  journal:
    enabled: true
    max_entries: 500
  summary:
    enabled: true
```

Exact schema should be formalized later, but the concept direction is now clear.

## Agent bootstrap role
The current session bootstrap already injects:
- `sys0`
- `env0`
- `config0`
- `tools0`

Agent should fit into that bootstrap explicitly.

Proposed addition:
- `agent0`

Meaning:
- a system-layer message carrying the agent system instructions
- sourced from the agent file or CLI `--system`

## System layers
There are three system layers and they must remain distinct:

1. **Base System**
   - from app/system prompt configuration
   - internal, bios-like
   - always present

2. **Environment System**
   - generated from environment loader
   - always present

3. **Agent System**
   - from agent file
   - or overridden by CLI `--system`

Agent does not replace base system.
Agent adds or overrides only the agent-specific layer.

## Session persistence implications
Once a session is initiated, the agent is no longer the runtime driver in a strict sense; the session transcript has become the source of truth.
However, persisting the selected/root agent on the session is still useful.

Reason:
- analytics
- log interpretation
- provenance / auditability
- future UX visibility

So the selected/root agent should persist in session metadata.

## Agent vs Agents in session metadata
The singular and plural forms mean different things.

### `Agent`
- singular
- root/owning agent that started the session
- provenance identity
- not `initial/current`
- not switchable like a skill

### `Agents`
- plural
- available callable subagents
- scope/access list used by agent tools such as `call_agent`
- should likely have `Initial` and `Current` shape like `Tools` and `Skills`

This mirrors the existing distinction between:
- `Skill` = active loaded skill
- `Skills` = available skill set

## Available agents in session metadata
Current session metadata already tracks available:
- tools
- skills

By symmetry, the same architecture should also exist for agents.

This suggests session metadata should eventually distinguish between:
- selected/current agent for the session
- available/allowed agents visible to the session

That mirrors the same separation already present for:
- loaded skill
n- available skills

## Agent modes
Agent mode is a runtime-output policy.
Current intended modes:
- `final_message`
- `stream`
- `silent`
- `structured`

Phase direction from clarified answers:
- `final_message`: supported
- `silent`: should be implemented now if easy
- `stream`: likely implementable in this phase at CLI boundary
- `structured`: accepted conceptually but should return not implemented for now

## Auth mode
Agent auth mode is not a brand-new execution subsystem.
It is a mapping layer over existing authorization behavior.

Important constraint:
- outside TUI there is no interactive user-approval flow with instructions
- so non-interactive modes must resolve deterministic behavior

This makes auth mode part of runtime policy, not just TUI UX.

## Save behavior
Save behavior belongs to runtime/session configuration.
It should not remain only a TUI autosave concern.

Important distinction:
- **manual persistent save** is a user/session management concern
- **run/session save behavior** is runtime policy

Agent-level save config controls whether the run persists its session steps automatically.
This applies naturally to `run` and should later inform `call_agent` behavior too.

## Working directory
Agent may define working directory defaults.
This should integrate with the existing working-directory concept already present in the app and tools.
The system should reuse that concept, not invent a parallel one.

## Model field
Agent model should be provider/model-shaped in the config, e.g.:

```yaml
model: openai/gpt-5.5
```

Runtime resolution must parse this into provider + model components cleanly.

## Memory field
Agent can include memory config.
For this phase, memory is mostly structural/configurational:
- namespace
- instructions
- summary/journal toggles
- path exposure through environment/session config

The actual memory engine is not the main scope yet.

## Environment listing
Environment should list available agents the same way it lists available skills.
This is intentionally meant to mirror the existing pattern, not invent a special presentation.

Expected section style:
- name
- description

## Planning consequences
The agent model implies several new clean layers:
- agent registry
- agent definition loader/parser
- selected agent session metadata
- available agents session metadata
- agent bootstrap message injection
- runtime resolution from settings + agent + CLI overrides

This should be implemented as a new clean concept, not as scattered special cases.

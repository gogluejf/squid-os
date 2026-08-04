# Stream Protocol

## Purpose
Define the phase-1 CLI-facing stream protocol direction.
This document is a preparation artifact for planning. It is not the implementation plan.

## Scope
This doc is about the protocol/output surface for streaming runs.
It is not automatically a full redesign of the internal loop/event engine.

## Core direction
For phase 1, stream output should be treated primarily as a CLI boundary contract.
The internal loop may evolve later, but we do not need to over-redesign it just to ship useful stream mode.

## Main question
Should stream output support multiple formats, or always use one?

Current recommended direction:
- always JSONL for stream mode

## Why JSONL
JSONL is the simplest strong default because it is:
- machine-readable
- line-oriented
- stream-friendly
- easy to log
- easy to pipe into other tools
- easy to extend with new event types

It also avoids unnecessary branching such as:
- text stream mode A
- JSON stream mode B
- pretty stream mode C

That would add complexity too early.

## Relation to output modes
Current intended execution/output modes:
- `final_message`
- `stream`
- `silent`
- `structured`

For this doc:
- `stream` means evented output at the CLI boundary
- the recommended output encoding is JSONL

## Event philosophy
The stream protocol should expose meaningful event boundaries rather than raw implementation accidents.
The event model should fit Squid-OS philosophy and the current loop design.

Candidate event families already discussed include:
- message lifecycle
- thinking lifecycle
- tool-call lifecycle
- tool-execution lifecycle
- permission/auth state
- session/internal lifecycle events
- error/finished state

## Possible event set
Illustrative event set:
- `message_start`
- `message_delta`
- `message_end`
- `thinking_start`
- `thinking_delta`
- `thinking_end`
- `tool_call_start`
- `tool_call_update`
- `tool_call_end`
- `tool_execution_start`
- `tool_execution_end`
- `permission_request`
- `session_event`
- `error`
- `finished`

This list is conceptual guidance, not yet a locked implementation schema.

## Event payload conventions
Every JSONL event should include:
- `event`
- `ts`

Only emit fields that have meaningful values for the event type.
Do not include empty/null/zero-value fields in the output.

Where applicable, events should also include:
- `message_id`
- `tool_call_id`
- `role`

Message roles should match current session concepts where relevant:
- `system`
- `user`
- `assistant`
- `internal`
- `synthetic`
- `tool`

The stream protocol should not expose every session message as a raw dump, but when an event represents a message or session lifecycle item, its role should be explicit.

`session_event` should cover internal/synthetic/session lifecycle events such as:
- model switched
- thinking switched
- skill loaded/unloaded
- stream cancelled
- other synthetic/internal transcript events worth surfacing

## Recommended rule for phase 1
Do not redesign the entire internal streaming engine unless truly needed.
Instead:
- define a clean CLI stream protocol
- map current loop/engine behavior into it
- evolve deeper internals only where necessary

## JSONL example: simple text generation
```jsonl
{"event":"session_start","ts":"2026-07-20T14:33:00Z","saved":true,"session":"2026-07-20_14-33"}
{"event":"message_start","ts":"2026-07-20T14:33:01Z","message_id":"msg_12","role":"assistant"}
{"event":"message_delta","ts":"2026-07-20T14:33:01Z","message_id":"msg_12","role":"assistant","text":"Analyzing the startup path..."}
{"event":"message_delta","ts":"2026-07-20T14:33:01Z","message_id":"msg_12","role":"assistant","text":" I found two likely bottlenecks."}
{"event":"message_end","ts":"2026-07-20T14:33:02Z","message_id":"msg_12","role":"assistant"}
{"event":"finished","ts":"2026-07-20T14:33:02Z","stop_reason":"end_turn"}
```

If the run is not saved, omit reusable session identity:

```jsonl
{"event":"session_start","ts":"2026-07-20T14:33:00Z","saved":false}
```

Rules:
- include saved session name only when saved
- do not expose internal session ID
- keep stream stdout as JSONL

## JSONL example: thinking + tool call
```jsonl
{"event":"thinking_start","ts":"2026-07-20T14:34:00Z","message_id":"msg_13","role":"assistant"}
{"event":"thinking_delta","ts":"2026-07-20T14:34:00Z","message_id":"msg_13","role":"assistant","text":"Need to inspect the run loop and startup path."}
{"event":"thinking_end","ts":"2026-07-20T14:34:01Z","message_id":"msg_13","role":"assistant"}
{"event":"tool_call_start","ts":"2026-07-20T14:34:01Z","message_id":"msg_13","role":"assistant","tool":"read_file","tool_call_id":"call_1"}
{"event":"tool_call_update","ts":"2026-07-20T14:34:01Z","message_id":"msg_13","role":"assistant","tool":"read_file","tool_call_id":"call_1","delta":"{\"path\":\"main.go\"}"}
{"event":"tool_call_end","ts":"2026-07-20T14:34:01Z","message_id":"msg_13","role":"assistant","tool":"read_file","tool_call_id":"call_1"}
{"event":"tool_execution_start","ts":"2026-07-20T14:34:01Z","message_id":"msg_13","role":"tool","tool":"read_file","tool_call_id":"call_1"}
{"event":"tool_execution_end","ts":"2026-07-20T14:34:01Z","message_id":"msg_13","role":"tool","tool":"read_file","tool_call_id":"call_1","status":"success"}
{"event":"message_start","ts":"2026-07-20T14:34:02Z","message_id":"msg_14","role":"assistant"}
{"event":"message_delta","ts":"2026-07-20T14:34:02Z","message_id":"msg_14","role":"assistant","text":"The startup path loads settings, initializes paths, then branches into TUI or run mode."}
{"event":"message_end","ts":"2026-07-20T14:34:03Z","message_id":"msg_14","role":"assistant"}
{"event":"finished","ts":"2026-07-20T14:34:03Z","stop_reason":"end_turn"}
```

## JSONL example: auth/policy stop
```jsonl
{"event":"tool_call_start","ts":"2026-07-20T14:35:00Z","message_id":"msg_15","role":"assistant","tool":"bash","tool_call_id":"call_9"}
{"event":"tool_call_end","ts":"2026-07-20T14:35:00Z","message_id":"msg_15","role":"assistant","tool":"bash","tool_call_id":"call_9"}
{"event":"permission_request","ts":"2026-07-20T14:35:00Z","message_id":"msg_15","role":"tool","tool":"bash","tool_call_id":"call_9","policy":"end-on-write"}
{"event":"finished","ts":"2026-07-20T14:35:00Z","stop_reason":"authorization_blocked"}
```

## `silent` relation
`silent` is not stream mode.
It should remain separate.
A silent run may produce no normal final user-facing text output.
That behavior should be defined in the run output contract, not conflated with stream JSONL.

## `structured` relation
`structured` is also separate from stream mode.
For phase 1 it should likely return explicit not-implemented behavior rather than complicating the stream protocol.

## Planning consequences
This doc implies planning work for:
- CLI stream output contract
- event naming
- event payload minimums
- mapping current loop behavior into the CLI event protocol
- explicit examples for users and future tooling

## Non-goal for phase 1
A full internal streaming redesign is not automatically required.
The main goal is a clear and stable external stream contract.

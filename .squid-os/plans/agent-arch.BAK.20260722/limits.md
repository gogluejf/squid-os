# Limits

## Purpose
Define the first-phase treatment of agent/runtime limits.
This document is a preparation artifact for planning. It is not the implementation plan.

## Scope
This is a short document because limits are important, but they should not become an overengineered subsystem in phase 1.

## Core idea
Limits belong to the agent/runtime/session configuration model.
They should be represented clearly now.
Some should be enforced now, and some may be modeled first and enforced later.

## Candidate limits
Current intended limits include concepts such as:
- steps
- tools
- time
- max tool result tokens

Illustrative shape:

```yaml
limits:
  steps: 100
  tools: 300
  time: 2h
  max_tool_result_tokens: 15000
```

## Phase-1 principle
Do not overcomplicate limits.
The right question is:
- which limits are easy and valuable to enforce now?
- which should be modeled now but enforced later?

## Strong candidate for immediate enforcement
### `max_tool_result_tokens`
This already fits current architecture very well.
There is already logic around tool result token limiting, so this is a natural enforced limit in phase 1.

## Likely modeled now, maybe enforced incrementally
### `steps`
Conceptually useful.
Could be enforced through the loop count across assistant/tool cycles.
If not fully enforced in first pass, it should still exist in config.

### `tools`
Conceptually useful.
Could be enforced as maximum executed tool calls for a run/session.
If not fully enforced in first pass, it should still exist in config.

### `time`
Conceptually useful.
Could be enforced via runtime deadline/timeout behavior.
If not fully enforced in first pass, it should still exist in config.

## Why limits still matter even before full enforcement
Even partial enforcement is valuable because limits are part of:
- agent identity
- runtime safety
- future scheduler safety
- future autonomous usage safety

So they belong in the model even if phase-1 enforcement is selective.

## Planning consequence
The implementation plan should explicitly mark, for each limit:
- modeled now
- enforced now
- deferred enforcement

That keeps the scope honest and avoids ambiguity.

# Drop Incognito

## Purpose
Record the cleanup decision around incognito mode.
This document is a preparation artifact for planning. It is not the implementation plan.

## Decision
Incognito mode should be dropped.

## Why
Incognito is becoming redundant once save behavior is modeled cleanly as session/run configuration.
The system should not need a separate legacy bypass mode for concerns that now belong to explicit runtime/session config.

## Replacement idea
The useful part of incognito was mostly:
- no history persistence
- no session auto-persistence
- reduced side effects

Those concerns should be expressed through normal configuration/runtime semantics instead of a special top-level mode.

Especially:
- save behavior becomes a session/run property
- runloop should own save behavior the same way TUI flow does
- manual save remains a separate user action

## Why this is cleaner
Dropping incognito avoids:
- a parallel special-case mode
- extra branching in CLI and app startup
- confusing overlap with save behavior
- legacy semantics surviving after the architecture has moved on

## Scope of removal
The cleanup should remove:
- CLI exposure of incognito
- app/runtime branching based on incognito
- history/session save bypass logic that only exists for incognito semantics

## Important distinction
This does **not** mean all sessions must always persist.
It means persistence behavior should be modeled explicitly through runtime/session config rather than through a special incognito concept.

## Planning consequence
This should be a short explicit cleanup milestone in the implementation plan.
It is not a major architecture doc, just a cleanup decision that should not be forgotten.

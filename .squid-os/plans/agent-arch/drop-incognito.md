# Drop Incognito

## Purpose
Record the cleanup decision around incognito mode.
This document is a preparation artifact for planning. It is not the implementation plan.

## Decision
The incognito **CLI flag** should be dropped.
The in-app incognito toggle (alt+i) remains as a privacy shortcut.

## Why
The incognito CLI flag is redundant — it should not gate startup behavior.
The in-app toggle handles the privacy use case cleanly without special CLI branching.

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
Removing the CLI flag avoids:
- extra branching in CLI and app startup
- confusing overlap with save behavior
- legacy semantics surviving after the architecture has moved on

## Scope of removal
The cleanup should remove:
- CLI exposure of incognito (the `--incognito` flag in main.go)
- ad hoc root-command branching based on incognito startup flag

The in-app incognito toggle (alt+i) **remains**:
- It keeps the session alive and stops saving/history
- Toggle off reloads session from disk (discarding incognito changes)
- It is not removed; it is simply no longer a CLI startup flag

## Important distinction
This does **not** mean all sessions must always persist.
It means the CLI no longer has an incognito startup flag.
Persistence behavior is modeled explicitly through runtime/session config.
The in-app incognito toggle (alt+i) continues to work as a privacy shortcut.

## Planning consequence
This should be a short explicit cleanup milestone in the implementation plan.
It is not a major architecture doc, just a cleanup decision that should not be forgotten.

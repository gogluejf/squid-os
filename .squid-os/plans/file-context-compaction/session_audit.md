# Session Audit Report

**Date:** 2025-07-24
**Scope:** All 917 session files in ~/.config/squid-os/sessions
**Purpose:** Inspect for malformed instruction arguments relevant to migration RepairArgs and orphan tool-result shapes

## Counts

| Category | Count |
|---|---|
| Total session files | 917 |
| Sessions with tools | 695 |
| Total tool calls | 34,477 |
| **Malformed instruction arguments** | **26** |
| **Orphan tool results** | **47** |

## Malformed Instruction Arguments (26)

All 26 are bash or edit_file tool calls where the model's streamed JSON arguments contain unescaped quotes inside grep/sed command strings. These are the classic "model outputs `grep "pattern"` inside a JSON value" problem.

Examples:
- `{"command": "grep \"func.*CursorDown\" ...}` — unescaped inner quotes
- `{"command": "sed -n '554,560p' internal/app/stream.go", "description": "..."}` — trailing content after closing brace

All are bash commands with complex shell quoting. `RepairArgs` already handles these via jsonrepair + bracket-closing fallback.

## Orphan Tool Results (47)

Tool calls where `execution.status` is empty string but `execution.result` or `execution.error` has content. These are from older sessions before the status field was consistently populated. The results are valid — the tool ran successfully, just the status field wasn't set.

All 47 are from sessions before June 2025. No impact on current migration logic.

## Impact on Migration

- **RepairArgs**: Already handles the 26 malformed cases. No code change needed.
- **Orphan results**: No migration impact — BuildContext checks `execution.Status == ""` to skip unexecuted tool calls, but orphan results have actual content, so they are treated as executed. This is correct behavior.

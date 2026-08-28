---
name: mouse-paint
description: Paints multiline block text and named symbols into the active drawing canvas by controlling the system mouse. Use when the user asks to mouse-paint text, card suits, or Squid-OS art at the current pointer position.
version: 1.0.0
allowed-tools: bash
---

## Overview
Provides a simple cross-platform mouse-paint CLI with automatic Linux X11/Wayland, Windows, and macOS backend selection, deterministic bitmap rendering, multiline text, and built-in symbols.

## Variables
- `<skill-folder>` — directory containing this skill and its mouse-paint CLI.

## Instructions
1. Interpret the requested painting literally. Preserve newlines and supported symbol markup such as `[squid]` or `[heart]`.
2. Before every tool call, tell the user briefly what will happen.
3. Run `python3 <skill-folder>/scripts/mouse_paint.py --doctor` before painting when readiness has not already been established in the current session.
4. If doctor reports a missing dependency, quote the exact installation command, ask the user for permission to install it, and do not paint yet.
5. Place no application-specific focus or setup requirements on the CLI. It paints into the active canvas under the pointer.
6. Invoke painting with `python3 <skill-folder>/scripts/mouse_paint.py "<text>"`. Pass `--at X,Y` only when the user explicitly requests coordinates.
7. Treat actual painting as an OS-changing action. In interactive mode, execute only when the user explicitly asks to paint, draw, run, go, or repeat. Never execute merely while discussing or editing the skill.
8. Report success concisely. On failure, return the script error and its remediation without claiming that anything was painted.

## Rules
- Never launch or focus an application in phase 1.
- Never install dependencies without explicit user approval.
- Never expose or ask the user to choose an internal backend; detection is automatic.
- Always leave the primary mouse button released, including after errors.
- Use only supported markup: `[heart]`, `[diamond]`, `[club]`, `[spade]`, and `[squid]`.
- Preserve literal newline characters in multiline requests.
- Do not call the painting script unless the user explicitly requests execution.

## Output Format
```
Status: <painted | ready | dependency-required | failed>
Details: <concise result, exact install command, or error>
```

## Examples
Input: Paint `Squid OS`, then the squid underneath.
Action: Run `python3 <skill-folder>/scripts/mouse_paint.py $'Squid OS\n[squid]'`.
Output: `Status: painted`

Input: Can this paint card symbols?
Action: Explain support without running the script.
Output: `Status: ready`

Input: Paint `[heart] [diamond] [club] [spade]`.
Action: Check doctor if needed, then run the CLI only after readiness is established.
Output: `Status: painted`

## Resources

### Scripts
- [mouse_paint.py](scripts/mouse_paint.py) — Executable script

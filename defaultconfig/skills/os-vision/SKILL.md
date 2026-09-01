---
name: os-vision
description: "Gives the AI eyes on your X11 desktop: list windows, find them by name, take screenshots of specific windows or regions, inspect what processes are running in terminal tabs, and detect monitor layout. Requires Linux with X11."
allowed-tools: bash read_file inspect_media open
---

## Overview
A global skill that provides OS-level visual access to the X11 desktop. It can enumerate all open windows, search them by regex, capture screenshots of individual windows or pixel regions, identify which processes are running in terminal tabs, and report monitor geometry. All output is JSON for easy AI parsing. The skill detects session type and dependencies on first use, and gracefully declines on non-Linux or non-X11 systems.

## Variables
- `<skill-folder>` — directory containing the generated skill's SKILL.md
- `<tmp>` — the session's temporary directory for storing screenshots and scratch files

## Instructions

1. **First call — dependency check:**
   Run `python3 <skill-folder>/scripts/os_vision.py check`
   - If it returns `{"ok": false, ...}`, show the user the error and install commands. Do not proceed.
   - If `{"ok": true}`, continue.

2. **List windows:**
   Run `python3 <skill-folder>/scripts/os_vision.py list`
   Returns all visible windows with id, title, position, size, and pid.

3. **Find windows by name:**
   Run `python3 <skill-folder>/scripts/os_vision.py find "<regex>"`
   Returns matching windows. Use this when the user references an app by name (e.g. 'screenshot GIMP').

4. **Screenshot a window:**
   - By ID: `python3 <skill-folder>/scripts/os_vision.py screenshot --id <window_id>`
   - By name: `python3 <skill-folder>/scripts/os_vision.py screenshot --name "<app name>"`
   - Full desktop: `python3 <skill-folder>/scripts/os_vision.py screenshot --full`
   - Region: `python3 <skill-folder>/scripts/os_vision.py screenshot --region <W>x<H>+<X>+<Y>`
   Output is saved to `<tmp>/os-vision-<timestamp>.png`. The script prints the file path.

5. **Inspect a screenshot (only when user asks to see/analyze it):**
   Use `inspect_media` with the screenshot path and a query like 'Describe this image'.

6. **Open a screenshot (interactive mode only, when user says 'open it'):**
   Use `open` with the screenshot file path.

7. **Window info (process, CWD, tree):**
   Run `python3 <skill-folder>/scripts/os_vision.py info --id <window_id>`
   Returns pid, cwd, process tree, and child processes.

8. **Monitor layout:**
   Run `python3 <skill-folder>/scripts/os_vision.py monitors`
   Returns connected displays with position and resolution.

## Rules

- Always run `check` before any other command in a new session.
- If the user says 'screenshot <app>', use `find` first to resolve the window ID, then `screenshot --id`.
- If multiple windows match a name, ask the user which one (show the list).
- Screenshots are saved to `<tmp>` — never hardcode absolute paths.
- Only call `inspect_media` when the user explicitly asks to see, describe, or analyze a screenshot.
- Only call `open` in interactive mode when the user asks to open/view a file.
- In autonomous mode, never open files — just save and report the path.
- If a command fails, show the raw error to the user.
- Output is always JSON. Parse it, don't guess.

## Output Format
```json
// check
{"ok": true, "session": "x11", "deps": {"xdotool": true, "import": true, "xrandr": true}}
{"ok": false, "error": "Not Linux", "hint": "os-vision requires Linux with X11."}
{"ok": false, "missing": ["xdotool"], "install": ["sudo apt install xdotool"]}

// list
{"windows": [{"id": 96469375, "title": "GIMP", "x": 700, "y": 388, "w": 1998, "h": 1200, "pid": 12345}]}

// find
{"matches": [{"id": 96469375, "title": "GIMP", "x": 700, "y": 388, "w": 1998, "h": 1200}]}

// screenshot
{"path": "<tmp>/os-vision-20260815-025434.png", "size": "1998x1200", "bytes": 444994}

// info
{"id": 60819428, "title": "~/src/squid-os", "pid": 9010, "cwd": "/home/goglue/src/squid-os", "process": "gnome-terminal-server", "tree": "bash(40735)---squid-os(638723)"}

// monitors
{"monitors": [{"name": "DP-0", "resolution": "3840x1600", "x": 0, "y": 90}, {"name": "eDP", "resolution": "1920x1200", "x": 3840, "y": 789}]}
```

## Examples
**User:** "What windows are open?"
**Action:** `python3 <skill-folder>/scripts/os_vision.py list`
**Result:** Show the list of windows with titles and positions.

**User:** "Screenshot GIMP"
**Action:**
1. `python3 <skill-folder>/scripts/os_vision.py find "GIMP"` → get window ID
2. `python3 <skill-folder>/scripts/os_vision.py screenshot --id 96469375`
**Result:** Report the saved file path.

**User:** "Open it" (interactive mode)
**Action:** `open <tmp>/os-vision-20260815-025434.png`

**User:** "What's running in that terminal tab?"
**Action:** `python3 <skill-folder>/scripts/os_vision.py info --id 60819428`
**Result:** Show the process tree — e.g. "bash → squid-os"

**User:** "Can you see my desktop?"
**Action:**
1. `python3 <skill-folder>/scripts/os_vision.py screenshot --full`
2. `inspect_media` with the path and query "Describe this desktop"
**Result:** Describe what's visible on screen.

**User:** "What monitors do I have?"
**Action:** `python3 <skill-folder>/scripts/os_vision.py monitors`
**Result:** Show monitor names, resolutions, and positions.


## Resources

### Scripts
- [os_vision.py](scripts/os_vision.py) — Executable script

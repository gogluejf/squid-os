---
name: chat-analytics
description: Browses and analyzes Squid-OS chat session JSON files with an interactive dashboard showing token usage, performance metrics, tool calls, file activity, and model comparison.
version: 1.5.0
allowed-tools: bash read_file write_file
---

## Overview
Serves a self-contained analytics dashboard for Squid-OS chat sessions. A Python HTTP server precomputes token tallies, performance timelines, tool usage, file tracking, and model stats from session JSON files. The frontend provides session browsing, search, dashboard overview, and detailed per-session analytics with charts. The agent can also query the API directly to answer analytics questions.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<sessions-dir>` — the Squid-OS sessions directory (e.g. ~/.config/squid-os/sessions)

## Instructions
### Starting the Server
Check first — never restart a healthy instance. The server is stateless (it reads session JSON on demand), so restarting gains nothing and only adds latency.

1. Check status:
```bash
<skill-folder>/scripts/server-status.sh
```
- If it prints `running`, skip to step 4 — do NOT stop or start anything.
- If it prints `stopped` (or the script fails), continue to step 2.

2. Start the server:
```bash
<skill-folder>/scripts/start-server.sh
```
This script handles symlink creation, pid tracking, cleanup, and background launch.

3. Verify the server is running:
```bash
<skill-folder>/scripts/server-status.sh
```
If it still does not print `running`, check `/tmp/chat-analytics.log` for errors before reporting failure.

4. **Interactive mode only** (check the `session-mode` field in the Squid-OS environment section): open the dashboard with @tool:open using the URL `http://localhost:17771`.
Tell the user: `Dashboard is open at http://localhost:17771`.
In autonomous mode skip this step entirely — never call @tool:open or launch any GUI application; just report that the API is available at `http://localhost:17771`.

### Deep Linking to a Session
The dashboard supports hash-based deep links — no extra server endpoint needed:
- `http://localhost:17771/#/<session-name>` opens that session's detail view directly (exact or prefix match).
- Opening a session updates the URL hash, and a **🔗 Copy Link** button in the session header copies the shareable URL.
- When the user asks to "open/show analytics for session X" (or the current session), open `http://localhost:17771/#/<session-name>` instead of the bare dashboard — interactive mode only. The current session name is visible in the sessions directory listing (most recent folder) or from the session context. In autonomous mode, do not open any URL; query the API directly and answer inline.

### Stopping the Server
When the skill is done or being unloaded:
```bash
<skill-folder>/scripts/stop-server.sh
```
Tell the user: `Analytics server stopped.`

### Answering Analytics Questions
Once the server is running, query endpoints with curl:

Global queries:
- `curl -s http://localhost:17771/api/dashboard/summary`
- `curl -s http://localhost:17771/api/dashboard/models`
- `curl -s http://localhost:17771/api/dashboard/activity`

Per-session queries (replace `<session>` with the session folder name):
- `/api/sessions/<session>/tally`
- `/api/sessions/<session>/timeline`
- `/api/sessions/<session>/performance`
- `/api/sessions/<session>/tools`
- `/api/sessions/<session>/skills`
- `/api/sessions/<session>/files`
- `/api/sessions/<session>/general`
- `/api/sessions/<session>`

When the user asks an analytics question:
1. Check server state first with:
```bash
<skill-folder>/scripts/server-status.sh
```
2. If stopped, start it with the start script.
3. Query the appropriate endpoint with curl.
4. Parse the JSON and answer concisely.

## Rules
- Always use the helper scripts: `start-server.sh`, `stop-server.sh`, `server-status.sh`.
- Never inline background shell logic like `nohup`, `&`, `disown`, or `setsid` in the skill instructions.
- Use @tool:open for URLs, and only in interactive mode — in autonomous mode never call it or launch GUI applications.
- Port is fixed at `17771`.
- Check status before starting; never stop a running instance just to restart it. Only stop when explicitly asked or when the server must be unloaded.
- Never modify original session files — all operations are read-only.

## Output Format
```
Server started.
Dashboard URL: http://localhost:17771
Status: running

You can ask me analytics questions and I'll query the API for you.
```

## Examples
Input: User wants to browse chat session analytics.

Agent executes:
1. `<skill-folder>/scripts/server-status.sh` → `stopped`, so start it
2. `<skill-folder>/scripts/start-server.sh`
3. Open `http://localhost:17771` with @tool:open (interactive mode only).

Output:
Server started.
Dashboard URL: http://localhost:17771
Status: running

Input: User asks for analytics again while the server is already running.

Agent executes:
1. `<skill-folder>/scripts/server-status.sh` → `running pid=... url=http://localhost:17771`
2. Query the API directly — no stop, no start.

Input: User asks `Which model do I use most?`

Agent executes:
```bash
curl -s http://localhost:17771/api/dashboard/models | python3 -c "..."
```

Output:
Most used: Lorbus/Qwen3.6-27B-int4-AutoRound with 521 sessions.

## Resources
### Scripts
- [server.py](scripts/server.py) — HTTP server with analytics API endpoints
- [start-server.sh](scripts/start-server.sh) — Launches server in background, creates pidfile, ensures symlink
- [stop-server.sh](scripts/stop-server.sh) — Stops tracked server instance and cleans pidfile
- [server-status.sh](scripts/server-status.sh) — Reports `running` or `stopped`

### Assets
- [index.html](assets/index.html) — Dashboard UI with charts and tables

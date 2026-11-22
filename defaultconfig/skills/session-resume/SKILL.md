---
name: session-resume
description: Resumes work after a session death or gap. Scans the Squid-OS session chain backwards, reconstructs task/plan/intent, pinpoints exactly where work stopped (crash message, last tool call, uncommitted files), and outputs an ADHD-friendly summary with next steps. Use when starting a new session to pick up previous work.
tags: [session, resume, context, recovery]
---
## Overview
Finds the previous session(s) for this working dir, then RECONSTRUCTS THE STORY by reading each session's chat.json yourself. The scanner script only gives cheap facts (folders, crash lines, first/last user messages as POINTERS, commits per session, git state). Understanding what was done, why, and what died is YOUR job — read the actual messages freely.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<working-dir>` — active working directory and project root for the current session
- `<sessions-dir>` — `~/.config/squid-os/sessions`

## Instructions

### 1. Scan (cheap facts)
```
python3 <skill-folder>/scripts/scan_chain.py --working-dir <working-dir>
```
JSON report contains, per session: folder, created/updated, model, message count, crash line, `first_user` + `last_user` (200-char POINTERS, not summaries), `commits_during_session`, plus **skill-invocation signals**: `skills_invoked` (union of user `@skill:name` mentions and `skill_load` tool calls), `resumed_via_skill` (true if session-resume was among them), and `synthetic_skill_events` (synthetic/internal "Skill \"x\" has been loaded/unloaded" messages). Use these as cheap continuity evidence in step 2. Overall git state: branch, last 5 commits, dirty files.

### 2. Reconstruct the CHAIN
**Working dir is a WEAK signal — it only filters candidates, it never justifies a link.** A single dir hosts many unrelated tasks; do not chain sessions just because they share a working_dir.

**The walk (strict):**
1. Take the session immediately before the current one (newest older session in this dir, by **modified date**).
2. Does it contain a session-resume invocation? Check `resumed_via_skill` / `skills_invoked` from the report (covers user `@skill:session-resume`, `skill_load` calls, and synthetic skill events).
   - **YES → it's in the chain.** Move to the session before IT and repeat step 2.
   - **NO → it is the ROOT of this work line. Include it, then STOP.** The first session of any task never called session-resume (the skill only exists for sessions 2+), so a no-flag session is where the trail begins — not a wall. Anything older than the root is a different work line.
3. Cap at 4 sessions. Ignore sessions whose working_dir doesn't match (candidate filter only).

Remember WHY the skill exists: the previous session usually DIED (context busted, crash, compaction). The chain is one interrupted work line; "where it died" matters more than the chain display.

If the chain is just [current] (previous session was unrelated noise), say so explicitly: "no earlier session chains here". Never pad the chain with same-dir noise beyond the root (e.g. system-helper chats like pavucontrol).

### 3. UNDERSTAND THE STORY (the important part)
For each session in the chain, read its chat.json directly:
```
<sessions-dir>/<folder>/chat.json
```
You are free to use any approach — grep, python one-liners, ranged reads. Suggested moves:
- **What got delivered:** grep for `git commit` / `git add` in tool calls → those commit messages ARE the feature list. Cross-check against `commits_during_session`.
- **What was being worked:** read the LAST ~30-50 messages (tail of the file). A crashed session's final assistant text is often empty — the real "where it was" lives in the last tool calls and the user messages around them.
- **What the user wanted:** the last 2-3 USER messages usually state the immediate intent ("fix X", "why does Y fail").
- **Test/failure evidence:** look for test runs, failure tables, trace files mentioned (e.g. `/tmp/*.mjs`, `~/Downloads/*trace*.json`) — they may still exist on disk; check.
- **Plan files:** if a plan path is referenced (`.squid-os/plans/...`), read the relevant section for broader context and open tasks.

Do NOT rely on the 200-char pointers alone. Do NOT invent content — every claim must come from a file you actually read. If a chat.json is unreadable, say so in one line and move on.

### 4. Pinpoint the DEATH POINT (newest session)
- Exact crash line (from report) or 'clean end'.
- What was in-flight: which file was being patched, which fix pattern, what was being traced — from the tail of the chat.json.
- Dirty/uncommitted files (cross-check git status).

### 5. Determine WHAT IS NEXT
Priority: (a) last user message of the dead session, (b) in-flight work from step 4, (c) open items in the referenced plan file. Output a short numbered list.

### 6. Present the summary
Use the Output Format template. ADHD-friendly: short lines, emoji markers, no filler. Include a "Done so far" section built from ACTUAL commits + verified work (not guesses).

### 7. Session-mode behavior
- interactive: END with 'Ready to continue? (y/n)' + the numbered next-step list. Do NOT start work until confirmed.
- autonomous: state the next step and begin executing immediately.

## Rules
- Gists ≤ 10 words. No preamble, no 'certainly', no restating the question.
- Total output under ~40 lines.
- Dirty files matter more than commits: always surface uncommitted work.
- Delivered features = commits made during the session window. List them with caveats if untested.
- Read-only during the scan: do not modify any files (plus the continue decision).

## Output Format
```
## 🧠 Session Resume

**Task:** <one line — the overall task/plan>
**Plan file:** <path if referenced, else '—'>

### Chain (oldest → newest)
1. `<session-folder>` — <gist ≤10 words> ✅/💥
2. `<current session folder>` — this session

(✅ = finished cleanly, 💥 = died/crashed)
If no earlier session passes the continuity test: list only the most recent relevant session + current, and add one line: `⛓️ No chain — previous sessions in this dir are unrelated.`

### 💀 Where it died
- **Error:** `<exact crash line or 'clean end'>`
- **Last user ask:** `<what the user last wanted, one line>`
- **Was doing:** <in-flight work from chat.json tail, one line>
- **Uncommitted:** `<dirty files, or 'none'>`

### ✅ Done so far
- <feature/fix, with commit ref if known>
- ...

### ⏭️ Next
1. <concrete step>
2. <concrete step>

---
Ready to continue? (y/n)
```

## Resources
### Scripts


| File | Description |
|---|---|
| `scan_chain.py` | fact-fetcher only (sessions list, crash lines, first/last pointers, commits per session, git state). Story reconstruction is the agent's job. |
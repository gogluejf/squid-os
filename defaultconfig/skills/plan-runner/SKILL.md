---
name: plan-runner
description: Orchestrates plan execution. Invoke to run, resume, or check progress on a development plan.
version: 2.0.0
allowed-tools: bash read_file write_file edit_file inline_agent skill_load
---

## Overview
Loads a plan, introduces milestones, and drives the per-task orchestrator cycle (execute → verify → review → commit). The runner is the ORCHESTRATOR: it dispatches, verifies mechanically, triggers reviews, commits, and tracks progress. It NEVER writes code itself.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<project-folder>` — the current project directory
- `<plans-dir>` — where plans live for this project (usually `<project-folder>/.squid-os/plans`)
- `<progress>` — `<plans-dir>/<plan-name>/.plan-progress.json`
- `<reviews>` — `<plans-dir>/<plan-name>/reviews.md`

## Instructions

1. **Get Plan Name**: Ask the user for the plan name (folder under `<plans-dir>/<plan-name>/`). If unclear, list available plans with `ls <plans-dir>/`.

2. **Load Plan**: Read `<plans-dir>/<plan-name>/.plan.json` to get milestones, tasks, and plan metadata (commit prefix, source-of-truth docs, wave definitions — if present).

3. **Load or Init Progress**: Check for `<progress>`. If it doesn't exist, create it:
   `python3 <skill-folder>/scripts/progress.py init --plan <plans-dir>/<plan-name>/.plan.json`

4. **Find Next Milestone**: Run:
   `python3 <skill-folder>/scripts/progress.py next-milestone --progress <progress>`
   - If `all_done`: show final summary and stop.
   - Otherwise note the milestone number and name.

5. **Show Milestone Intro**: Run:
   `python3 <skill-folder>/scripts/progress.py milestone-info --progress <progress> --milestone <milestone_number>`
   Display the milestone info as shown in the example below, then ask `Start? (y/n)`. If n, stop.

6. **Task Loop** — repeat until the milestone is complete:
   a. Run `python3 <skill-folder>/scripts/progress.py next-task --progress <progress>` to get `task_id task_name`.
   b. Display: `## ▸ Working on <task_name>`
   c. **Run the full per-task orchestrator cycle from [references/orchestrator.md](references/orchestrator.md)** for this task. That reference is the single source of truth for: mark-in-progress, execute, verify, fix cycles, dual review, resolution, recording, commit, mark-done. Follow its strict order exactly.
   d. Check milestone: `python3 <skill-folder>/scripts/progress.py milestone-complete --progress <progress> --milestone <milestone_number>`
      - If `no`: loop back to step 6a.
      - If `yes`: proceed to step 7.

7. **Milestone Complete**:
   a. Run `python3 <skill-folder>/scripts/progress.py milestone-info --progress <progress> --milestone <milestone_number>`.
   b. Run `python3 <skill-folder>/scripts/progress.py get-milestone-summary --progress <progress> --milestone <milestone_number>` and display the output (skips if empty).
   c. Ask: `Commit? (y/n)`. If y, run `cd <project-folder> && git add -A && git commit`.

8. **Next Milestone**: Loop back to step 4.

## Rules
- **Never Code**: The runner dispatches and decides. All code changes come from executor agents; all review verdicts come from reviewer agents. This holds in EVERY round — including fix rounds and the final no-review fix round. If an executor burns its budget, re-dispatch a FRESH executor with a diagnosis-first prompt (paste the failing output). Never edit files inline to save time or tokens; if re-dispatch also fails → BLOCKED and surface to the user.
- **Orchestrator Reference Wins**: The per-task cycle lives in `references/orchestrator.md`. Do not improvise around it — no skipping verify, no committing before reviews record, no advancing past an uncommitted task.
- **Minimal Display**: Show task name during execution, summary after completion. No full task descriptions.
- **No Commit Tracking**: progress.py only tracks task status. Git is the source of truth for commits.
- **Graceful Resume**: Always check `<progress>` before starting — resume at the first incomplete milestone. A task left `in_progress` after an interruption is re-entered at the orchestrator's verify step (its work may already be on disk).

## Output Format

**Milestone intro:**
```
## Milestone 3: Authorization Gate (6 tasks)

Getting ready to implement 6 tasks for this milestone.

- ◯ **3.1** Add authorization types and authorization context
- ◯ **3.2** Add authorization state to stream and Model.needsAuthorization
- ◯ **3.3** Refactor executeTools to support authorization interruption
- ◯ **3.4** Implement executeSingleTool and resolveAuthorization
- ◯ **3.5** Implement continueAfterAuth and injectAndResume
- ◯ **3.6** Wire authorization into handleStreamEvent tool_calls flow

Start? (y/n)
```

**Task execution:** (Runner outputs the "Working on" line, executor outputs the summary)
```
## ▸ Working on Add authorization types and authorization context
[Executor outputs its summary here]
```

**Milestone complete:**
```
## Milestone 3: Authorization Gate (6 tasks)

Milestone implementation complete.

- ✓ **3.1** Add authorization types and authorization context
- ✓ **3.2** Add authorization state to stream and Model.needsAuthorization
- ✓ **3.3** Refactor executeTools to support authorization interruption
- ✓ **3.4** Implement executeSingleTool and resolveAuthorization
- ✓ **3.5** Implement continueAfterAuth and injectAndResume
- ✓ **3.6** Wire authorization into handleStreamEvent tool_calls flow

- **3.2 Add authorization state to stream and Model.needsAuthorization**
  - **Lesson:** needsAuthorization is a simple switch on settings mode — no extra abstraction needed.

- **3.3 Refactor executeTools to support authorization interruption**
  - **Tradeoff:** Recursive iterative path could blow stack on huge batches — acceptable since tool calls are bounded.

Commit? (y/n)
```

## Resources

### References


| File | Description |
|---|---|
| `orchestrator.md` | the per-task cycle: execute → verify → fix → review → resolve → record → commit → mark-done, plus failure handling and optional wave gates. |
| `reviewer.md` | the shared review prompt template rendered for both reviewer agents. |
### Scripts


| File | Description |
|---|---|
| `progress.py` | Task status tracking and milestone queries. |
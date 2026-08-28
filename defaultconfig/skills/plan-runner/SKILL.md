---
name: plan-runner
description: Orchestrates plan execution. Invoke to run, resume, or check progress on a development plan.
version: 1.0.0
allowed-tools: bash read_file write_file edit_file
---

## Overview
Loads a plan, introduces milestones, delegates tasks to task-executor, and pauses at milestone boundaries for user commit confirmation.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<project-folder>` — the current project directory
- `<progress>` — `<project-folder>/plans/<plan-name>/.plan-progress.json`

## Instructions

1. **Get Plan Name**: Ask the user for the plan name (folder under `<project-folder>/plans/<plan-name>/`). If unclear, list available plans with `ls <project-folder>/plans/`.

2. **Load Plan**: Read `<project-folder>/plans/<plan-name>/.plan.json` to get milestones, tasks, and plan metadata.

3. **Load or Init Progress**: Check for `<progress>`. If it doesn't exist, create it:
   `python3 <skill-folder>/scripts/progress.py init --plan <project-folder>/plans/<plan-name>/.plan.json`

4. **Find Next Milestone**: Run:
   `python3 <skill-folder>/scripts/progress.py next-milestone --progress <progress>`
   - If `all_done`: show final summary and stop.
   - Otherwise note the milestone number and name.

5. **Show Milestone Intro**: Run:
   `python3 <skill-folder>/scripts/progress.py milestone-info --progress <progress> --milestone <milestone_number>`
   Display the milestone info as shown in the example below, then ask `Start? (y/n)`. If n, stop.

6. **Loop — Execute Tasks**:
   a. Run `python3 <skill-folder>/scripts/progress.py next-task --progress <progress>` to get `task_id task_name`.
   b. Mark in progress: `python3 <skill-folder>/scripts/progress.py mark-in-progress --progress <progress> --task-id <task_id>`.
   c. Display: `## ▸ Working on <task_name>`
   d. Delegate to `task-executor` with the plan path and task id. Wait for completion. The executor will display the task summary.
   e. Mark done: `python3 <skill-folder>/scripts/progress.py mark-done --progress <progress> --task-id <task_id>`.
   f. Check milestone: `python3 <skill-folder>/scripts/progress.py milestone-complete --progress <progress> --milestone <milestone_number>`.
      - If `no`: loop back to step 6a.
      - If `yes`: proceed to step 7.

7. **Milestone Complete**:
   a. Run `python3 <skill-folder>/scripts/progress.py milestone-info --progress <progress> --milestone <milestone_number>`.
   b. Run `python3 <skill-folder>/scripts/progress.py get-milestone-summary --progress <progress> --milestone <milestone_number>` and display the output (skips if empty).
   c. Ask: `Commit? (y/n)`. If y, run `cd <project-folder> && git add -A && git commit`.

8. **Next Milestone**: Loop back to step 4.

## Rules
- **Minimal Display**: Show task name during execution, summary after completion. No full task descriptions.
- **No Commit Tracking**: progress.py only tracks task status. Git is the source of truth for commits.
- **Delegation**: Never code yourself. Always delegate to `task-executor`.
- **Graceful Resume**: Always check `<progress>` before starting — resume at the first incomplete milestone.

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

### Scripts
- [progress.py](scripts/progress.py) — Task status tracking and milestone queries.

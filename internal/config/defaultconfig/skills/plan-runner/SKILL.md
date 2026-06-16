---
name: plan-runner
description: Orchestrates plan execution. Invoke to run, resume, or check progress on a development plan.
version: 1.0.0
allowed-tools: bash read_file write_file edit_file
---

## Overview
Loads a plan, shows the full plan with status, executes tasks one at a time with commit after each, and loops until complete.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<project-folder>` — the current project directory
- `<progress>` — `<project-folder>/plans/<plan-name>/.plan-progress.json`

## Instructions

1. **Get Plan Name**: Ask the user for the plan name (folder under `<project-folder>/plans/<plan-name>/`). If unclear, list available plans with `ls <project-folder>/plans/`.

2. **Load Plan**: Read `<project-folder>/plans/<plan-name>/.plan.json` to get milestones, tasks, and plan metadata.

3. **Load or Init Progress**: Check for `<progress>`. If it doesn't exist, create it:
   `python3 <skill-folder>/scripts/progress.py init --plan <project-folder>/plans/<plan-name>/.plan.json`

4. **Show Full Plan**: Run:
   `python3 <skill-folder>/scripts/progress.py full-plan --progress <progress>`
   Display the output so the user sees the entire plan with ✓/◯/◷/✗ per task.

5. **Check Completion**: Run:
   `python3 <skill-folder>/scripts/progress.py all-done --progress <progress>`
   - If `yes`: go to step 9 (final summary).
   - If `no`: proceed to step 6.

6. **Ask User**: Prompt `Start next task? (y/n)`. If `n`, stop.

7. **Execute Task**:
   a. Run `python3 <skill-folder>/scripts/progress.py next-task --progress <progress>` to get `task_id task_name`.
   b. Mark in progress: `python3 <skill-folder>/scripts/progress.py mark-in-progress --progress <progress> --task-id <task_id>`.
   c. Display: `## ▸ Working on <task_name>`
   d. Delegate to `task-executor` with the plan path and task id. Wait for completion.
   e. Mark done: `python3 <skill-folder>/scripts/progress.py mark-done --progress <progress> --task-id <task_id>`.
   f. Show task summary: `python3 <skill-folder>/scripts/progress.py get-summary --progress <progress> --task-id <task_id>` and display the output.

8. **Commit & Loop**:
   a. Ask `Commit? (y/n)`. If `y`, commit the changes.
   b. Go back to step 4.

9. **Final Summary**:
   a. Run `python3 <skill-folder>/scripts/progress.py full-plan --progress <progress>` and display the completed plan.
   b. Run `python3 <skill-folder>/scripts/progress.py final-summary --progress <progress>` and display key lessons and tradeoffs.
   c. Show completion message and stop.

## Rules
- **Always Show Full Plan**: The user always sees the entire plan with status before deciding what to do.
- **No Commit Tracking**: progress.py only tracks task status. Git is the source of truth for commits.
- **Delegation**: Never code yourself. Always delegate to `task-executor`.
- **Graceful Resume**: Always check `<progress>` before starting — resume at the first incomplete task.

## Output Format

**Full plan display:**
```
## Milestone 1: Foundation (2/3)
- ✓ **1.1** Create type definitions
- ✓ **1.2** Implement parser
- ◯ **1.3** Add validation

## Milestone 2: Execution (0/4)
- ◯ **2.1** Build executor
- ◯ **2.2** Add error handling
- ◯ **2.3** Support callbacks
- ◯ **2.4** Add logging

Start next task? (y/n)
```

**Task execution:**
```
## ▸ Working on Add validation
[Executor runs and outputs its summary]

## 1.3: Add validation
- **Done:** Validation logic implemented for all input types
- **Tradeoffs:** Chose runtime validation over compile-time for flexibility
- **Confidence:** High

Commit? (y/n)
```

**Final summary:**
```
## Milestone 1: Foundation (3/3)
- ✓ **1.1** Create type definitions
- ✓ **1.2** Implement parser
- ✓ **1.3** Add validation

## Milestone 2: Execution (4/4)
- ✓ **2.1** Build executor
- ✓ **2.2** Add error handling
- ✓ **2.3** Support callbacks
- ✓ **2.4** Add logging

### Lessons Learned
- **1.2 Implement parser:** Recursive descent was simpler than token-based approach

### Key Tradeoffs
- **2.2 Add error handling:** Chose early return over try-catch for cleaner control flow

All tasks complete!
```

## Resources

### Scripts
- [progress.py](scripts/progress.py) — Task status tracking, full plan display, and summary queries.

---
name: task-executor
description: Executes a single plan task. Invoked by plan-runner to implement a task.
version: 1.0.0
allowed-tools: bash read_file write_file edit_file
---

## Overview
Analyzes code gaps, writes clean DDD/SOLID code, verifies acceptance criteria, runs tests, and returns a summary.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<project-folder>` — the current project directory

## Instructions
1. **Extract Task**: Run `python3 <skill-folder>/scripts/extract-task.py --plan <project-folder>/plans/<plan-name>/.plan.json --task-id {task_id}` to get your scoped context (epic, milestone, task).

2. **Load Conventions**: Read `<skill-folder>/assets/conventions.md` for coding standards.

3. **Analyze Gap** (from extracted task data):
   - Identify what exists vs what's needed based on `task.what`, `task.snippet`, and `milestone.objective`.
   - Check for duplication and design pattern — search the codebase for similar existing code that could be reused.

4. **Code the Task**:
   - Create or edit the files as specified.
   - Use the task's `snippet` as a blueprint, not a template to copy blindly.

5. **Self-Assess**:
   - Does the code fulfill `task.what` and `task.why`?
   - Does it align with `milestone.objective` and `milestone.success`?
   - Are `task.acceptance` criteria met?
   - Is there unnecessary duplication?

6. **Run Verification**: 
   - Execute the commands in `task.verify` (if any). Note any failures.

7. **Store Summary**: Save your findings via: `python3 <skill-folder>/scripts/set-summary.py --progress <project-folder>/plans/<plan-name>/.plan-progress.json --task-id {task_id} --done "{what was done}" --tradeoffs "{tradeoffs}" --confidence "{high|medium|low}" --failures "{failures or None}" --decisions "{architectural decisions made}" --lessons-learned "{gotchas, non-obvious failures, things learned}"`. Call this multiple times if needed — it merges per field. This script only writes summary fields — it cannot change task status.

8. **Display Summary**: Output the completed task summary to the user in this exact format:
```
## {task_id}: {task_name}
- **Done:** {what was done}
- **Tradeoffs:** {tradeoffs}
- **Confidence:** {high|medium|low}
- **Decisions:** {architectural decisions made}
- **Lessons learned:** {gotchas, non-obvious failures, things learned}

`Task {task_id} completed.`
```


## Rules
- **DRY First**: Before writing new code, search for existing similar code. Reuse or Create reusable over duplicate. Avoid having 2 versions of the similar code.
- **DDD Layers**: Adapt to the project's existing layering. Place new code alongside similar existing code. Dependencies point inward.
- **SOLID**: Each type has one responsibility. Dependencies point inward (domain has zero dependencies).
- **File Size**: If a file exceeds ~300-400 lines, ask whether it's doing too much — split by responsibility, not by arbitrary line count.
- **Clear Naming**: Names describe intent, not implementation. Prefer `OrderRepository` over `DBOrderHandler`.
- **Snippet as Blueprint**: Use snippets as structural guidance, not verbatim copy. Adapt to actual codebase conventions.
- **Avoid defensive coding**: Avoid defensive codign that silently hide defect and mask bugs. We capture those bug at testing.
- **No Silent Failures**: If verification fails, report it clearly in the summary. Do not hide failures.
- **No Assumptions**: If the task is ambiguous or the codebase state doesn't match expectations, report the issue in the summary.
- **Summary Only**: Only use `set-summary.py` to write your own task's summary fields. Never attempt to change `status` — the runner owns that.


## Output Format
```
## {task_id}: {task_name}
- **Done:** {what was done}
- **Tradeoffs:** {tradeoffs}
- **Confidence:** {high|medium|low}
- **Decisions:** {architectural decisions made}
- **Lessons learned:** {gotchas, non-obvious failures, things learned}

Task {task_id} completed.
```

## Examples
Input: plan-runner delegates task 3.1
Output:
```
## 3.1: Add authorization types and authorization context
- **Done:** Created internal/app/authorization.go with AuthResult and AuthorizationContext.
- **Tradeoffs:** None
- **Confidence:** high
- **Decisions:** Used simple two-field AuthResult instead of four-variant enum.
- **Lessons learned:** None

Task 3.1 completed.
```

## Resources

### Scripts
- [extract-task.py](scripts/extract-task.py) — Extracts a single task with epic + milestone context from `.plan.json`. Use instead of reading the full plan.
- [set-summary.py](scripts/set-summary.py) — Stores summary fields (done, tradeoffs, confidence, failures, decisions, lessons_learned). Cannot modify status.

### Assets
- [conventions.md](assets/conventions.md) — Coding conventions (DDD, SOLID, naming)

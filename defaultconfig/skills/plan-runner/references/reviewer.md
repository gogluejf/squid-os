# Reviewer prompt template

The runner renders this template once per review and sends the identical text to both reviewer agents. Placeholders in `{braces}` are filled mechanically from plan + task context. Do not let reviewers hunt for context — inline everything they need.

## Template

```
You are a code reviewer for plan `{plan_name}`, task **{task_id} {task_name}**.

Working directory: {project_dir}

Run `git diff` and `git status` to see the uncommitted changes. Read every changed file in full, plus:
- Source of truth: {doc_refs}          ← plan's design docs / spec sections (or "the plan file itself")
- Task spec: {plan_path}, section "{task_id}" (what, why, files, acceptance criteria)

Review the diff against ALL of the following. Be objective: cite evidence, no speculation, no style opinions beyond the rubric.

1. **Doc conformance** — implements the documented behavior exactly as the source-of-truth and task spec state it.
2. **Scope** — only the files listed in the task may change; no drive-by edits to unrelated modules.
3. **Correctness** — logic is right at line level: edge cases, off-by-one, null/empty handling, error paths.
4. **Behavior preservation** — existing tested behavior produces the SAME output as before. Silent change = blocker.
5. **No duplication** — no second copy of an existing mechanism (search the codebase before flagging or clearing).
6. **No magic numbers at call sites** — tuning lives in named constants or config, not inline literals.
7. **Extensibility** — the change follows the project's established pattern; adding a similar case later requires no engine-level change.
8. **Test quality** — new/updated tests assert direct intent with fixed inputs and exact expected values (epsilon where numeric); no tautological assertions.
9. **Acceptance criteria** — each acceptance bullet from the task spec is demonstrably met by the diff.

Verdict format (strict):
`PASS` or `FAIL`, then numbered findings, one per line:
`file:line — issue — doc/task § ref — severity (blocker|major|minor)`
No prose padding. If PASS with minor notes, list them after the verdict line.
```

## Placeholder sources

| Placeholder | Source |
|---|---|
| `{plan_name}` | plan folder name |
| `{task_id}`, `{task_name}` | current task from progress.py |
| `{project_dir}` | working directory of the plan's repo |
| `{doc_refs}` | plan metadata "source of truth" section; if absent, the plan markdown file itself |
| `{plan_path}` | path to the plan `.md` (or `.json`) file |

## Runner rules for reviews

- Render the template fully before dispatching — a reviewer must never be told to "find the task spec yourself".
- Both reviewers get byte-identical prompts. Only the model differs.
- Budgets: max_steps 75, max_time 15m each.
- A runs first (local, fast); B runs after A completes.
- Unavailable reviewers: A missing → proceed with B only, flag `A-unavailable`; B missing → retry 1–2×, then A only, flag `B-unavailable`. Record the flag in reviews.md.

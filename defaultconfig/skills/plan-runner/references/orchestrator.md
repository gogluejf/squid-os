# Orchestrator — per-task cycle

The plan-runner is the orchestrator. It dispatches, verifies mechanically, triggers reviews, commits, and tracks progress. **It NEVER writes code.** All code changes come from executor agents; all review verdicts come from reviewer agents.

## Roles & models

| Role | How it runs | Defaults | Notes |
|---|---|---|---|
| Executor | `inline_agent` with `skills: ["task-worker"]` | session default model | writes code + tests |
| Reviewer A | `inline_agent`, prompt rendered from [reviewer.md](reviewer.md) | `ninfer/qwen3.8-27b` | local; max_steps 75, max_time 15m |
| Reviewer B | `inline_agent`, prompt rendered from [reviewer.md](reviewer.md) | `openai-codex/gpt-5.6-sol` | cloud; max_steps 75, max_time 15m; retry 1–2× if overloaded |
| Tests | deterministic shell | — | never an LLM |

Both reviewers receive the **identical** rendered prompt — only the model differs. Independent verdicts on identical input are what make disagreement meaningful. Model names may be overridden by plan metadata.

## Per-task loop (strict order)

1. `progress.py mark-in-progress --task-id <id>`
2. **Execute** — inline agent with the `task-worker` skill, passed the plan path + task id. Budgets: max_steps 150, max_tools 200, max_time 15m. The executor reads its own scoped context from the plan file; do not re-explain the task to it.
3. **Verify** — run every `Verification:` command from the task + related existing tests. Mechanical pass/fail only.
4. **Fix cycle** — if red: fresh executor with a diagnosis-first prompt (paste the failing output, ask for root cause then fix). Max 2 retries. Still red → **BLOCKED**: stop, surface findings to the user, wait.
5. **Review A** — fresh inline agent, Reviewer A model, prompt = [reviewer.md](reviewer.md) rendered with current task context, on the uncommitted diff.
6. **Review B** — same rendered prompt, Reviewer B model. Run after A completes (A first: local, fast).
7. **Resolve**:
   - Both PASS → step 8.
   - Any FAIL → ACCEPT/REJECT each finding: ACCEPT → executor fixes (fresh dispatch with the accepted findings), then re-verify (step 3) and re-review only the changed diff; REJECT → note why in one line.
   - Disagreement between A and B on the same finding → mechanical tests break the tie; if tests don't decide, ask the user a one-line question.
   - Max 2 resolve rounds. After round 2, do ONE last fix round for accepted findings, then re-verify mechanically only — NO further reviews. Note remaining/deferred items in reviews.md and move on. Still broken after the final fix → BLOCKED, surface to user.
8. **Record** — append both verdicts + resolution to `<plans-dir>/<plan-name>/reviews.md`:
   ```markdown
   ## <task-id> <task name>
   - A (<model>): PASS|FAIL — <findings or "none">
   - B (<model>): PASS|FAIL — <findings or "none">
   - Resolution: <accepted/rejected findings, tie-breaks, rounds used>
   ```
9. **Commit** — `git add -A && git commit -m "<prefix> <task-id>: <task name>"`. Only after steps 3–8 complete. `<prefix>` comes from plan metadata; default: no prefix.
10. `progress.py mark-done --task-id <id>`

**A task is NOT done until committed. No next task before the commit lands.** Commit authorization for these per-task commits comes from this workflow itself; milestone-boundary commits still require explicit user confirmation.

## Resume after interruption

If a task is left `in_progress` when the runner restarts: inspect `git status` / `git diff`. If uncommitted work exists for that task, re-enter at step 3 (verify). If the tree is clean and the task has no commit, re-enter at step 2 (execute).

## Failure handling

- Task blocked after 2 fix cycles → stop, report to user. Wait.
- Executor budget exhausted → diagnosis-first re-dispatch (max 1), then BLOCKED. The orchestrator NEVER edits code inline to work around a burned budget — that burns more tokens and breaks the role separation.
- Reviewer A unavailable → proceed with B only, flag `A-unavailable` in reviews.md.
- Reviewer B unavailable → retry 1–2×, then proceed with A only, flag `B-unavailable`.
- User interrupts are limited to reviewer disagreement — one line, batched at the next gate if non-blocking.

## Wave gate (only if the plan defines waves)

Some plans declare waves with gate commands (full test suite, regression suites, baselines). If — and only if — the plan defines them:

1. At each wave boundary, run the plan's gate commands. Any NEW failure vs the plan's baseline → fix via executor, re-run. Max 2 cycles, then stop and surface.
2. **Wave review** — both reviewers (same shared prompt) on the accumulated wave diff since the last gate.
3. Gate commit: `<prefix>: wave <n> gate`. Clean tree required at every gate.

Plans without waves skip this section entirely — the per-task loop above is the whole workflow.

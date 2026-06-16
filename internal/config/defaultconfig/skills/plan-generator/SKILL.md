---
name: plan-generator
description: Generates structured coding plans (Epic/Milestone/Task) with DDD alignment. Invoke when the user asks for a development plan, feature roadmap, or multi-step engineering breakdown before coding begins. 
allowed-tools: bash read_file write_file open
---

## Overview
Analyzes the codebase and user request to create a development plan. Breaks work into Epics, Milestones (Features/Bounded Contexts), and Tasks. Tasks are vertical slices — each cuts through all layers (Interface → Application → Domain) for one cohesive behavior. Tasks include What, Why, Files (add/edit/rm), Snippet, and Verification. Milestones include architecture patterns and diagrams. Uses scripts to enforce Markdown structure and generate a styled HTML report. Opens the HTML in Chromium upon completion. Project-specific scope.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<project-folder>` — the current project directory (e.g. `~/src/squid-os`)

## Instructions
1.  **Interview & Challenge**: If the user's request is ambiguous, missing key details (like scope, existing architecture, or specific constraints), or poorly articulated, interview the user to gather all necessary information. Challenge assumptions if they conflict with the existing codebase. Only proceed when you have a clear, cohesive understanding.
2.  **Analyze**: Review the user's request and the current codebase (using `read_file` on key files if needed) to understand the scope and existing architecture.
3.  **Build Plan**: Use the deterministic builder script to construct the plan step-by-step.
    *   Initialize: `python3 <skill-folder>/scripts/build_plan.py --dir <project-folder>/plans/$PLAN_NAME init --plan-name "$PLAN_NAME" --title "<Epic Title>" --why "<Reason>" --outcomes "<Outcomes>"`
    *   For each milestone: `python3 <skill-folder>/scripts/build_plan.py --dir <project-folder>/plans/$PLAN_NAME add-milestone --name "<Name>" --pattern "<Pattern>" --objective "<Objective>" --success "<Success>" --diagram "<Description>"`
    *   For each task: `python3 <skill-folder>/scripts/build_plan.py --dir <project-folder>/plans/$PLAN_NAME add-task --type "feature" --name "<Name>" --what "<Action>" --why "<Reason>" --files "+ path1" --files "~ path2" --snippet "code1" --snippet "code2" --acceptance "criteria1" --verify "cmd1"` (repeat flags for multiple items)
    *   Export Markdown: `python3 <skill-folder>/scripts/build_plan.py --dir <project-folder>/plans/$PLAN_NAME export`
4.  **Render HTML**: Run `python3 <skill-folder>/scripts/render_plan.py "<project-folder>/plans/$PLAN_NAME/$PLAN_NAME.md" "<project-folder>/plans/$PLAN_NAME/$PLAN_NAME.html" "$PLAN_NAME"` to generate the rich HTML report.
5.  **Open**: Run `chromium "<project-folder>/plans/$PLAN_NAME/$PLAN_NAME.html"` to display the plan.
.  **Report**: Confirm completion to the user.

## Plan Structure
Use these definitions to guide the content of each field:

### Epic
- **Name**: The high-level feature or project title.
- **Why**: The problem this epic solves.
- **Outcomes**: The business or technical value delivered.

### Milestone
- **Name**: A **Feature** or **Bounded Context** (e.g., "User Auth", "Product Catalog"). *Do not use architectural layers (Domain/App) as Milestone names unless the entire plan is for a library of one type.*
- **Pattern**: The primary architectural pattern guiding this feature (e.g., "CQRS", "Port & Adapter"). *Multiple patterns allowed.*
- **Objective**: The specific goal of this feature.
- **Success**: The criteria for this feature to be considered complete.
- **Diagram**: The flow or structure of this feature. Use **Mermaid syntax** for visual diagrams. Start the value with a Mermaid directive (`graph TD`, `flowchart LR`, `sequenceDiagram`, `stateDiagram-v2`, `classDiagram`, etc.) for a rendered diagram. The renderer wraps it in `<pre class="mermaid">` and loads Mermaid from CDN. Falls back to plain text description if Mermaid syntax is not detected.
```
graph TD
    A[User Input] --> B{Validation}
    B -->|pass| C[Process]
    B -->|fail| D[Return Error]
    C --> E[Save to DB]
```

### Task
- **Name**: The atomic action (commit-atomic).
- **Type**: What kind of change this is — one of: `feature` (new observable behavior), `refactor` (restructure without behavior change), `bug` (fix incorrect behavior), `test` (add/improve tests), `doc` (documentation), `chore` (build/scripts/deps).
- **What**: **1–2 sentences max.** State the concrete action and target location (e.g., "Add `X` field to `Y` struct in `internal/...`").
- **Why**: **1–2 sentences max.** State the reason and impact (e.g., "Enables the system to do Z").
- **Snippet**: **Valid syntax with comments.** Use standard comments (`//` or `#`) for logic descriptions, placeholders, or omitted blocks. It acts as a blueprint to communicate concepts to human and coding agent.  Repeatable.
- **Files**: The paths (which naturally show the layer: `internal/domain/`, `internal/app/`, `cmd/`). Repeatable.
- **Acceptance**: Verification criteria. Repeatable.
- **Verify**: Test commands. Repeatable.

## Rules
- **Autonomy Focus**: The plan is a guide for execution, not a pre-built solution. Provide enough context to start, but leave the implementation to the agent.
- **Task Atomicity**: Tasks must be commit-atomic and verifiable.
- **Meaningful Granularity**: Tasks should represent meaningful engineering units, not individual methods, renderers, handlers, getters, validators, or small helpers. Only split work when the pieces provide independent value, risk, ownership, or verification.
- **Conciseness**: `--what` and `--why` must be **1–2 sentences max**. Do not dump implementation details into them.
- **Snippet Blueprint**: `--snippet` should communicate design, not implementation. **Do not include granular logic** Include type definitions, data struct, api contracts, function signatures, interface contracts, or workflow skeletons. Never generate detailed algorithms, business logic, control flow, pseudocode, or executable code.` **Do not include raw text descriptions or details** outside of code or comments.
- **Mermai syntaxt**: Generate Mermaid 9.4.3 compatible diagrams and strip () and | characters from flowchart node labels.

```
type OrderProcessor struct {
    OrderStore     OrderStore
    Inventory      InventoryService
    Payments       PaymentService
    Fulfillment    FulfillmentService
    EventPublisher EventPublisher
}

func (p *OrderProcessor) ProcessOrder(orderID string) error {
    // Validate order state
    // Reserve inventory
    // Process payment
    	//Unlock invetory on failure
    // Trigger fulfillment
    // Publish completion event
}
```
- **Acceptance Criteria**: List **all** verifiable conditions the task must satisfy. Use multiple `--acceptance` flags — one per distinct behavior, edge case, or integration point. Don't collapse multiple checks into one vague line.
- **Feature Milestones**: Milestones must represent Features or Bounded Contexts, not just technical layers.
- **Prefer Cohesion**: Keep closely related implementation details together. Rendering logic, keyboard handling, validation, and supporting helpers that belong to the same feature should normally be part of a single task.
- **Vertical Slices**: Each task is a vertical slice — it cuts through all layers (Interface → Application → Domain → Infrastructure) for one complete behavior. If a type, its state, the function that uses it, and the handler that calls it are part of one flow, they belong in a single task. Only split at natural boundaries: user input, I/O, independent sub-features, or when scope exceeds ~5 files or ~150 lines.
- **Milestone Sizing**: Prefer a maximum of 3–8 tasks per milestone. If a milestone exceeds this range, reconsider whether tasks are being split too finely.
- **No Assumptions**: Do not guess ambiguous requirements; ask the user.
- **No Hardcoded Paths**: Always use contextual tags (`<skill-folder>`, `<project-folder>`) in instructions — never embed absolute paths.
- **Mermaid Diagrams**: Always use Mermaid syntax for diagrams (`graph TD`, `flowchart LR`, `sequenceDiagram`, etc.). The renderer detects Mermaid directives and renders them as visual diagrams. Plain text descriptions fall back to legacy arrow-split rendering.



## Output Format
Each plan generates a folder in `plans/<plan-name>/` containing:
- `.plan.json` — The source-of-truth JSON state (Epic, Milestones, Tasks).
- `<plan-name>.md` — The structured Markdown export.
- `<plan-name>.html` — The rich HTML visualization.

## Resources
### Scripts
- [build_plan.py](scripts/build_plan.py) — Deterministic builder for Epic, Milestone, and Task.
- [render_plan.py](scripts/render_plan.py) — Generates rich HTML visualization.

## Examples
**Input:** "Plan a feature to add user authentication."
**Output:** Creates `plans/user-auth/user-auth.md`, `plans/user-auth/user-auth.html`, and `plans/user-auth/.plan.json` and opens in Chromium.

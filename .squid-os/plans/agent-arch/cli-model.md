# CLI Model

## Purpose
Define the intended CLI grammar and behavioral model for Squid-OS.
This document is a preparation artifact for planning. It is not the implementation plan.

## Core direction
The CLI is not a new invention.
It already exists.
It must be refactored into a clean mode-driven interface.
It should not remain a thin legacy wrapper around the TUI.

## Canonical command shape
Long-term canonical command family:

```text
squid-os
squid-os tui
squid-os run
squid-os server
squid-os gnu
```

## Default command behavior
Bare command:

```text
squid-os
```

Meaning:
- start the TUI

Important clarified behavior:
- if the user passes `--prompt` with bare `squid-os`, it still means TUI
- `--prompt` should prefill the textarea in TUI mode
- `--prompt` should no longer implicitly switch to headless execution

So current legacy behavior must be replaced.

## Subcommands

### `tui`
Purpose:
- explicit TUI boot

Behavior:
- same effective behavior as bare `squid-os`
- `--prompt` pre-populates the textarea

### `run`
Purpose:
- explicit non-TUI execution path
- supports inline runtime config or named agents

Examples:

```text
squid-os run \
  --model openai/gpt-5.5 \
  --system "You are a senior Go engineer." \
  --tools read_file,bash \
  --prompt "Find the startup bottleneck."
```

```text
squid-os run code-review \
  --prompt "Review the startup sequence."
```

```text
squid-os run code-review \
  --model anthropic/claude-opus \
  --thinking false \
  --prompt "Quick review only."
```

### `server`
Purpose:
- reserved CLI surface for future API server mode

Phase-1 behavior:
- present in `--help`
- accepted as a command surface
- returns a clear not-implemented response

### `gnu`
Purpose:
- reserved CLI surface for shell GNU tool generation
- migration target for `bin/mgnu`

Phase-1 behavior:
- present in `--help`
- accepted as a command surface
- returns a clear not-implemented response

## `run` grammar
`run` must support two major shapes.

### 1. Inline run
No named agent:

```text
squid-os run --model ... --prompt ...
```

Meaning:
- construct runtime directly from settings + CLI overrides

### 2. Named-agent run
Named agent first positional argument:

```text
squid-os run code-review --prompt ...
```

Meaning:
- load named agent
- resolve runtime from settings + agent + CLI overrides

Equivalent explicit form:

```text
squid-os run --agent code-review --prompt ...
```

Preferred/canonical UX:
- positional agent is the nicer shorthand for `run`

Conflict rule:
- if positional agent and `--agent` are both provided and differ, fail clearly

## Run precedence
Resolved precedence is:
1. CLI flags
2. agent definition
3. global settings

No exceptions are intended at this point.

## CLI flag support matrix

| Flag | Command support | Short description | Values |
|------|------------------|-------------------|--------|
| `--prompt` | `squid-os`, `tui`, `run` | Initial user input. In TUI it prefills textarea; in run it is the execution prompt. | string |
| `--session` | `squid-os`, `tui`, `run` | Continue/open an existing saved session. In run it adds a new turn non-TUI. | session name |
| `--model` | `squid-os`, `tui`, `run` | Override effective model selection. Supports combined provider/model form. | `provider/model` string |
| `--thinking` | `squid-os`, `tui`, `run` | Override thinking mode for the session or run. | `true`, `false` |
| `--working-dir` | `squid-os`, `tui`, `run` | Set the working directory context. | path string |
| `--agent` | `squid-os`, `tui`, `run` | Select the root/owning installed agent for a new session/run. In `run`, positional agent may be preferred. | agent name |
| `--agents` | `squid-os`, `tui`, `run` | Restrict/override available callable subagents. | comma-separated agent names |
| `--system` | `squid-os`, `tui`, `run` | Override the Agent System text for the session/run. Not the Base System prompt file. | string |
| `--tools` | `squid-os`, `tui`, `run` | Restrict/override available tool set. | comma-separated tool names |
| `--skills` | `squid-os`, `tui`, `run` | Restrict/override available skill set, not the active loaded skill. | comma-separated skill names |
| `--skill` | `squid-os`, `tui`, `run` | Force/set the active loaded skill for the next turn or new session. | skill name |
| `--save` | `run` | Control runtime auto-persist behavior for the run session. | `true`, `false` |
| `--save-name` | `run` | Explicit persisted session name for run output if save is enabled. | string |
| `--auth-mode` | `run` | Set non-interactive runtime auth behavior via mapped policy. | mapped auth mode string |
| `--memory-namespace` | `run` | Override resolved memory namespace. | `workspace`, `global`, `agent` |
| `--memory-instructions` | `run` | Override memory instruction text. | string |
| `--max-steps` | `run` | Maximum loop/agent steps for the run. | integer |
| `--max-tools` | `run` | Maximum tool executions for the run. | integer |
| `--max-time` | `run` | Maximum run duration. | duration string, e.g. `30m`, `2h` |
| `--max-tool-result-tokens` | `squid-os`, `tui`, `run` | Maximum stored/returned tool result size. | integer |
| `--max-agent-depth` | `run` | Maximum nested agent call depth. Default 5. At 0, call_agent returns error. | integer |
| `--mode` | `run` | Execution/output mode for the run. | `final_message`, `stream`, `silent`, `structured` |

## Notes on the matrix
- bare `squid-os` and `squid-os tui` should support only flags that make sense before the UI starts
- `run` supports both session bootstrap flags and execution/output flags
- `server` and `gnu` should appear in help, but phase-1 flag support for them is not a priority because they return not implemented
- if both positional agent and `--agent` are supported in `run`, the exact conflict rule should be defined explicitly during plan construction

## `--prompt`
`--prompt` is cross-mode input.

Behavior by mode:
- bare `squid-os` or `squid-os tui`
  - populate textarea default content
- `squid-os run`
  - provide the user message for execution

This is a key difference from the current implementation.

## `--session`
`--session` is cross-mode session selection.

Behavior by mode:
- bare `squid-os --session <name>`
  - open TUI on that session
- `squid-os tui --session <name>`
  - same as above
- `squid-os run --session <name> --prompt "..."`
  - continue the saved session by adding a new turn non-TUI

Important intent:
- `--session` means continue/open an existing session
- it does not mean rebuild the session from scratch

## `--session` flag compatibility
When continuing an existing session, the most natural allowed flags are the ones that behave like next-turn runtime switches rather than fresh-session bootstrap replacement.

Likely safe with `--session`:
- `--prompt`
- `--model`
- `--thinking`
- `--skill`
- `--skills`
- `--tools`
- `--agents`
- `--auth-mode`
- `--save`

Likely constrained or disallowed with `--session`:
- `--agent`
- positional agent in `run`
- `--system` as a fresh bootstrap layer replacement
- other flags that imply recreating session origin/bootstrap instead of continuing it

Exact final compatibility rules should be made explicit in the planning phase, but the continuation principle is now clear.

## stdin behavior for `run`
`run` should support stdin as prompt input.

Examples:

```text
cat code.go | squid-os run
```

Meaning:
- stdin becomes the prompt body if `--prompt` is absent

```text
cat code.go | squid-os run --prompt "analyze this:"
```

Meaning:
- prompt prefix and stdin should compose
- final effective prompt should be built as the prompt text followed by stdin content

Recommended composition model:

```text
<prompt text>

<stdin content>
```

So:
- stdin should not silently override `--prompt`
- `--prompt` and stdin should combine predictably

## Run session save output
When `run` saves a session, the saved session name should be reported as operational metadata.

Rules:
- report only the saved session name
- do not print internal session IDs
- use stderr for non-stream modes so stdout stays clean

Examples:
- `final_message`: final answer on stdout, saved session name on stderr
- `silent`: no normal stdout, saved session name on stderr if saved
- `stream`: include the saved session name in the JSONL stream metadata/event

If the run is not saved:
- do not print a session identity message by default

## `--system`
`--system` should remain the user-facing flag name.
Its meaning in the new model is:
- agent system override text

It should **not** mean base system prompt file selection.

This keeps the user mental model simple:
- `--system` defines the role/system instruction for the runnable agent/session behavior
- base system remains internal/app-level and comes from app settings/base prompt configuration

## System layers
There are three distinct system layers:

1. **Base System**
   - internal app/base prompt from system prompt file config
   - bios-like
   - always present

2. **Environment System**
   - generated by the app from environment data
   - always present

3. **Agent System**
   - from agent file
   - or replaced by CLI `--system`

This separation is important and should be reflected cleanly in runtime/session bootstrap.

## Future base-system override naming
If the CLI ever needs to override the base system prompt file explicitly, it should use a different flag name, such as:
- `--sys-prompt`

This avoids overloading `--system` with two meanings.

## `--version`
The CLI must expose a `--version` flag.

Purpose:
- print the current Squid-OS version string and exit ( this feautre already existi and must be preserved)

This should consume the same canonical version source used in the TUI header and provider User-Agent strings.

## Named agent source of truth
Named agents come from the global agents registry.
For this phase:
- agents are global
- they live in config
- they mirror the same broad registry/discovery philosophy as skills

## Help surface
Even when commands are not implemented yet, they should still appear in help if they are part of the intended product grammar.

That applies to:
- `server`
- `gnu`

## Legacy behavior to drop
The new CLI should explicitly drop the old legacy assumptions and hacks, including:
- `--prompt` meaning implicit headless mode
- image-first legacy behavior as part of the main CLI path
- ad hoc root-command branching instead of clear mode/subcommand grammar

## Output modes
`run` must support agent/runtime output modes such as:
- `final_message`
- `stream`
- `silent`
- `structured`

Phase notes:
- `final_message`: supported target
- `silent`: should be implementable
- `stream`: likely implementable at CLI boundary
- `structured`: should return clear not-implemented behavior for now

## Completion
Autocomplete support matters early.
Specifically:
- bash completion
- zsh completion

This should be part of the CLI design from the beginning rather than a late afterthought.

Completion should support dynamic values where practical.

Dynamic completion targets:
- `--session`
  - saved session names from sessions folder
- `--agent`
  - installed agent names from agents registry/folder
- `--agents`
  - installed agent names from agents registry/folder
- `--skill`
  - installed skill names from skills registry/folder
- `--skills`
  - installed skill names from skills registry/folder

Static completion targets:
- `--thinking`
  - `true`, `false`
- `--save`
  - `true`, `false`
- `--mode`
  - `final_message`, `stream`, `silent`, `structured`
- `--memory-namespace`
  - `workspace`, `global`, `agent`
- `--auth-mode`
  - supported mapped auth mode values

Limit flags:
- `--max-steps`, `--max-tools`, `--max-tool-result-tokens`
  - numeric values, no special completion required
- `--max-time`
  - duration string, no special completion required

Tool completion:
- `--tools` should complete available tool names
- avoid manually hardcoding tool names in the shell script if practical
- prefer deriving from the app/tool registry through the binary or generated completion data

Model completion:
- `--model` completion is desirable but not required immediately
- possible future approach: cache scanned model names to a file when the app scans/builds the model list, then completion can read that cache

Context-aware flag completion:
- if easy, completion can become smarter based on flags already present, such as `--session`
- if harder, do not block phase 1 on this
- value completion for sessions/agents/skills/tools is the important first target

## Why this doc matters
The CLI is no longer just a frontend concern.
It defines:
- product grammar
- runtime entrypoints
- how agents are invoked
- how prompts are routed
- how future sub-agent CLI self-calls will behave

So the CLI model must be settled before generating the implementation plan.

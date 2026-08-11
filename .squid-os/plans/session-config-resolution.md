# Session Configuration Resolution

## Legend

| Source | Meaning |
|---|---|
| **D** | computed default (e.g. `os.Getwd()`) |
| **S** | global settings |
| **A** | agent definition |
| **C** | explicit CLI flag |
| **E** | saved session config |
| **—** | source does not provide this property |

## Session loading

When starting a session, an existing session is loaded in this priority order:

1. **Explicit `--session`** — user specified a session name
2. **Autoload last** — if settings have `auto_load_last_session` and `last_session_name` (TUI only)

If none match, a new session is created.

When an agent has `save.enabled: true` and no `--save-name` is given, the agent's own name is used as the default autosave session name.

## Bootstrap flags rejected with existing session

When continuing a session (via `--session` or autoload), these flags are rejected because they affect context already embedded in the transcript:

- `--system`
- `--working-dir`
- `--memory-namespace`
- `--memory-instructions`

## Precedence table

`→` means "overridden by". Leftmost wins; rightmost overrides.

| Property | New session | Continue session |
|---|---|---|
| `Target` | `C` | `C` |
| `Inference.Provider` | `S → A → C` | TUI: `E → S → C` · Run: `E → C` |
| `Inference.Model` | `S → A → C` | TUI: `E → S → C` · Run: `E → C` |
| `Inference.Thinking.Enabled` | `S → A → C` | TUI: `E → S → C` · Run: `E → C` |
| `Inference.Thinking.ParseReasoningFromText` | `S` | TUI: `E → S` · Run: `E` |
| `SystemPromptFile` | `S` | `E` (immutable) |
| `AgentName` | `A → C` | `E` |
| `AgentSystem` | `A → C` | `E` (flag rejected) |
| `ActiveSkill` | `C` | `E → C` |
| `AuthMode` | `S → A → C` | TUI: `E → S → C` · Run: `E → C` |
| `WorkingDir` | `D → A → C` | `E` (flag rejected) |
| `Tools` | `D → A → C` | `E → C` |
| `Skills` | `D → A → C` | `E → C` |
| `Agents` | `D → A → C` | `E → C` |
| `Memory.Namespace` | `D → A → C` | `E` (flag rejected) |
| `Memory.Path` | derived from namespace | `E` (flag rejected, path re-derived from E's namespace) |
| `Memory.Instructions` | `A → C` | `E` (flag rejected) |
| `Autosave.Enabled` | `S → A → C` | TUI: `E → S → C` · Run: `E → C` |
| `Autosave.Name` | `C → timestamp` | `E → C` |
| `Limits.MaxSteps` | `A → C` | `E → C` |
| `Limits.MaxTools` | `A → C` | `E → C` |
| `Limits.MaxToolResultTokens` | `S → A → C` | TUI: `E → S → C` · Run: `E → C` |
| `Limits.MaxAgentDepth` | `D → A → C` | `E → C` |
| `Limits.MaxTime` | `A → C` | `E → C` |
| `DebugEnabled` | `S` | TUI: `E → S` · Run: `E` |
| `ContextCompaction` | `S` | `S` |

## Application mode

| Mode | Meaning |
|---|---|
| **Immediate** | Applied directly to `Config` |
| **Pending** | Kept as `Config` until next turn; desired value staged in `Pending` |
| **Rebuild** | Would require rebuilding transcript bootstrap messages (not implemented) |

| Property | Application mode |
|---|---|
| `Target` | Pending |
| `Inference` | Pending |
| `ActiveSkill` | Pending |
| `Tools` | Pending |
| `Skills` | Pending |
| `Agents` | Pending |
| `SystemPromptFile` | Immediate (new only) / Rebuild (existing) |
| `AgentName` | Immediate (provenance only) |
| `AgentSystem` | Immediate (new only) / Rebuild (existing) |
| `AuthMode` | Immediate |
| `WorkingDir` | Immediate (new only) / Rebuild (existing) |
| `Memory` | Immediate (new only) / Rebuild (existing) |
| `Autosave` | Immediate |
| `Limits` | Immediate |
| `DebugEnabled` | Immediate |
| `ContextCompaction` | Immediate (settings-controlled for new and existing sessions) |

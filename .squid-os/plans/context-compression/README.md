# File Context Compaction Direction

## Goal

Reduce the exact next LLM request without deleting or rewriting persisted session history. The same dynamically built request must supply both the provider messages and the footer token count.

This phase covers:

- Full and ranged `read_file`
- `edit_file`
- `write_file` with `write` or `create` traces
- Current next-request totals in Chat Analytics

Bash, `sed` parsing, and structured code search are separate future work.

## Control and persistence

One global setting controls runtime use:

```json
{
  "context_compaction": true
}
```

Resolution:

- New session: copy the setting into `initial` and `config`.
- Existing session: preserve the persisted session value.
- No CLI or agent override.

Persist complete tool arguments, results, token metrics, and `FileEntry` history. Do not persist compaction decisions, assistant `context_metrics`, or per-turn compaction records.

## Ranged `read_file`

`read_file` accepts optional inclusive line bounds:

```json
{
  "path": "internal/chat/session.go",
  "start_line": 100,
  "end_line": 180
}
```

Rules:

- Neither range argument present: full read.
- Either range argument present: partial read.
- Return only the requested lines for a partial read.
- Read the complete file internally and compute the normal full-file checksum.
- Keep canonical absolute `FileEntry.path` and full-file `FileEntry.checksum`.
- Merge successful reads through the existing `file_state` mechanism.
- Multiple partial reads of the same tracked file version are allowed and do not invalidate one another.
- If a partial read discovers an externally changed tracked file, reject it without replacing the tracked checksum and require a full read.
- A full read refreshes the tracked checksum through existing behavior.
- Do not add a range registry, coverage model, or new `FileStateEntry` shape.

The persisted instruction arguments are sufficient to distinguish full and partial reads. No range fields are required on `FileEntry`.

## Checkpoints and observations

A full checkpoint establishes complete file content:

- Successful `read_file` without range arguments
- Successful `write_file` with trace `write`
- Successful `write_file` with trace `create`

Other successful events are retained observations or deltas:

- Ranged `read_file`
- Successful `edit_file`
- No-op `edit_file` that emits trace `read`

Checkpoint rules:

- A ranged read never supersedes an earlier read, range, or edit.
- The latest full checkpoint supersedes all earlier successful events for the same canonical path.
- Keep the latest full checkpoint and every event after it.
- Keep failed or incomplete operations.
- Interleaved files are planned independently.

Examples:

```text
range A 1-100       keep
range A 200-300     keep
edit A              keep
```

```text
range A 1-100       compact
range A 200-300     compact
edit A              compact
full read A         keep
```

```text
full read A         keep
edit A              keep
range A 200-260     keep
```

```text
create A            compact
edit A              compact
write A             keep
```

## Dynamic request construction

For every next request:

```text
complete session history
    -> BuildCompactionPlan
    -> transform provider messages
    -> count the transformed messages
    -> send those same messages
```

Compaction is non-destructive. Preserve tool-call IDs, names, chronology, valid argument objects, and matching tool results.

Use fixed internal replacements:

- Superseded read: keep path/range arguments; replace result.
- Superseded edit: replace `old_string`, `new_string`, and result.
- Superseded write/create: replace `content` and result.

The replacement format is an implementation detail, not configuration.

When compaction is disabled, build the existing provider messages and report zero savings.

## Token accounting

Keep separate values for instruction arguments and execution results:

```text
RawInstruction
RawExecution
Raw
RetainedInstruction
RetainedExecution
Retained
SavedInstruction
SavedExecution
Saved
```

The compacted total must be counted from the exact transformed provider messages, including replacement overhead. Never estimate the footer by subtracting persisted token metrics through a separate path.

Footer semantics:

```text
lifetime input/output · ctx compacted/context-window · saved
```

- Lifetime input/output remain historical counters.
- The context fraction and bar use compacted next-request tokens.
- Cache the derived request context and invalidate it whenever request-relevant session content changes.

## Chat Analytics

Chat Analytics always computes and displays the current next-request calculation from complete persisted history:

```text
Raw
Compacted
Saved
```

This is the same concept whether the session switch is on or off. There is no actual/forecast mode and no historical per-turn metric.

Go and Python must share expected fixtures for:

- Full read followed by full read
- Multiple partial reads
- Partial reads followed by edit
- Partial reads followed by full read
- Write/create checkpoints
- No-op and failed edits
- Interleaved paths
- Combined instruction/result token totals

## Out of scope

- Bash result compaction
- Parsing historical `sed` commands
- Structured `search_code`
- Glob/find tooling
- General conversation summarization
- Persisted compaction decisions or metrics
- Automatic filesystem reads during request construction

Structured search and Bash observations can be designed later without changing the core contract: tool-specific rules produce decisions, and one request builder applies and measures them.

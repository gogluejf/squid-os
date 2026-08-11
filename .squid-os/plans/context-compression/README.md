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

When compaction is disabled, send the existing (uncompacted) provider messages as the outgoing snapshot, but still calculate the full compaction projection — raw, compacted, saved, saved_instruction, saved_execution — for the token tally and footer. The setting controls only the outgoing snapshot; the projection is always computed.

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

This is the same concept whether the session switch is on or off. There is no actual/forecast mode and no historical per-turn metric. The persisted `token_tally.context` provides session-wide totals for migrated sessions; per-file rows are always derived from tool history.

### Shared fixtures

Go (`internal/chat/compaction_test.go`) and Python (`chat-analytics/scripts/test_file_compaction.py`) share a single fixture at `internal/chat/testdata/file_compaction_cases.json`. Both implementations must produce identical compacted_events, retained_events, and token totals for every scenario.

The fixture covers:

- Repeated full reads (repeated_full)
- Multiple partial reads with no checkpoint (multiple_ranges)
- Ranged read, edit, ranged read (range_edit_range)
- Ranged reads followed by full read (ranges_then_full)
- Write/create followed by read (write_read_checkpoints)
- Full read, no-op edit, edit (noop_edit)
- Failed read before successful read (failures_retained)
- Interleaved paths with independent planning (interleaved_paths)
- Ranged read, edit, ranged read, full read (range_edit_range_full)
- Create, edit, write, edit (write_create_edit_edit)
- Failed write not a checkpoint (failed_write_not_checkpoint)
- Single full read (single_full_read)
- Single ranged read (single_ranged_read)
- Full read then ranged read then edit (full_then_range_then_edit)

## Out of scope

- Bash result compaction
- Parsing historical `sed` commands
- Structured `search_code`
- Glob/find tooling
- General conversation summarization
- Persisted compaction decisions or metrics
- Automatic filesystem reads during request construction

Structured search and Bash observations can be designed later without changing the core contract: tool-specific rules produce decisions, and one request builder applies and measures them.

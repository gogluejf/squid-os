# File Tool Context Compaction

## Purpose

Reduce the historical file-tool content sent to the LLM while preserving the complete session document for audit, analytics, UI rendering, and future policy changes.

This document covers only:

- `read_file` with trace `read`
- `edit_file` with trace `edit`
- `write_file` with trace `write`
- `write_file` with trace `create`

Bash output and other tools are out of scope.

## Core principle

Persist every original tool call, argument, result, token metric, and `FileEntry`. Apply compaction only when constructing messages for an LLM request.

```text
Tool execution
    -> persist complete ToolCallEntry and FileEntry
    -> dynamically build a file context plan
    -> build compacted API messages
    -> send request to provider
```

Do not rewrite old session messages or mark old entries as permanently compacted. A dynamic plan keeps historical data intact and allows the policy to evolve.

## Terminology

### Full-state checkpoint

An operation that establishes the complete known content of a file:

- Successful `read_file`
- Successful `write_file` overwriting an existing file (`write`)
- Successful `write_file` creating a new file (`create`)

### Delta

A successful `edit_file` operation (`edit`) applied after a checkpoint.

### Superseded event

A successful file event that occurred before a later successful full-state checkpoint for the same path. Its detailed arguments and result no longer contribute to understanding the latest file state.

### Retained event

An event required to represent the latest known state:

- The latest full-state checkpoint
- Successful edits after that checkpoint
- An unresolved failed or no-op operation

## Source data

The planner should use persisted tool execution data rather than infer operations from tool names alone:

```json
{
  "instruction": {
    "name": "read_file",
    "arguments": "{\"path\":\"internal/chat/session.go\"}"
  },
  "execution": {
    "status": "success",
    "result": "<file content>",
    "tokens": 2738,
    "files": [
      {
        "path": "/absolute/path/internal/chat/session.go",
        "trace": "read",
        "checksum": "...",
        "time": "..."
      }
    ]
  }
}
```

Use the normalized `FileEntry.path` as the file identity. Do not group events only by the path string originally supplied in tool arguments because relative and absolute paths may identify the same file.

## Planning algorithm

For every API request:

1. Scan persisted messages and tool calls chronologically.
2. Collect file events from `tool_calls[].execution.files[]`.
3. Group events by normalized file path.
4. For each path, locate the latest successful `read`, `write`, or `create` event.
5. Mark successful events before that checkpoint as superseded.
6. Retain the checkpoint.
7. Retain all events after the checkpoint, including edits.
8. Retain unresolved failures and no-op attempts until a later successful checkpoint or mutation makes them irrelevant.
9. Build API messages using the decisions without modifying persisted messages.

Conceptual decision type:

```go
type FileContextDecision struct {
    ToolCallID   string
    Path         string
    Trace        string
    Action       string // keep or compact
    SupersededBy string
    Reason       string
}
```

## Compaction rules by operation

### Read

A successful `read_file` result contains a complete file snapshot.

- Keep the latest checkpoint read.
- Compact an older read when a later successful checkpoint exists.
- Keep the small `path` argument even when its result is compacted.

Compacted result example:

```text
[Compacted file result: snapshot superseded by tool call tool-42]
```

### Edit

A successful `edit_file` is a delta.

- Keep an edit if it occurs after the latest checkpoint.
- Compact an edit if a later successful checkpoint confirms the complete resulting state.
- When compacting, compact both potentially large `old_string` and `new_string` arguments and the execution result.
- Preserve required argument fields so historical tool input remains schema-shaped.

Compacted arguments example:

```json
{
  "path": "internal/chat/session.go",
  "old_string": "[compacted]",
  "new_string": "[compacted]",
  "replace_all": false
}
```

Compacted result example:

```text
[Compacted successful edit: superseded by file snapshot tool-42]
```

An `edit_file` call that finds no match or makes no change currently emits trace `read`. Treat it as an observation/no-op rather than a mutation. Keep its explanatory result while it remains relevant; a later successful checkpoint can supersede it.

### Write

A successful `write_file` against an existing file emits trace `write` and establishes a complete new state.

- Keep it if it is the latest checkpoint and later edits depend on it.
- Compact it when a later successful checkpoint exists.
- The full file content is in `instruction.arguments.content`, so argument compaction is important.

Compacted arguments example:

```json
{
  "path": "internal/chat/session.go",
  "content": "[compacted]"
}
```

Compacted result example:

```text
[Compacted successful overwrite: superseded by file snapshot tool-42]
```

### Create

A successful `write_file` for a new path emits trace `create` and is also a full-state checkpoint.

Apply the same rules as `write`:

- Keep the content if it is the latest checkpoint.
- Keep later edits.
- Compact the create arguments and result if a later read, write, or create checkpoint exists.

## Sequence examples

### Repeated reads

```text
read A     compact
read A     compact
read A     keep: latest checkpoint
```

The final read contains the latest complete snapshot.

### Read followed by edits

```text
read A     keep: latest checkpoint
edit A     keep: delta after checkpoint
edit A     keep: delta after checkpoint
```

The baseline and both deltas are needed unless the runtime materializes a newer full snapshot.

### Edits followed by a read

```text
read A     compact
edit A     compact
edit A     compact
read A     keep: latest checkpoint
```

The final read confirms the complete state, so earlier operations no longer need detailed content in the API context.

### Write followed by edits

```text
write A    keep: latest checkpoint
edit A     keep
edit A     keep
```

The write content is the baseline for the edits.

### Write followed by a read

```text
write A    compact
read A     keep: latest checkpoint
```

The later read confirms what is actually on disk.

### Create followed by edits and a read

```text
create A   compact
edit A     compact
edit A     compact
read A     keep: latest checkpoint
```

### Interleaved files

Compaction is independent per normalized path:

```text
read A     compact for A
read B     keep for B
edit A     compact for A
read A     keep for A
edit B     keep for B
```

The later read of A does not affect B.

### Failed edit

```text
read A               keep
edit A -> error      keep while unresolved
```

A failed edit does not establish a new state. Do not use it as a checkpoint.

After a successful correction and confirming read:

```text
read A               compact
edit A -> error      compact or summarize as resolved
edit A -> success    compact
read A               keep
```

## Tool-call protocol integrity

Do not remove only one side of a tool exchange. Preserve:

- Assistant tool-call part
- Tool-call ID
- Tool name
- Valid JSON argument object
- Matching tool-result part
- Original chronology

Replace large historical fields with compact markers. This avoids orphaned tool results and maintains provider message structure.

For strict providers, compacted arguments should retain the required schema fields instead of introducing an unsupported `_compacted` property.

## Build boundary

The session should expose complete history while API construction accepts a context plan:

```go
plan := BuildFileContextPlan(session.Doc.Messages, policy)
apiMessages := BuildAPIMessages(session.Doc.Messages, plan)
```

A possible policy configuration:

```json
{
  "context": {
    "file_compaction": {
      "enabled": true,
      "compact_results": true,
      "compact_arguments": true
    }
  }
}
```

The first implementation may compact only superseded execution results. Argument compaction can follow after provider compatibility tests, but it is necessary to reclaim large historical `write_file.content`, `edit_file.old_string`, and `edit_file.new_string` values.

## Analytics and metrics

Chat Analytics should continue reading the original persisted tool history. It should not reimplement or guess the compaction policy.

If historical compression analytics are required, persist request-level metrics after building each request:

```json
{
  "context_metrics": {
    "file_execution_tokens_original": 108557,
    "file_execution_tokens_effective": 48740,
    "file_execution_tokens_saved": 59817,
    "file_events_kept": 82,
    "file_events_compacted": 178
  }
}
```

These metrics describe what was actually sent for one request. They are preferable to mutable flags on historical tool entries.

The complete session remains the audit source for:

- Reads, edits, creates, and writes
- Original instruction and execution token counts
- File diffs and checksums
- Tool-call chronology

## Validation requirements

Tests should cover at least:

1. `read -> read`
2. `read -> edit`
3. `read -> edit -> read`
4. `write -> edit`
5. `write -> read`
6. `create -> edit -> read`
7. Failed edit before and after recovery
8. No-op edit emitting trace `read`
9. Interleaved operations on multiple paths
10. Relative tool argument resolving to an absolute `FileEntry.path`
11. Multiple file entries in one tool call
12. Tool-call/result ID pairing after compaction
13. Valid JSON and required fields for compacted arguments
14. Persisted session immutability
15. Deterministic plans for identical history and policy

## Initial rollout

A conservative rollout should be:

1. Build the plan dynamically and leave session persistence unchanged.
2. Compact only superseded successful `read_file` execution results.
3. Validate provider compatibility and measure savings.
4. Compact superseded `edit_file` arguments and results.
5. Compact superseded `write_file`/create content and results.
6. Add request-level metrics for Analytics.
7. Consider materialized synthetic checkpoints only if retained edit chains remain too large.

## Out of scope

- Bash result compaction
- General conversation summarization
- Deleting persisted history
- Filesystem mutation during planning
- Treating checksum-only `file_state` as file content
- Automatically reading files during API message construction

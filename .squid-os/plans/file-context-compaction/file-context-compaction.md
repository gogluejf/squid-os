# Dynamic File Context Compaction

## Core Problem

Squid-OS sends superseded file-tool arguments and results back to the LLM, and its footer reports lifetime tokens rather than the exact next-request context. Full-file reads are also inefficient for focused inspection, encouraging unstructured sed usage.

## Goal

Add settings-controlled ranged reads and deterministic file compaction, build and measure the exact next LLM request, show its compacted size in the footer, and keep Chat Analytics aligned with the same rules.

---

## 1. Session Compaction Setting

- **Pattern:** Settings snapshot into session configuration

**Objective:** Resolve one global context_compaction switch into new sessions and preserve it in saved sessions without CLI or agent overrides.

**Success Criteria:** Settings and session JSON expose the switch, runtime resolution follows the documented precedence, and tests cover new and continued sessions.

```mermaid
flowchart LR
    S[settings context_compaction] --> R[runtime Resolve]
    R --> N[new SessionConfig]
    E[saved SessionConfig] --> C[continued session]
    C --> X[no CLI or agent override]
```

### 1.1. Resolve compaction from settings into session state

**Type:** feature

**What:** Add the context compaction boolean to Settings and SessionConfig, resolve it from settings for new sessions, and preserve the saved value for existing sessions.

**Why:** Makes compaction a single settings-controlled behavior while keeping each session deterministic after creation.

**Files:**

- ~ defaultconfig/settings.json
- ~ internal/config/settings.go
- ~ internal/config/session.go
- ~ internal/runtime/runtime.go
- ~ internal/runtime/runtime_test.go
- ~ .squid-os/plans/session-config-resolution.md

**Snippet:**

```
type Settings struct {
    ContextCompaction bool `json:"context_compaction"`
}

type SessionConfig struct {
    ContextCompaction bool `json:"context_compaction"`
}
```

```
// New session: Settings -> SessionConfig
// Existing session: persisted SessionConfig
// No CLI or agent override.
```

**Acceptance Criteria:**

- [ ] The default settings file defines context_compaction.
- [ ] New sessions copy the settings value into initial and config.
- [ ] Existing interactive and autonomous sessions preserve their persisted value.
- [ ] No CLI option, runtime Overrides field, or agent field controls compaction.
- [ ] session-config-resolution.md documents settings-only resolution and immediate application.

**Verify:**

```bash
go test ./internal/config ./internal/runtime
```

---

## 2. Ranged File Reads

- **Pattern:** Structured partial observation with full-file freshness

**Objective:** Let read_file return focused line ranges while retaining canonical file paths and full-file checksums for stale-file protection.

**Success Criteria:** Full and ranged reads share the existing FileEntry/file_state flow, range reads return bounded content, and later compaction can distinguish partial reads from instruction arguments.

```mermaid
flowchart LR
    A[read_file arguments] --> Q{range present}
    Q -->|no| F[return full content]
    Q -->|yes| P[return selected lines]
    F --> C[full-file checksum and FileEntry]
    P --> C
    C --> S[file_state freshness]
    P --> O[partial observation not checkpoint]
    F --> K[full checkpoint]
```

### 2.1. Support checksum-safe ranged read_file calls

**Type:** feature

**What:** Add optional start_line and end_line arguments to read_file, returning only the selected lines while computing and storing the normal full-file checksum.

**Why:** Provides the focused inspection efficiency currently obtained through sed without introducing a range registry or weakening stale-file detection.

**Files:**

- ~ internal/tools/core.go
- ~ internal/tools/core_test.go
- ~ internal/chat/tool_exec.go
- ~ internal/chat/tool_exec_test.go

**Snippet:**

```
// read_file input contract
{
  "path": "string",
  "start_line": "optional positive integer",
  "end_line": "optional positive integer"
}

// Persisted distinction:
// range present in ToolCallEntry.Instruction.Arguments => partial read
// no range fields => full read
// FileEntry.Path remains canonical and FileEntry.Checksum remains full-file.
```

**Acceptance Criteria:**

- [ ] Omitting both range arguments returns the complete file with unchanged behavior.
- [ ] A valid inclusive range returns only the requested lines with stable line-bound semantics.
- [ ] Invalid, reversed, zero, and out-of-bounds ranges return clear errors without updating file state.
- [ ] A successful ranged read computes FileEntry.Checksum from the complete file bytes and merges it through the existing file_state path.
- [ ] Multiple ranged reads of the same tracked file version succeed and do not invalidate one another.
- [ ] If a ranged read detects that a previously tracked file changed externally, it is rejected without replacing the tracked checksum and asks for a full read.
- [ ] A full read after an external change refreshes the tracked checksum through existing read behavior.
- [ ] No range registry, coverage model, or FileStateEntry schema expansion is introduced.

**Verify:**

```bash
go test ./internal/tools ./internal/chat -run 'ReadFile|FileState|FileChange'
```

---

## 3. File Compaction Rules

- **Pattern:** Pure deterministic planner

**Objective:** Derive one immutable compaction plan from persisted file-tool history using full-checkpoint rules and partial-read observations.

**Success Criteria:** The planner distinguishes full reads from ranged reads using persisted arguments, compacts only events superseded by a later full checkpoint, and reports combined instruction/result savings without mutating history.

```mermaid
flowchart LR
    H[complete tool history] --> E[extract file events]
    E --> G[group by FileEntry path]
    G --> Q{full checkpoint}
    Q -->|full read write create| K[latest checkpoint]
    Q -->|ranged read or edit| O[observation or delta]
    K --> C[compact earlier successful events]
    K --> R[retain checkpoint and later events]
    O --> R
    C --> P[CompactionPlan]
    R --> P
```

### 3.1. Plan deterministic full-checkpoint compaction

**Type:** feature

**What:** Add a pure planner that classifies persisted file events, identifies full checkpoints, and produces per-tool-call compaction decisions with token summaries.

**Why:** Centralizes safe file rules for request construction, footer accounting, Analytics parity, and later tool-specific extensions.

**Files:**

- + internal/chat/compaction.go
- + internal/chat/compaction_test.go
- + internal/chat/compaction_benchmark_test.go

**Snippet:**

```
type CompactionDecision struct {
    ToolCallID string
    Path string
    Trace string
    CompactArguments bool
    CompactResult bool
    SupersededBy string
}

type CompactionTokens struct {
    RawInstruction int
    RawExecution int
    Raw int
    RetainedInstruction int
    RetainedExecution int
    Retained int
    SavedInstruction int
    SavedExecution int
    Saved int
}

type CompactionPlan struct {
    Decisions map[string]CompactionDecision
    Tokens CompactionTokens
}

func BuildCompactionPlan(messages []config.Message) CompactionPlan
```

```
// Checkpoint classification contract:
// read_file without start_line/end_line => full checkpoint
// read_file with either range argument => partial observation
// write_file trace write/create => full checkpoint
// edit_file => delta, including no-op read traces
```

**Acceptance Criteria:**

- [ ] The planner uses canonical FileEntry.path to group events and persisted read_file arguments to distinguish full from partial reads.
- [ ] Successful full reads and write/create events are full checkpoints.
- [ ] Ranged reads never supersede earlier reads, ranges, or edits.
- [ ] A later full read, write, or create compacts all earlier successful events for that path, including ranged reads.
- [ ] The latest checkpoint and all later partial reads and edits remain retained.
- [ ] No-op edit_file calls that emit read are not checkpoints.
- [ ] Failed and incomplete operations remain retained.
- [ ] Raw, retained, and saved instruction/execution totals match persisted metrics.
- [ ] Planning is deterministic and does not mutate messages.
- [ ] The planner scans persisted history once, groups events by canonical path with maps, and performs direct decision lookup by tool-call ID.
- [ ] The implementation avoids repeated whole-history scans per event and scales linearly with messages, tool calls, and file events.
- [ ] Benchmarks report planner ns/op, B/op, and allocs/op for a large synthetic session.

**Verify:**

```bash
go test ./internal/chat -run Compaction
```

```bash
go test -bench 'BuildCompactionPlan' -benchmem ./internal/chat
```

---

## 4. Compacted Request Context

- **Pattern:** Single request snapshot

**Objective:** Build, transform, and measure one exact provider-message snapshot for the next LLM request.

**Success Criteria:** Enabled sessions send compacted file history with valid tool pairing; disabled sessions remain unchanged; reported compacted tokens describe the exact request.

```mermaid
sequenceDiagram
    participant Session
    participant Builder as BuildContext
    participant Planner
    participant Engine
    Session->>Builder: messages and enabled flag
    Builder->>Planner: BuildCompactionPlan
    Planner-->>Builder: decisions
    Builder->>Builder: transform and count exact messages
    Builder-->>Session: Context
    Session->>Engine: same Context messages
```

### 4.1. Build and send the exact compacted request

**Type:** feature

**What:** Add BuildContext to apply compaction decisions, count the resulting next-request provider messages, and make the stream path send that exact snapshot.

**Why:** Prevents request construction and token reporting from drifting while preserving complete persisted history.

**Files:**

- + internal/chat/context.go
- + internal/chat/context_test.go
- ~ internal/chat/engine.go
- ~ internal/chat/session.go
- ~ internal/chat/loop.go
- + internal/chat/context_benchmark_test.go

**Snippet:**

```
type ContextTokens struct {
    Raw int
    Compacted int
    Saved int
}

type Context struct {
    Messages []provider.Message
    Compaction CompactionPlan
    Tokens ContextTokens
}

func BuildContext(messages []config.Message, enabled bool) Context
func (s *Session) BuildContext() Context
```

**Acceptance Criteria:**

- [ ] Disabled compaction produces the existing provider messages and zero savings.
- [ ] Enabled compaction preserves tool-call IDs, names, chronology, valid JSON arguments, and matching results.
- [ ] Superseded read results are replaced while path and optional range arguments remain valid.
- [ ] Superseded edit old_string/new_string and result content are replaced with fixed internal text.
- [ ] Superseded write/create content and result content are replaced with fixed internal text.
- [ ] Persisted messages are unchanged after BuildContext.
- [ ] Compacted token count includes replacement overhead and describes Context.Messages exactly.
- [ ] StartStreamWithContext sends Context.Messages without an independent rebuild.
- [ ] Context transformation uses direct plan lookup, reuses unchanged strings where practical, and does not deep-copy the complete session history.
- [ ] Benchmarks report request-building ns/op, B/op, and allocs/op for a large synthetic session.

**Verify:**

```bash
go test ./internal/chat -run Context
```

```bash
go test -bench 'BuildContext' -benchmem ./internal/chat
```

---

## 5. Live Context Visibility

- **Pattern:** Revision-cached derived state

**Objective:** Expose compacted next-request size in the footer without rescanning history on every render.

**Success Criteria:** The footer context bar uses compacted next-request tokens, shows savings, preserves lifetime input/output counters, and refreshes after request-content mutations.

```mermaid
flowchart LR
    M[message or tool mutation] --> I[invalidate Context cache]
    F[footer render] --> Q{cache current}
    Q -->|yes| U[use token totals]
    Q -->|no| B[BuildContext once]
    B --> U
    U --> V[compacted over context window]
    U --> L[lifetime input output separate]
```

### 5.1. Show cached compacted context in the footer

**Type:** feature

**What:** Cache the derived Context on Session mutations and update footer contracts to show compacted next-request usage and savings separately from lifetime input/output totals.

**Why:** Makes context-window pressure accurate without expensive recomputation on every TUI frame.

**Files:**

- ~ internal/chat/session.go
- ~ internal/chat/tool_exec.go
- ~ internal/app/render.go
- ~ internal/ui/footer.go
- ~ internal/ui/footer_test.go

**Snippet:**

```
type FooterData struct {
    LifetimeInputTokens int
    LifetimeOutputTokens int
    RawContextTokens int
    CompactedContextTokens int
    SavedContextTokens int
    ContextWindow int
}

func (s *Session) BuildContext() Context
func (s *Session) InvalidateContext()
```

**Acceptance Criteria:**

- [ ] Session caches one Context and invalidates it whenever persisted request content changes.
- [ ] Repeated footer renders without mutation reuse the cache.
- [ ] The context fraction and bar use CompactedContextTokens divided by ContextWindow.
- [ ] Savings are shown when compaction is enabled and nonzero.
- [ ] Lifetime input/output remain separate and never drive context-window usage.
- [ ] During streaming the footer reflects the sent snapshot; after persistence it refreshes for the next request.
- [ ] Footer tests cover enabled, disabled, zero-window, over-window, narrow, and wide layouts.

**Verify:**

```bash
go test ./internal/chat ./internal/app ./internal/ui
```

---

## 6. Compaction Observability

- **Pattern:** Cross-implementation contract fixtures

**Objective:** Keep Chat Analytics and Squid-OS aligned on the current next-request file compaction calculation.

**Success Criteria:** Analytics displays the same raw, compacted, and saved values as Go for full and ranged read sequences, with no persisted per-turn metrics or actual/forecast modes.

```mermaid
flowchart LR
    H[complete session history] --> G[Go rules]
    H --> P[Python Analytics rules]
    X[shared fixtures] --> G
    X --> P
    G --> A[raw compacted saved]
    P --> A
    S[future structured search] -. separate plan .-> G
```

### 6.1. Align Analytics with full and ranged read rules

**Type:** test

**What:** Document and test one current next-request calculation across Go and Chat Analytics, including ranged-read classification and combined instruction/result totals.

**Why:** Prevents dashboard drift without persisting compaction decisions, request metrics, or historical turn records.

**Files:**

- + internal/chat/testdata/file_compaction_cases.json
- ~ internal/chat/compaction_test.go
- ~ .squid-os/plans/context-compression/README.md
- ~ /home/goglue/.config/squid-os/skills/chat-analytics/scripts/server.py
- ~ /home/goglue/.config/squid-os/skills/chat-analytics/scripts/test_file_compaction.py
- ~ /home/goglue/.config/squid-os/skills/chat-analytics/assets/index.html

**Snippet:**

```
{
  "name": "partial partial full",
  "events": ["read range", "read range", "read full"],
  "expected": {
    "compacted_events": 2,
    "retained_events": 1
  }
}
```

**Acceptance Criteria:**

- [ ] The one-page README states settings-only control, dynamic non-destructive compaction, and fixed internal replacement text.
- [ ] The README defines ranged reads as observations and full reads/write/create as checkpoints.
- [ ] Go and Python fixtures cover repeated full reads, multiple ranges, range-edit-range, ranges followed by full read, write/read checkpoints, no-op edits, failures, and interleaved paths.
- [ ] Analytics always presents the current next-request calculation regardless of whether the session switch is enabled.
- [ ] Analytics combines instruction and execution tokens and includes replacement overhead once runtime transformation lands.
- [ ] No context metrics are persisted on assistant messages and no historical per-turn records are added.
- [ ] Bash, sed parsing, and structured search remain explicitly out of scope for this plan.

**Verify:**

```bash
go test ./internal/chat && python3 -m unittest discover -s /home/goglue/.config/squid-os/skills/chat-analytics/scripts -p 'test_*.py' -v
```

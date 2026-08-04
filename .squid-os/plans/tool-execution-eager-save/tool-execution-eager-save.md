# EPIC: Eager Tool Execution: Save Assistant Message Before Tool Loop
Why: When tool execution pauses for authorization, the assistant message disappears from view because it hasn't been saved yet. This makes it impossible for the user to see what the assistant is about to do. Additionally, if the app crashes mid-tool-loop, all tool progress is lost.
Outcomes: 1. Assistant message saved immediately after stream ends, before any tool executes. 2. Tool entries visible with preview diffs during auth pause. 3. Crash resilience - executed tools are persisted with incremental metrics. 4. Simplified render path - no special streaming-vs-paused conditions.

## MILESTONE: 1 - Preview at Auth Gate & Smart Rendering
Pattern: Lazy Evaluation, View Model
Objective: Generate preview data only when a tool needs authorization. Render only tools up to the pending index so the last visible tool is the one being authorized. Show error messages even when collapsed.
Success: Preview runs on-demand at the auth gate. Pending tools render with diffs up to the pending index. Error tools show their error message visible.
Diagram: graph LR
  A[Tool needs auth] --> B["Call tool.Preview"]
  B --> C{Preview ok}
  C --> D[Fill Execution with preview, show auth question]
  C --> E[Mark status error, skip auth, continue]
  A --> F[Execute directly]

### TASK: 1.1 - Add on-demand preview at auth gate in resumeToolExecution
Type: feature
What: In the auth gate of the tool loop, call tool.Preview(args) for destructive tools. On success, pre-fill Execution with preview data (Files with diffs, Result) and status=pending, then show the auth question. On error, mark status=error with the error message, skip authorization entirely, and continue to the next tool.
Why: Preview only when needed keeps the eager-save step simple (just Instruction + empty Execution). If a preview fails, there's no point asking the user to authorize a tool that will error.
Files: ~ internal/app/stream.go
Snippet: // In the tool loop, when needsAuthorization is true:\nif m.needsAuthorization(tool, args) {\n    // Run preview first\n    if tool.Preview != nil {\n        preview := tool.Preview(args)\n        if preview.Status == tools.ResultStatusError {\n            // Preview failed — mark as error, skip auth\n            entries[i].Execution.Status = tools.ResultStatusError\n            entries[i].Execution.Error = preview.Error\n            continue\n        }\n        // Preview succeeded — populate execution with preview data\n        entries[i].Execution.Status = tools.ResultStatusPending\n        entries[i].Execution.Result = preview.Result\n        entries[i].Execution.Files = preview.Files\n        for j := range preview.Files {\n            preview.Files[j].ToolCallID = p.id\n        }\n    } else {\n        entries[i].Execution.Status = tools.ResultStatusPending\n    }\n    // Now set up auth context and show question\n    m.stream.authorizationCtx = &AuthorizationContext{...}\n    m.stream.pendingToolIndex = i\n    m.setAuthMode()\n    return m, nil\n}
Acceptance: For destructive tools with Preview (write_file, edit_file), preview runs before the auth question and populates Execution.Files with diffs and Execution.Result
Acceptance: If preview returns an error, the tool is marked as error status and authorization is skipped
Acceptance: For destructive tools without Preview field, Execution is set to pending without preview data
Acceptance: The auth question still shows after a successful preview, with the diff visible in the viewport
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.2 - Render tools up to pending index with visible error messages
Type: feature
What: Update renderToolCallsInline in message.go to: (1) only render tool entries up to and including the pending tool index (the one being authorized), skipping entries after it. (2) Show error message text even when collapsed, not just the [✗] prefix.
Why: During auth pause, we want the last visible tool to be the one being authorized. Tools that haven't been reached yet shouldn't clutter the view. Error messages are the whole point of an error entry — they should be visible.
Files: ~ internal/ui/message.go
Snippet: // renderToolCallsInline: stop rendering at the pending boundary\nfunc renderToolCallsInline(toolCalls []config.ToolCallEntry, boxWidth int, expanded bool, pendingIndex *int, reg *tools.Registry) string {\n    var b strings.Builder\n    for i, tc := range toolCalls {\n        // Don't render tools beyond the pending index\n        if pendingIndex != nil && i > *pendingIndex {\n            break\n        }\n        // ... existing render logic\n\n        // Error messages always visible (not just when expanded)\n        if tc.Execution.Status == "error" {\n            if tc.Execution.Error != "" {\n                content = append(content, renderPerLine(tc.Execution.Error, t.Style.Error))\n            }\n        }\n\n        // Diff visible for both success and pending (with preview data)\n        if (tc.Execution.Status == "success" || tc.Execution.Status == "pending") && len(tc.Execution.Files) > 0 {\n            if d := renderToolFilesDiff(tc.Execution.Files, boxWidth, t.Style); d != "" {\n                content = append(content, d)\n            }\n        }\n    }\n    return b.String()\n}
Acceptance: During auth pause, only tools up to and including the pending index are rendered
Acceptance: Tools after the pending index are hidden from view
Acceptance: Error tool entries show their error message text even when collapsed
Acceptance: Pending tool entries with preview FileEntry diffs render the side-by-side diff
Acceptance: Success status tools continue to render diffs as before
Acceptance: When no auth is active (pendingIndex is nil), all tools render as before
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 2 - Eager Save & In-Place Mutation
Pattern: Command Pattern, State Machine
Objective: Save the assistant message before the tool loop starts, then mutate the saved message's ToolCalls in place as each tool executes. Update metrics incrementally for crash resilience.
Success: After stream ends with tool_calls, the assistant message is immediately visible. Each tool execution updates the saved message. Metrics are updated incrementally — if the app crashes, the persisted state reflects exactly which tools have executed.
Diagram: sequenceDiagram
  participant S as Stream
  participant R as resumeToolExecution
  participant M as Saved Message
  participant V as Viewport
  S->>R: stream done, tool_calls
  R->>M: appendAssistantMsg with pending tools
  R->>V: updateViewportContent
  loop for each tool i
    R->>R: file change gate, auth gate, execute
    R->>M: mutate ToolCalls i Execution
    R->>M: update incremental metrics
    R->>V: updateViewportContent
  end
  R->>M: finalize remaining metrics
  R->>R: stream.reset, startStream

### TASK: 2.1 - Save assistant message before tool loop with initial metrics
Type: refactor
What: Have appendAssistantMsg return the index of the appended message. Call it before the tool loop with all tool entries (Instruction populated, Execution status=pending empty). Save initial metrics we know: text metrics, thinking metrics, tool call instruction tokens/duration, and a partial SequenceStat.
Why: The message must be saved before any tool runs so it persists across auth pauses and survives crashes. Saving known metrics upfront means a crash doesn't lose progress -- the saved state reflects what we know.
Files: ~ internal/app/stream.go
Snippet: // In handleStreamEvent, when event.Done && stopReason==tool_calls:\nfunc (m *Model) resumeToolExecution() (tea.Model, tea.Cmd) {\n    // Build all tool entries with instruction only\n    entries := make([]config.ToolCallEntry, len(m.stream.partialTools))\n    for i, p := range m.stream.partialTools {\n        entries[i] = buildInstructionEntry(p)\n        entries[i].Execution.Status = tools.ResultStatusPending\n    }\n\n    // Save eagerly with initial metrics\n    msgIdx := m.appendAssistantMsg(config.Message{\n        // ... text, thinking, textMetrics, thinkingMetrics from stream\n        ToolCalls:       entries,\n        ToolCallMetrics: {Tokens: m.stream.metrics.ToolCallTokens(), ...},\n        // DurationTimeMs = 0, TokensPerSecond = 0 — finalized later\n    })\n\n    // Loop mutates m.session.file.Messages[msgIdx].ToolCalls in place\n    for i := 0; i < len(entries); i++ {\n        // file change gate -> auth gate (with preview) -> execute\n    }\n}
Acceptance: appendAssistantMsg returns the index of the appended message
Acceptance: Message is saved before any tool executes, with all tool entries as pending
Acceptance: TextMetrics and ThinkingMetrics are saved from stream state (already known)
Acceptance: ToolCallMetrics instruction tokens are saved (already known from partialTools)
Acceptance: DurationTimeMs and TokensPerSecond start at 0 and are updated incrementally
Acceptance: SequenceStat is created with initial values and updated as tools execute
Acceptance: The viewport shows the full assistant message immediately after stream ends
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.2 - Mutate saved message in place with incremental metrics
Type: refactor
What: Rewrite the tool execution loop to mutate session.file.Messages[msgIdx].ToolCalls[i].Execution directly. After each execution, update incremental metrics on the saved message: SequenceStat.ExecDurMs, ToolCallMetrics, and a running DurationTimeMs. Invalidate render cache and update viewport after each tool.
Why: Mutating the saved message means the viewport always reflects current state. Incremental metrics mean a crash preserves a consistent snapshot of what's been executed.
Files: ~ internal/app/stream.go
Snippet: // Loop mutates saved message directly\nfor i := startIndex; i < len(m.stream.partialTools); i++ {\n    tc := &m.session.file.Messages[msgIdx].ToolCalls[i]\n    p := m.stream.partialTools[i]\n    tool := m.toolReg.Get(p.name)\n    args := parseArgs(p.args)\n\n    // Gate: file change (before auth)\n    // Gate: authorization (with on-demand preview)\n\n    // Execute\n    resultStart := time.Now()\n    result := tool.Execute(args)\n    tc.Execution.Status = result.Status\n    tc.Execution.Result = result.Result\n    tc.Execution.Error = result.Error\n    tc.Execution.Tokens = countTokensApprox(content)\n    tc.Execution.DurationMs = time.Since(resultStart).Milliseconds()\n    tc.Execution.Files = result.Files\n\n    // Update incremental metrics on saved message\n    msg := &m.session.file.Messages[msgIdx]\n    if msg.SequenceStat != nil {\n        msg.SequenceStat.ExecDurMs += tc.Execution.DurationMs\n    }\n    msg.DurationTimeMs = time.Since(m.stream.metrics.Start).Milliseconds()\n\n    m.session.invalidateRenderAt(msgIdx)\n    m.updateViewportContent()\n}
Acceptance: Each tool execution updates the saved message's ToolCalls entry in place
Acceptance: After each execution, SequenceStat.ExecDurMs accumulates the tool's duration
Acceptance: DurationTimeMs is updated with the elapsed time from stream start
Acceptance: Render cache is invalidated at msgIdx and viewport updated after each tool
Acceptance: pendingEntries and pendingToolIndex fields are replaced by msgIdx tracking
Acceptance: If the app crashes mid-loop, the saved message has accurate metrics for all tools executed so far
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.3 - Simplify auth pause and reorder gates: file change before auth
Type: refactor
What: Remove stream.active=false from setAuthMode. Replace pendingEntries/pendingToolIndex in streamState with a single authMsgIndex field. Move file change validation gate to run before the authorization gate — if a file changed externally, fail immediately without asking for authorization.
Why: Keeping active=true avoids the disappearing-content bug. A single authMsgIndex is simpler than a whole entries slice. File change gate before auth avoids pointless authorization questions for files that have changed.
Files: ~ internal/app/stream.go
Snippet: // Updated gate order in the tool loop:\nfor i := startIndex; i < len(m.stream.partialTools); i++ {\n    tc := &m.session.file.Messages[msgIdx].ToolCalls[i]\n    p := m.stream.partialTools[i]\n    tool := m.toolReg.Get(p.name)\n    args := parseArgs(p.args)\n\n    // Gate 1: File change validation (BEFORE auth)\n    if isFileTool && pathVal != "" {\n        if err := tools.Validate(resolvedPath, sessionState); err != nil {\n            tc.Execution.Status = tools.ResultStatusError\n            tc.Execution.Error = "blocked: file changed externally"\n            // Cancel remaining\n            break\n        }\n    }\n\n    // Gate 2: Authorization (with on-demand preview)\n    if m.needsAuthorization(tool, args) {\n        // ... preview + setAuthMode\n    }\n\n    // Execute\n}\n\n// setAuthMode no longer sets active=false\nfunc (m *Model) setAuthMode() tea.Cmd {\n    m.stream.authMsgIndex = msgIdx\n    m.stream.authToolIndex = i\n    // ... rest unchanged\n}
Acceptance: setAuthMode no longer sets stream.active=false
Acceptance: pendingEntries field removed from streamState, replaced by authMsgIndex and authToolIndex
Acceptance: File change validation runs before authorization — tools on changed files fail immediately without auth prompt
Acceptance: Auth resume uses authMsgIndex to locate the saved message and mutate the right entry
Acceptance: Auth question overlay appears while the saved assistant message with tool entries remains visible
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.4 - Finalize remaining message metrics after loop completes
Type: refactor
What: After the tool loop finishes (all tools done, rejected, or cancelled), compute final metrics on the saved message: TokensPerSecond, InputTokens (TotalExecutionTokens), final DurationTimeMs, StopReason=tool_calls, and finalize SequenceStat.AvgTokensPerSec. Invalidate render cache.
Why: These metrics depend on the full set of executed tools and total duration. They are computed after the loop completes to have the full picture.
Files: ~ internal/app/stream.go
Snippet: // After loop completes, finalize metrics on the saved message\nmsg := &m.session.file.Messages[msgIdx]\nmsg.DurationTimeMs = time.Since(m.stream.metrics.Start).Milliseconds()\nmsg.TokensPerSecond = m.stream.metrics.AvgTokenPerSec()\nmsg.InputTokens = config.TotalExecutionTokens(msg.ToolCalls)\nmsg.StopReason = "tool_calls"\n\nif msg.SequenceStat != nil {\n    msg.SequenceStat.DurationMs = msg.DurationTimeMs\n    msg.SequenceStat.InputTokens = msg.InputTokens\n    msg.SequenceStat.OutputTokens = m.stream.metrics.TotalOutputTokens()\n    msg.SequenceStat.AvgTokensPerSec = msg.TokensPerSecond\n}\n\nm.session.invalidateRenderAt(msgIdx)\n
Acceptance: DurationTimeMs reflects total time from stream start through all tool executions
Acceptance: TokensPerSecond is correctly computed from total output tokens / inference duration
Acceptance: InputTokens is the sum of all tool execution result tokens
Acceptance: SequenceStat is finalized with complete execution durations and token counts
Acceptance: StopReason is set to tool_calls on the saved message
Acceptance: Render cache is invalidated so header shows final metrics
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 3 - Cleanup & Edge Cases
Pattern: Error Handling, State Management
Objective: Clean up render path, handle all error cases with the new eager-save model, and ensure crash resilience.
Success: Render path has no special auth-pause branching. Auth rejection, captured instructions, stream errors, and multi-round tool chains all correctly update the saved message.
Diagram: stateDiagram-v2
    [*] --> Streaming
    Streaming --> ToolsSaved: stream done, tool_calls
    ToolsSaved --> Executing: loop running
    Executing --> FileFail: file changed externally
    Executing --> PreviewFail: preview error
    Executing --> AuthPause: needs authorization
    AuthPause --> Executing: approved
    AuthPause --> Rejected: denied
    Executing --> Instructions: user provided instructions
    Executing --> LoopDone: all tools executed
    LoopDone --> Streaming: finalize metrics, start next stream
    LoopDone --> ChatMode: no more tool_calls

### TASK: 3.1 - Clean up render.go and remove auth-pause special cases
Type: refactor
What: Remove any authorizationCtx condition from the streaming render block in render.go. Since the assistant message is always saved before the tool loop, the streaming render path is only needed during active token streaming. The saved message handles everything else.
Why: With eager save, there's no more disappearing content during auth pause. The render path doesn't need branching for auth state.
Files: ~ internal/app/render.go
Snippet: // updateViewportContent - streaming block unchanged\n// No authorizationCtx condition needed\nif m.stream.active {\n    // ... existing streaming render logic (as-is)\n}\n\n// Remove the old workaround:\n// if m.stream.active || m.stream.authorizationCtx != nil {  <-- gone\n
Acceptance: render.go has no authorizationCtx condition in the streaming render block
Acceptance: The streaming render path only activates when stream.active is true
Acceptance: During auth pause, the saved message renders normally from the cached messages
Acceptance: No visual regression - assistant message with tools stays visible during auth
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.2 - Handle rejection, captured instructions, and stream errors with eager save
Type: bug
What: Update auth rejection, captured instructions, and stream error/cancellation handling to mutate the saved message. Rejection marks remaining tools as error. Captured instructions cancel remaining tools and append a user message. Stream errors during resumed streaming preserve the already-saved tool message.
Why: With eager save, all these paths need to update the already-saved message rather than a local entries slice. Crash resilience requires that rejected tools and error states are persisted.
Files: ~ internal/app/stream.go
Snippet: // Auth rejection: mark current + remaining as error in saved message\nif !result.Approved {\n    msg := &m.session.file.Messages[msgIdx]\n    msg.ToolCalls[i].Execution.Status = tools.ResultStatusError\n    msg.ToolCalls[i].Execution.Error = "rejected by user"\n    for j := i + 1; j < len(msg.ToolCalls); j++ {\n        msg.ToolCalls[j].Execution.Status = tools.ResultStatusError\n        msg.ToolCalls[j].Execution.Error = "cancelled: prior tool rejected"\n    }\n    break\n}\n\n// Captured instructions: cancel remaining, append user message\nif capturedInstructions != "" {\n    msg := &m.session.file.Messages[msgIdx]\n    for j := i + 1; j < len(msg.ToolCalls); j++ {\n        msg.ToolCalls[j].Execution.Status = tools.ResultStatusError\n        msg.ToolCalls[j].Execution.Error = "cancelled: user provided instructions"\n    }\n    m.session.appendMsg(userMsgWithInstructions)\n    break\n}\n\n// Stream error after tool loop: prior message is already saved\nif event.Error != nil {\n    m.session.appendMsg(syntheticErrorMsg(event.Error.Error()))\n    m.stream.reset()\n    return m, m.setChatMode()\n}
Acceptance: Rejected tool: entry shows error status in saved message, remaining tools show cancelled
Acceptance: Captured instructions: remaining tools cancelled in saved message, user instruction message appended after
Acceptance: Stream error after tool loop: previous assistant message with tool results remains intact
Acceptance: User cancellation during resumed stream: both the tool message and the partial new stream are handled
Acceptance: All mutations on the saved message trigger render cache invalidation and viewport update
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.3 - Ensure multi-round tool chains work with eager save
Type: bug
What: Verify multi-round tool chains (stream → tools → stream → tools) work correctly with eager save. Each round's assistant message is independently saved with its own msgIdx. File state tracking persists across rounds. The startStream function after loop completion works unchanged since the message is already saved.
Why: Multi-round tool chains are existing behavior that must be preserved. Each round independently saves its message before its own tool loop.
Files: ~ internal/app/stream.go
Snippet: // After loop completes and metrics are finalized:\nm.stream.reset()\nm.updateViewportContent()\nreturn m.startStream()\n\n// startStream builds API messages from current session state\n// (which now includes the saved assistant message with tool results)\n// and starts a new stream — works as before since the message is persisted\n
Acceptance: Each round's assistant message is independently saved before its tool loop
Acceptance: File state tracking (FileState map) continues to work correctly across rounds
Acceptance: startStream after loop completion sees the saved tool results in the message and builds API messages correctly
Acceptance: Multiple rounds of tool chains don't interfere with each other's msgIdx
Verification: cd ~/src/squid-os && go build ./...

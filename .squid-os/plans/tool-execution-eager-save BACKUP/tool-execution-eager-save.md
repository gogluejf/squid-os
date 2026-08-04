# EPIC: Eager Tool Execution: Save Assistant Message Before Tool Loop
Why: When tool execution pauses for authorization, the assistant message disappears from view because it hasn't been saved yet. This makes it impossible for the user to see what the assistant is about to do. Additionally, if the app crashes mid-tool-loop, all tool progress is lost.
Outcomes: 1. Assistant message saved immediately after stream ends, before any tool executes. 2. Tool entries visible with preview diffs during auth pause. 3. Crash resilience - executed tools are persisted. 4. Simplified render path - no special streaming-vs-paused conditions.

## MILESTONE: 1 - Message Model & Preview
Pattern: Value Object
Objective: Add a Preview method to the Tool interface for generating read-only Execution data (Files, Result) without side effects. Extend ToolCallEntry to support a 'pending' execution state with preview data.
Success: Tools with Preview produce FileEntry diffs and Result text before execution. ToolCallEntry renders correctly with status=pending and preview data in the UI.
Diagram: graph LR
  A[partialTool from stream] --> B[buildToolEntryWithPreview]
  B --> C{tool has Preview?}
  C -->|yes| D[Call tool.Preview]
  D --> E[Set Execution.Status=pending, Files, Result from preview]
  C -->|no| F[Set Execution.Status=pending, no preview data]
  E --> G[ToolCallEntry ready for save]
  F --> G

### TASK: 1.1 - Add Preview-aware tool entry builder
Type: feature
What: Add buildToolEntryWithPreview function that builds a ToolCallEntry from a partialTool, calling tool.Preview if available to pre-fill Execution with diffs and Result text, status=pending.
Why: Enables rendering preview diffs before the tool actually executes, so the user sees exactly what will happen during authorization.
Files: ~ internal/app/stream.go
Snippet: func buildToolEntryWithPreview(p partialTool, tool *tools.Tool, args map[string]interface{}) config.ToolCallEntry {\n    entry := buildInstructionEntry(p)\n    if tool != nil && tool.Preview != nil {\n        preview := tool.Preview(args)\n        entry.Execution.Status = ResultStatusPending\n        entry.Execution.Result = preview.Result\n        entry.Execution.Error = preview.Error\n        entry.Execution.Files = preview.Files\n        for j := range preview.Files {\n            preview.Files[j].ToolCallID = p.id\n        }\n    } else {\n        entry.Execution.Status = ResultStatusPending\n    }\n    return entry\n}
Acceptance: buildToolEntryWithPreview returns a ToolCallEntry with status=pending
Acceptance: For tools with Preview (write_file, edit_file), Execution.Files contains FileEntry with Diff
Acceptance: For tools without Preview (read_file, open, bash), Execution is pending but empty of Files/Result
Acceptance: No disk writes occur during preview -- FileEntry diff is computed from in-memory reads only
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.2 - Render preview diffs for pending tools
Type: feature
What: Update renderToolCallsInline in message.go to show file diffs when Execution.Status is 'pending' and Files are non-empty (currently only shows for 'success').
Why: Preview data is pre-populated for destructive tools. The diff should render alongside the pending status indicator so the user sees the expected changes during auth.
Files: ~ internal/ui/message.go
Snippet: // Diff is visible for both success and pending (with preview data)\nif (tc.Execution.Status == "success" || tc.Execution.Status == "pending") && len(tc.Execution.Files) > 0 {\n    if d := renderToolFilesDiff(tc.Execution.Files, boxWidth, t.Style); d != "" {\n        content = append(content, d)\n    }\n}
Acceptance: Pending tool entries with preview FileEntry diffs render the side-by-side diff in the UI
Acceptance: Pending tool entries without preview data (read_file, etc.) show no diff
Acceptance: The [ ] pending prefix indicator still appears for pending tools
Acceptance: Success status tools continue to render diffs as before
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 2 - Eager Save & In-Place Update
Pattern: Command Pattern, State Machine
Objective: Restructure resumeToolExecution to save the assistant message before the tool loop starts, then mutate the saved message's ToolCalls in place as each tool executes.
Success: After stream ends with tool_calls, the assistant message is immediately visible in the viewport. During auth pauses, the full message with tool entries remains visible. Each executed tool updates its entry in the saved message.
Diagram: sequenceDiagram
  participant S as Stream
  participant R as resumeToolExecution
  participant M as Saved Message
  participant U as User
  participant V as Viewport
  S->>R: stream done, stopReason=tool_calls
  R->>M: appendAssistantMsg with all tools as pending
  R->>V: updateViewportContent (message visible)
  loop for each tool i
    R->>R: check needsAuthorization
    alt needs auth
      R->>U: show Question overlay (message stays visible)
      U->>R: approve/reject
    end
    R->>M: mutate Messages[idx].ToolCalls[i].Execution
    R->>V: updateViewportContent
  end
  R->>M: update message-level metrics
  R->>R: stream.reset(), startStream()

### TASK: 2.1 - Move appendAssistantMsg before tool loop, return message index
Type: refactor
What: Change appendAssistantMsg to return the index of the appended message. Call it BEFORE the tool execution loop with all tool entries built via buildToolEntryWithPreview (status=pending). Metrics start minimal (DurationTimeMs=0, TokensPerSecond=0) and are updated in place.
Why: The message must be saved before any tool runs so it persists across auth pauses and survives crashes. Returning the index lets the loop mutate the right message in place.
Files: ~ internal/app/stream.go
Snippet: // In handleStreamEvent, when event.Done && stopReason==tool_calls:\nfunc (m *Model) resumeToolExecution() (tea.Model, tea.Cmd) {\n    // Build all tool entries with preview\n    entries := make([]config.ToolCallEntry, len(m.stream.partialTools))\n    for i, p := range m.stream.partialTools {\n        tool := m.toolReg.Get(p.name)\n        var args map[string]interface{}\n        if p.args != "" {\n            json.Unmarshal([]byte(p.args), &args)\n        }\n        entries[i] = buildToolEntryWithPreview(p, tool, args)\n    }\n\n    // Save message eagerly\n    msgIdx := m.appendAssistantMsg(config.Message{...entries...})\n\n    // Loop and mutate in place\n    for i := 0; i < len(entries); i++ {\n        m.session.file.Messages[msgIdx].ToolCalls[i] = entries[i]\n        // ... auth gate, execute, update entry, updateViewportContent\n    }\n}
Acceptance: appendAssistantMsg returns the index of the appended message
Acceptance: Message is saved before any tool executes, with all tool entries as pending
Acceptance: Message-level metrics (DurationTimeMs, TokensPerSecond) start at 0 and are updated after loop completes
Acceptance: SequenceStat is created with initial values and accumulated as tools execute
Acceptance: The viewport shows the full assistant message (text, thinking, tools) immediately after stream ends
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.2 - Refactor tool loop to mutate saved message in place
Type: refactor
What: Rewrite the tool execution loop to mutate session.file.Messages[msgIdx].ToolCalls[i] directly after each tool executes. Remove the local 'entries' slice as the source of truth -- use the saved message's ToolCalls instead. Update render cache at the message index after each mutation.
Why: Mutating the saved message means the viewport always reflects current state. No need for pendingEntries or separate local tracking during auth pauses.
Files: ~ internal/app/stream.go
Snippet: // Simplified loop - mutate saved message directly\nfor i := startIndex; i < len(m.stream.partialTools); i++ {\n    tc := &m.session.file.Messages[msgIdx].ToolCalls[i]\n    p := m.stream.partialTools[i]\n    tool := m.toolReg.Get(p.name)\n    args := parseArgs(p.args)\n\n    // Auth gate - message stays visible with tc as pending\n    if m.needsAuthorization(tool, args) {\n        // ... setAuthMode, return\n    }\n\n    // Execute\n    result := tool.Execute(args)\n    tc.Execution.Status = result.Status\n    tc.Execution.Result = result.Result\n    tc.Execution.Error = result.Error\n    tc.Execution.Tokens = countTokensApprox(content)\n    tc.Execution.DurationMs = elapsed\n    tc.Execution.Files = result.Files\n\n    // Invalidate render cache for this message so it re-renders\n    m.session.invalidateRenderAt(msgIdx)\n    m.updateViewportContent()\n}
Acceptance: Each tool execution updates the saved message's ToolCalls entry in place
Acceptance: After each execution, the render cache is invalidated and viewport updated
Acceptance: The pendingEntries field on streamState is no longer needed
Acceptance: When auth is needed, the tool entry remains in pending state in the saved message
Acceptance: After auth approval, execution fills in the same entry that was already saved
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.3 - Simplify setAuthMode and auth resume flow
Type: refactor
What: Remove m.stream.active=false from setAuthMode. Simplify OnConfirm/OnCancel callbacks to use the message index directly instead of passing pendingEntries. Remove the pendingEntries and pendingToolIndex fields from streamState since the saved message is now the source of truth.
Why: With the message already saved, the auth pause doesn't need to track a separate entries slice. The pending state lives in the saved message. Removing active=false keeps the streaming render path simple.
Files: ~ internal/app/stream.go
Snippet: // setAuthMode no longer sets active=false\nfunc (m *Model) setAuthMode() tea.Cmd {\n    ctx := m.stream.authorizationCtx\n    msgIdx := m.stream.authMsgIndex  // index of the saved message\n    toolIdx := m.stream.authToolIndex\n\n    q := &component.Question{\n        OnConfirm: func(selection int, instructions string, ctx any) tea.Cmd {\n            m := ctx.(*Model)\n            m.stream.authorizationCtx.Result = AuthResult{...}\n            // Resume from toolIdx, message already saved at msgIdx\n            return m.resumeToolExecution(toolIdx)\n        },\n    }\n    m.setComponent(q)\n    m.updateViewportContent()\n    return q.BlinkCmd()\n}
Acceptance: setAuthMode no longer sets stream.active=false
Acceptance: Auth question overlay appears while the saved assistant message with tool entries remains visible in viewport
Acceptance: pendingEntries and pendingToolIndex fields removed from streamState
Acceptance: Auth resume continues the loop from the correct tool index, mutating the saved message
Acceptance: stream.reset() is still called after the loop completes, clearing state for the next stream
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.4 - Update message metrics in place after loop completes
Type: refactor
What: After the tool loop finishes, update the saved message's metrics in place: DurationTimeMs, TokensPerSecond, InputTokens, StopReason, ToolCallMetrics, and SequenceStat. Invalidate render cache and re-render.
Why: Metrics were previously computed at append time (which was after the loop). Now the message is saved before the loop, so metrics need a final update pass.
Files: ~ internal/app/stream.go
Snippet: // After loop completes\nfunc (m *Model) finalizeToolMessage(msgIdx int) {\n    msg := &m.session.file.Messages[msgIdx]\n    msg.DurationTimeMs = m.stream.metrics.Duration().Milliseconds()\n    msg.TokensPerSecond = m.stream.metrics.AvgTokenPerSec()\n    msg.InputTokens = config.TotalExecutionTokens(msg.ToolCalls)\n    msg.StopReason = "tool_calls"\n    msg.ToolCallMetrics = config.ContentMetrics{...}\n\n    // Update SequenceStat\n    if msg.SequenceStat != nil {\n        msg.SequenceStat.DurationMs = msg.DurationTimeMs\n        msg.SequenceStat.AvgTokensPerSec = msg.TokensPerSec\n        msg.SequenceStat.InputTokens = msg.InputTokens\n        msg.SequenceStat.ExecDurMs = sumExecDurations(msg.ToolCalls)\n    }\n\n    m.session.invalidateRenderAt(msgIdx)\n    m.updateViewportContent()\n}
Acceptance: DurationTimeMs reflects actual total duration from stream start through all tool executions
Acceptance: TokensPerSecond is correctly computed from total output tokens / inference duration
Acceptance: InputTokens reflects sum of all tool execution result tokens
Acceptance: SequenceStat is updated with final execution durations and token counts
Acceptance: ToolCallMetrics has correct totals
Acceptance: Render cache is invalidated so the header shows final metrics
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.5 - Clean up render.go - remove streaming special cases for auth pause
Type: refactor
What: Remove the stream.active guard workaround from render.go. Since the assistant message is always saved before the tool loop, the streaming render path in updateViewportContent is only needed for active streaming (during the initial token flow), not for auth pauses. Clean up any leftover conditions related to authorizationCtx in the render path.
Why: With eager save, the streaming content is always in the saved message during auth pauses. The render path doesn't need special branching for auth state anymore.
Files: ~ internal/app/render.go
Snippet: // updateViewportContent - simplified streaming block\n// Only renders streaming content when actively streaming (during token flow)\n// Auth pauses show the saved message directly - no special case needed\nif m.stream.active {\n    // ... existing streaming render logic (unchanged)\n}
Acceptance: render.go has no authorizationCtx condition in the streaming render block
Acceptance: The streaming render path only activates when stream.active is true (during token flow)
Acceptance: During auth pause, the saved message renders normally from the cached messages
Acceptance: No visual regression - assistant message with tools stays visible during auth
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 3 - Integration & Edge Cases
Pattern: Error Handling, State Management
Objective: Handle error cases, user cancellation, crash recovery, and captured instructions with the new eager-save model.
Success: All error paths work correctly: rejected auth cancels remaining tools, captured instructions create a user message and cancel remaining tools, stream cancellation preserves already-executed tools, and crashes don't lose progress.
Diagram: stateDiagram-v2
    [*] --> Streaming
    Streaming --> ToolsSaved: stream done, tool_calls
    ToolsSaved --> Executing: loop running
    Executing --> AuthPause: needs authorization
    AuthPause --> Executing: approved
    AuthPause --> Rejected: denied
    Rejected --> LoopDone: remaining cancelled
    Executing --> Instructions: user provided instructions
    Instructions --> LoopDone: remaining cancelled
    Executing --> LoopDone: all done
    LoopDone --> Streaming: start next stream
    LoopDone --> ChatMode: no tool_calls in response

### TASK: 3.1 - Handle auth rejection and captured instructions with eager save
Type: bug
What: Update auth rejection and captured instructions handling: when rejected, set remaining tool entries to error status in the saved message. When instructions are provided, cancel remaining tools and append a user message with the instructions.
Why: With eager save, rejected tools and instruction-injected user messages need to update the already-saved message and optionally append a new message.
Files: ~ internal/app/stream.go
Snippet: // Auth rejection - mark remaining as error in saved message\nif !result.Approved {\n    tc := &m.session.file.Messages[msgIdx].ToolCalls[i]\n    tc.Execution.Status = tools.ResultStatusError\n    tc.Execution.Error = "rejected by user"\n    for j := i + 1; j < len(m.session.file.Messages[msgIdx].ToolCalls); j++ {\n        m.session.file.Messages[msgIdx].ToolCalls[j].Execution.Status = tools.ResultStatusError\n        m.session.file.Messages[msgIdx].ToolCalls[j].Execution.Error = "cancelled: prior tool rejected"\n    }\n    m.session.invalidateRenderAt(msgIdx)\n    break\n}\n\n// Captured instructions - cancel remaining, append user message\nif capturedInstructions != "" {\n    // ... cancel remaining in saved message\n    m.session.appendMsg(userMsgWithInstructions)\n    break\n}
Acceptance: Rejected tool: entry shows error status, remaining tools show cancelled
Acceptance: Captured instructions: remaining tools cancelled, user message appended after assistant message
Acceptance: Saved message is updated and re-rendered in both cases
Acceptance: After rejection or instructions, loop completes, metrics finalized, next stream starts (or chat mode if no more tool calls)
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.2 - Handle stream error and cancellation mid-tool-loop
Type: bug
What: Update stream error handling in handleStreamEvent and user cancellation (ctrl+c) during tool execution. When an error occurs mid-loop, the already-saved message stays with its partial results. Error or abort messages are appended as synthetic messages.
Why: With eager save, a stream error during the resumed stream (after tools) should not lose the tool results from the previous loop. The saved message with executed tools is already persisted.
Files: ~ internal/app/stream.go
Snippet: // Stream error during resumed stream after tool loop\n// The previous assistant message with tool results is already saved - no need to recover\n// Just append error message and return to chat mode\nif event.Error != nil {\n    // If we were in the middle of a resumed stream after tools,\n    // the prior message is already saved with its tool results\n    errText := "Stream error: " + event.Error.Error()\n    m.session.appendMsg(syntheticErrorMsg(errText))\n    m.stream.reset()\n    m.updateViewportContent()\n    return m, m.setChatMode()\n}
Acceptance: Stream error after tool loop: previous assistant message with executed tools remains visible
Acceptance: Error is shown as a synthetic message after the tool results
Acceptance: User cancellation during tool execution: partial tool results are preserved in the saved message
Acceptance: User cancellation during resumed stream: both the tool message and the partial new stream are handled gracefully
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.3 - Handle multi-round tool chains and file change validation
Type: bug
What: Ensure the file change validation gate works with eager save: when a file has changed externally, mark the tool as error in the saved message and cancel remaining tools. Ensure multi-round tool chains (stream -> tools -> stream -> tools) work correctly with the new model.
Why: The file change gate and multi-round tool chains are existing behaviors that must be preserved with the refactored eager-save model.
Files: ~ internal/app/stream.go
Snippet: // File change validation - mark in saved message\nif err := tools.Validate(resolvedPath, sessionState); err != nil {\n    tc := &m.session.file.Messages[msgIdx].ToolCalls[i]\n    tc.Execution.Status = tools.ResultStatusError\n    tc.Execution.Error = fmt.Sprintf("blocked: file changed externally: %s", resolvedPath)\n    // Cancel remaining in saved message\n    for j := i + 1; j < len(m.session.file.Messages[msgIdx].ToolCalls); j++ {\n        m.session.file.Messages[msgIdx].ToolCalls[j].Execution.Status = tools.ResultStatusError\n        m.session.file.Messages[msgIdx].ToolCalls[j].Execution.Error = "cancelled: prior tool failed"\n    }\n    m.session.invalidateRenderAt(msgIdx)\n    break\n}
Acceptance: File changed externally: tool entry shows error in saved message, remaining tools cancelled
Acceptance: Multi-round tool chain: after tool loop completes, startStream() creates a new stream that eventually produces another assistant message (possibly with more tools)
Acceptance: Each round's assistant message is independently saved and tracked
Acceptance: File state tracking (FileState map) continues to work correctly across rounds
Verification: cd ~/src/squid-os && go build ./...

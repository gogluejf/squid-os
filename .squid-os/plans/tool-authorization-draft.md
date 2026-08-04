# EPIC: Tool Authorization Mode

Add per-tool user authorization before execution with three configurable modes: auto, ask-on-write, and ask-for-all. Includes destructive flag on bash, per-tool confirmation prompts with optional instruction injection, and cancellation of pending tools when the user interrupts.

## MILESTONE: Domain — Authorization Model

Pattern: Feature Flag + Guard
Diagram: settings.Authorization determines whether each tool call triggers an authorization gate before execution. The gate checks tool destructiveness against the mode policy.

### TASK: Add Authorization setting to Settings

What: Add `Authorization string` field to `config.Settings` with values `auto` (default), `ask-on-write`, `ask-for-all`. Update `DefaultSettings()` and `LoadSettings()` to handle legacy configs missing the field.
Why: Central config point for the authorization mode, persists across sessions.
Files:
- `~ internal/config/settings.go`
Snippet:
```go
type Settings struct {
    // ...existing fields...
    Authorization string `json:"authorization"` // "auto", "ask-on-write", "ask-for-all"
}

func DefaultSettings() Settings {
    return Settings{
        // ...existing...
        Authorization: "auto",
    }
}
```
Verification: `go build ./...` succeeds; default settings produce `Authorization == "auto"`.

### TASK: Add Destructive property to Tool contract

What: Add `Destructive bool` field to the `Tool` struct in `tools/tools.go`. A helper `IsDestructive(args map[string]interface{}) bool` that returns true for inherently destructive tools AND for bash when `args["destructive"] == true`. Bash returns error if `destructive` key is missing.
Why: Central way to query whether a tool call modifies disk state, needed by the authorization gate.
Files:
- `~ internal/tools/tools.go`
Snippet:
```go
type Tool struct {
    // ...existing...
    Destructive func(args map[string]interface{}) (bool, error) // nil = never destructive
}

// For bash: checks args["destructive"]; returns error if missing
func init() {
    Bash.Destructive = func(args map[string]interface{}) (bool, error) {
        v, ok := args["destructive"]
        if !ok {
            return false, fmt.Errorf("missing required parameter: destructive")
        }
        b, ok := v.(bool)
        if !ok {
            return false, fmt.Errorf("destructive must be a boolean")
        }
        return b, nil
    }
    WriteFile.Destructive = constantDestructive(true)
    EditFile.Destructive = constantDestructive(true)
    SkillBuild.Destructive = constantDestructive(true)
}
```
Verification: Calling `Bash.Execute` without `destructive` returns error status.

### TASK: Update bash schema to require destructive

What: Add `destructive` to bash schema properties, mark as required. Update description to explain it signals file-disk modification (writes, deletes, moves, creates).
Why: The LLM must declare intent; no defensive defaults per user requirement.
Files:
- `~ internal/tools/bash.go`
Snippet:
```json
"destructive": {
    "type": "boolean",
    "description": "Set to true if this command modifies files, deletes, creates, or changes system state. Set to false for read-only operations (cat, ls, grep, find, git status, etc). Required."
}
```
And add `"destructive"` to the `"required"` array.
Verification: Schema is valid JSON; required array includes `"command"` and `"destructive"`.

### TASK: Update system prompt for destructive guidance

What: Add a section to the system prompt explaining when to set `destructive: true` vs `false` for bash calls, with concrete examples.
Why: The LLM needs clear guidance to set the flag correctly.
Files:
- `~ internal/config/sys-prompt.go`
Snippet:
```
- Bash destructive flag (required):
  - Set "destructive": true for: rm, mv, cp, mkdir, rmdir, chmod, chown, git add/commit/push, install commands, any write/modify operation.
  - Set "destructive": false for: cat, ls, find, grep, git status, git log, echo, df, du, ps, read-only operations.
```
Verification: `go build ./...` succeeds.

## MILESTONE: Application — Authorization Gate and Flow

Pattern: Interrupt-Resume with State Machine
Diagram:
```
stream DONE(tool_calls)
  → build partialTool list
  → for each tool:
      → check authorization needed?
        NO → execute, store result, continue to next tool
        YES → save pending tools to stream.pendingAuthorization
             → switch to ModeAuthorize
             → await user response
              → y → execute tool, check for injected text
                  HAS TEXT → inject user message, cancel remaining tools, resume stream
                  NO TEXT  → execute, continue to next tool
              → n → set REJECTED result, check for injected text
                  HAS TEXT → inject user message, cancel remaining tools, resume stream
                  NO TEXT  → continue to next tool
```

### TASK: Add ModeAuthorize to modes

What: Add `ModeAuthorize` to the Mode enum in `modes.go` with string `"authorize"`.
Why: New TUI mode for the authorization prompt overlay.
Files:
- `~ internal/app/modes.go`
Snippet:
```go
const (
    // ...existing...
    ModeAuthorize                 // Authorization prompt
)
```
Verification: Builds cleanly.

### TASK: Add authorization state to streamState

What: Add fields to `streamState` for the authorization gate:
- `pendingTools []partialTool` — tools remaining to evaluate/execute in this batch
- `authTool *partialTool` — the current tool being authorized
- `authSelected bool` — true = yes, false = no (default false/yes based on mode)
- `authInTextMode bool` — true if user is typing instructions
- `authText string` — accumulated instruction text
- `authRejectMessage string` — the fixed rejection result text
Why: The stream state needs to hold authorization context across the input round-trip.
Files:
- `~ internal/app/stream.go`
Snippet:
```go
type streamState struct {
    // ...existing...
    // Authorization gate
    pendingTools    []partialTool      // tools left to process in this batch
    authTool        *partialTool       // tool currently under authorization
    authSelected    bool               // true=yes, false=no
    authInTextMode  bool               // user is typing instruction text
    authText        string             // instruction text being typed
}
```
Verification: Builds cleanly.

### TASK: Implement authorization check and gate in executeTools

What: Rewrite `executeTools` in `stream.go` to check authorization per-tool based on `settings.Authorization` mode. For `auto`, execute all (current behavior). For `ask-on-write`, check `tool.Destructive(args)`. For `ask-for-all`, always prompt. When authorization is needed, save remaining tools to `streamState.pendingTools` and return a special signal (nil entries) that tells `handleStreamEvent` to enter `ModeAuthorize`.
Why: The core authorization logic — decides when to gate and what to do.
Files:
- `~ internal/app/stream.go`
Snippet:
```go
func (m *Model) executeTools(partials []partialTool) []config.ToolCallEntry {
    // ...existing file tracker setup...
    m.stream.pendingTools = partials // start with all
    
    for i, p := range partials {
        // ...build entry...
        needsAuth := false
        switch m.settings.Authorization {
        case "ask-for-all":
            needsAuth = true
        case "ask-on-write":
            if tool != nil && tool.Destructive != nil {
                isDestructive, _ := tool.Destructive(args) // error handled during Execute
                needsAuth = isDestructive
            }
        }
        if needsAuth {
            m.stream.authTool = &p
            m.stream.authSelected = true // default to yes
            // Save remaining tools (current + rest) as pending
            m.stream.pendingTools = partials[i:]
            return nil // signal: enter ModeAuthorize
        }
        // ...existing execute logic...
    }
    m.stream.pendingTools = nil
    return entries
}
```
Verification: `go build ./...` succeeds; in auto mode, behavior unchanged.

### TASK: Handle authorization entry in handleStreamEvent

What: In `handleStreamEvent`, when `executeTools` returns `nil`, switch to `ModeAuthorize`, set up the authorization state from `streamState`, and render. When `executeTools` returns entries (some or all executed), handle as before but check if `pendingTools` is non-empty — if so, the remaining were cancelled.
Why: Bridges the stream event loop with the authorization mode.
Files:
- `~ internal/app/stream.go`
Snippet:
```go
if toolEntries == nil {
    // Authorization gate engaged
    m.mode = ModeAuthorize
    m.stream.active = false // pause streaming
    m.updateViewportContent()
    return m, nil
}
// If pendingTools has entries that weren't executed, mark them as cancelled
if len(m.stream.pendingTools) > 0 {
    // Add cancellation results for skipped tools
    for _, p := range m.stream.pendingTools {
        // ...add cancelled entry...
    }
}
```
Verification: In ask-on-write mode with a destructive tool, the prompt appears.

### TASK: Handle authorization key input

What: Add `handleAuthorizeKey` in `input.go` that processes the authorization prompt:
- `←` / `right`: toggle yes/no selection
- `Enter`: submit current selection (execute or reject)
- `Tab`: enter text mode (switch to textarea-like input)
- `Shift+Tab`: exit text mode back to yes/no
- `Esc`: reject without text
- In text mode: normal character input, backspace, etc.
After submission, call `resolveAuthorization(accepted bool, text string)` which:
  1. If accepted → execute the tool normally
  2. If rejected → set result to `{Status: "error", Error: "Rejected by user"}`
  3. If text is non-empty → inject a user message after execution, cancel remaining tools, resume stream
  4. If text is empty → continue to next pending tool
Why: The interactive authorization flow.
Files:
- `~ internal/app/input.go`
Snippet:
```go
func (m Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch {
    case msg.Type == tea.KeyLeft:
        m.authSelected = false
        m.updateViewportContent()
        return m, nil
    case msg.Type == tea.KeyRight:
        m.authSelected = true
        m.updateViewportContent()
        return m, nil
    case key.Matches(msg, keys.Tab):
        m.authInTextMode = true
        m.updateViewportContent()
        return m, nil
    case key.Matches(msg, keys.ShiftTab):
        m.authInTextMode = false
        m.updateViewportContent()
        return m, nil
    case msg.Type == tea.KeyEnter:
        if m.authInTextMode {
            return m.resolveAuthorization(m.authSelected, m.authText)
        }
        return m.resolveAuthorization(m.authSelected, "")
    // ...text mode character handling...
    }
    return m, nil
}
```
Verification: Arrow keys toggle selection; Tab enters text mode; Shift+Tab returns; Enter submits.

### TASK: Add authorization key bindings

What: Add `ShiftTab` key binding to `keymap.go` for the authorization prompt.
Why: Needed for switching between yes/no and text input.
Files:
- `~ internal/app/keymap.go`
Snippet:
```go
type keyMap struct {
    // ...existing...
    ShiftTab key.Binding
}
var keys = keyMap{
    // ...existing...
    ShiftTab: key.NewBinding(
        key.WithKeys("shift+tab"),
        key.WithHelp("shift+tab", "back to confirm"),
    ),
}
```
Verification: Builds cleanly.

### TASK: Implement resolveAuthorization

What: The core resolution logic that executes/rejects the tool, handles text injection, cancels remaining tools, and resumes the stream.
  1. Execute or reject the authorized tool
  2. If text is non-empty:
     - Append the tool result to the assistant message
     - Inject a synthetic user message with the text
     - Cancel remaining pending tools with "Cancelled due to user interruption"
     - Resume stream
  3. If text is empty:
     - Continue processing remaining pending tools (which may also need authorization)
Why: This is the "break the assistant turn" mechanic — user instructions inject a new turn.
Files:
- `~ internal/app/stream.go`
Snippet:
```go
func (m *Model) resolveAuthorization(accepted bool, text string) (tea.Model, tea.Cmd) {
    p := *m.stream.authTool
    tool := m.toolReg.Get(p.name)
    entry := buildToolEntry(p) // existing logic
    
    if accepted {
        // Validate checksum, execute
        result := tool.Execute(args)
        entry.Execution = result
    } else {
        entry.Execution.Status = "error"
        entry.Execution.Error = "Rejected by user"
    }
    
    m.stream.authTool = nil
    m.stream.authInTextMode = false
    m.stream.authText = ""
    
    if text != "" {
        // Inject user message, cancel remaining
        userMsg := config.Message{Role: RoleUser, Text: text, ...}
        m.session.appendMsg(entry)...
        m.session.appendMsg(userMsg)
        for _, remaining := range m.stream.pendingTools[1:] {
            // Mark as cancelled
        }
        m.stream.pendingTools = nil
        return (&m).startStream()
    }
    
    // No text: continue with remaining tools
    m.stream.pendingTools = m.stream.pendingTools[1:]
    if len(m.stream.pendingTools) > 0 {
        return (&m).continueToolExecution(entry)
    }
    // All done, resume stream
    return (&m).startStream()
}
```
Verification: Accepted tool executes; rejected tool gets error; text injection resumes stream with user message; remaining tools are cancelled.

## MILESTONE: Interface — Authorization Prompt UI

Pattern: Inline Prompt Overlay
Diagram: The authorization prompt renders in the footer/status bar area, replacing the normal footer during ModeAuthorize.

### TASK: Render authorization prompt in footer

What: In `render.go`, when `mode == ModeAuthorize`, render the authorization prompt in the footer area showing:
- Tool name and display params (e.g., `bash(rm -rf /tmp/*)`)
- `[←] No` or `Yes [→]` with highlighted current selection
- `tab: add instructions` hint
- If in text mode: show the text being typed with cursor, and `shift+tab: review` hint
Why: The user-facing authorization interface.
Files:
- `~ internal/app/render.go`
- `~ internal/ui/footer.go`
Snippet:
```go
if m.mode == ModeAuthorize {
    footer = renderAuthorizePrompt(m)
}
```
The prompt shows:
```
⚠ bash  destructive: true, command: rm -rf /tmp/*
  → [No] Yes    tab: instructions  shift+tab: review  enter: confirm
```
When in text mode:
```
⚠ bash(rm -rf /tmp/*)  → [No] Yes
  Instructions: type here...▌   shift+tab: review  enter: submit
```
Verification: The prompt renders correctly in both yes/no and text modes.

### TASK: Add text input handling in text mode

What: In `handleAuthorizeKey`, when `authInTextMode` is true, handle runes, backspace, delete, and cursor movement for the instruction text field.
Why: The user needs to type instructions that get injected as a user message.
Files:
- `~ internal/app/input.go`
Snippet:
```go
if m.authInTextMode {
    switch {
    case msg.Type == tea.KeyRunes:
        m.authText += string(msg.Runes)
    case msg.Type == tea.KeyBackspace:
        // remove last character
    case msg.Type == tea.KeyEnter:
        return m.resolveAuthorization(m.authSelected, m.authText)
    // ...cursor keys...
    }
}
```
Verification: Typing in text mode accumulates text; backspace removes; enter submits.

## MILESTONE: Integration — Wire Everything Together

Pattern: Composite
Diagram: settings → executeTools → authorization gate → handleAuthorizeKey → resolveAuthorization → resume stream.

### TASK: Update handleKey to dispatch ModeAuthorize

What: Add `case ModeAuthorize` in `handleKey` in `input.go` to dispatch to `handleAuthorizeKey`.
Why: The update loop needs to route authorization key events.
Files:
- `~ internal/app/input.go`
Snippet:
```go
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch m.mode {
    // ...existing...
    case ModeAuthorize:
        return m.handleAuthorizeKey(msg)
    }
}
```
Verification: In ask-on-write mode, arrow keys and enter work during authorization.

### TASK: Handle cancellation of remaining tools

What: When the user provides text instructions (breaking the turn), remaining pending tools need to be recorded with a cancellation status. Add `ResultStatusCancelled = "cancelled"` to `tools/tools.go`. The cancellation result is `"Cancelled: user interrupted with additional instructions"`.
Why: The LLM needs to know that pending tools were not executed because the user interrupted.
Files:
- `~ internal/tools/tools.go`
- `~ internal/app/stream.go`
Snippet:
```go
const ResultStatusCancelled = "cancelled"

// In resolveAuthorization, for each remaining tool:
for _, p := range m.stream.pendingTools[1:] {
    entry := buildToolEntry(p)
    entry.Execution.Status = tools.ResultStatusCancelled
    entry.Execution.Error = "Cancelled: user interrupted with additional instructions"
    // append to the assistant message tool calls
}
```
Verification: When user injects text, remaining tools show as cancelled in the session.

### TASK: Update handleKey dispatch for ModeAuthorize in handleKey

What: Add the `ModeAuthorize` case to `handleKey` and ensure `handleAuthorizeKey` delegates text input correctly in both modes.
Why: Integration point between the mode router and the authorization handler.
Files:
- `~ internal/app/input.go`
Verification: `go build ./...` succeeds; the full authorization flow works end-to-end.

### TASK: E2E integration test scenarios

What: Verify the complete flow through manual testing:
1. `ask-on-write` + bash with `destructive: true` → prompt appears, yes executes, no rejects
2. `ask-on-write` + bash with `destructive: false` → executes without prompt
3. `ask-on-write` + read_file → executes without prompt
4. `ask-for-all` + read_file → prompt appears
5. `auto` + any tool → no prompt
6. Authorization with Shift+Tab text injection → tool executes, user message injected, stream resumes
7. Multiple tools in batch with authorization → first prompt appears, after resolution, next pending tool is evaluated
8. Bash without `destructive` field → tool returns error
Why: End-to-end verification of the feature.
Files: (no code changes, manual verification)
Verification: All 8 scenarios behave as expected.

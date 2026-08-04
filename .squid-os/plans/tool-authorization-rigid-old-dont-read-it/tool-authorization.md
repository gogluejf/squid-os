# Tool Authorization Modes

## Core Problem

Users need control over what the AI can modify on their filesystem. Different workflows demand different trust levels — from fully automated to fully supervised.

## Goal

Three authorization modes (auto, ask-on-write, ask-for-all), destructive field enforcement on bash, interactive yes/no confirmation with optional inline instructions, and cancellation of pending tool calls when the user interrupts.

---

## 1. Domain

- **Pattern:** Value Object

**Objective:** Define authorization modes and destructive classification types

**Success Criteria:** Settings persists authorization mode, config exposes AuthorizeMode type

```mermaid
Settings.AuthorizeMode (enum: auto | ask-on-write | ask-for-all) -> loaded at startup -> drives all tool execution decisions
```

### 1.1. Add AuthorizeMode to settings

**What:** Add AuthorizeMode string type and field to config.Settings, with constants for auto, ask-on-write, ask-for-all. Update DefaultSettings and validation in LoadSettings.

**Why:** The user needs a configurable setting to select the authorization policy. Defaults to auto (current behavior) for backward compatibility.

**Files:**

- ~ internal/config/settings.go

**Snippet:**

```
const (
	AuthorizationAuto      = "auto"
	AuthorizationAskOnWrite = "ask-on-write"
	AuthorizationAskForAll  = "ask-for-all"
)

type AuthorizeMode string

func (a AuthorizeMode) IsValid() bool {
	switch a {
	case AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll:
		return true
	}
	return false
}
```

```
Settings struct gains:
	Authorization AuthorizeMode \x60json:"authorization"\x60
```

**Acceptance Criteria:**

- [ ] Default is auto (backward compatible)
- [ ] LoadSettings rejects invalid values, falls back to auto
- [ ] SaveSettings round-trips the field

**Verify:**

```bash
go build ./...
```

```bash
cat ~/.config/squid-os/settings.json | grep authorization
```

---

## 2. Infrastructure

- **Pattern:** Port/Adapter

**Objective:** Bash tool declares destructive intent, classification logic for all tools

**Success Criteria:** Bash schema requires destructive bool, each tool is classifiable as destructive or not

```mermaid
Tool Schema.destructive (bash only) -> IsDestructive(tool, args) bool -> drives authorization gate
```

### 2.1. Add destructive to bash schema (required, no default)

**What:** Add the 'destructive' boolean field to the bash tool's JSON schema as a required field. Update the Execute function to return an error if 'destructive' is missing from args. Store the value in ToolResult for downstream consumption.

**Why:** The LLM must explicitly declare whether a bash command modifies state. No default — the tool fails if omitted, forcing the LLM to be deliberate.

**Files:**

- ~ internal/tools/bash.go

**Snippet:**

```
Schema properties addition:
"destructive": {
  "type": "boolean",
  "description": "true if the command modifies files, writes, deletes, or changes system state. false for read-only commands. REQUIRED - must be explicitly set."
}
Required array addition: "destructive"
```

```
In Execute:

destructive, ok := args["destructive"].(bool)
if !ok {
	return ToolResult{Status: ResultStatusError, Error: "destructive is required and must be a boolean"}
}
```

```
ToolResult gains a field to carry destructive classification (or check in caller from args)
```

**Acceptance Criteria:**

- [ ] bash without destructive arg returns error
- [ ] bash with destructive: true/false parses and executes normally

**Verify:**

```bash
go test ./internal/tools/...
```

```bash
go build ./...
```

### 2.2. Add IsDestructive classification to Tool

**What:** Add IsDestructive bool field to the Tool struct. Set it on write_file, edit_file, skill_build (true) and read_file, bash, skill_list, open, set_working_dir (false - bash determined at runtime from args). Create IsDestructiveTool(tool, args) helper that checks both the static classification and, for bash, the runtime args[destructive] value.

**Why:** The authorization layer needs a single way to determine whether a specific tool call with specific args is destructive or not.

**Files:**

- ~ internal/tools/tools.go

**Snippet:**

```
Tool struct gains:
	IsDestructive bool \x60json:"destructive"\x60
```

```
var IsDestructiveTool = func(t *Tool, args map[string]interface{}) bool {
	if t.IsDestructive {
		return true
	}
	if t.Name == "bash" {
		if d, ok := args["destructive"].(bool); ok {
			return d
		}
	}
	return false
}
```

**Acceptance Criteria:**

- [ ] write_file, edit_file, skill_build always return true
- [ ] bash with destructive:true returns true
- [ ] bash with destructive:false returns false
- [ ] read_file, skill_list, open, set_working_dir always return false

**Verify:**

```bash
go build ./...
```

---

## 3. Application

- **Pattern:** State Machine

**Objective:** Per-tool authorization gate with batch interruption and user-injected messages

**Success Criteria:** Stream pauses on authorization, user responds, pending tools cancelled with error, injected user message resumes stream

```mermaid
stream → tool_calls → ForEach tool: shouldAsk?(mode, args) → yes: ModeAuthorize → user: y/n+text → execute or reject → pending tools: CANCELLED → resume stream with optional user msg
```

### 3.1. Add ModeAuthorize to app modes

**What:** Add ModeAuthorize to the Mode enum in modes.go. This mode interrupts the streaming flow to present a yes/no confirmation. Add authorization state to streamState: pendingTool (index of tool being authorized), pendingInstruction (text typed by user before confirming), inInstructionMode (bool - whether user is typing text vs selecting y/n).

**Why:** We need a distinct UI mode that suspends streaming, shows the authorization prompt, and collects user intent before resuming.

**Files:**

- ~ internal/app/modes.go
- ~ internal/app/stream.go

**Snippet:**

```
const (
	...
	ModeAuthorize       // Authorization confirmation overlay
)
```

```
streamState additions:
	authPendingIdx       int        // index of tool awaiting authorization (-1 = none)
	authInInstruction    bool       // user is typing inline instructions
	authInstructionText  string     // captured instruction text
	authPendingCancelled []int      // indices of tools to mark CANCELLED
```

**Acceptance Criteria:**

- [ ] ModeAuthorize string returns "authorize"
- [ ] streamState has new authorization fields

**Verify:**

```bash
go build ./...
```

### 3.2. Implement authorization gate in executeTools

**What:** Restructure executeTools to process tools one at a time when authorization is needed. Before each tool: check the authorization mode (auto/ask-on-write/ask-for-all) and IsDestructiveTool. If authorization is needed, save state into streamState.authPendingIdx, switch to ModeAuthorize, and RETURN from executeTools (stream is paused). On resume from ModeAuthorize (see Task 3.3), continue from the saved index. If user rejected with instructions, execute the tool (or mark rejected), inject user message, cancel remaining tools with status error 'CANCELLED due to user interruption', and return.

**Why:** The current batch execution model must change: we now gate per-tool, pause the assistant turn, let the user respond, then resume. This is the core behavioral change.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) shouldAuthorize(t *tools.Tool, args map[string]interface{}) bool {
	switch m.settings.Authorization {
	case config.AuthorizationAuto:
		return false
	case config.AuthorizationAskForAll:
		return true
	case config.AuthorizationAskOnWrite:
		return tools.IsDestructiveTool(t, args)
	}
	return false
}
```

```
In executeTools loop, before tool execution:
	if m.shouldAuthorize(tool, args) {
		// Set up authorization state and return
		m.stream.authPendingIdx = i
		m.stream.authInInstruction = false
		m.stream.authInstructionText = ""
		return nil // signal to handleStreamEvent that we need auth
	}
```

**Acceptance Criteria:**

- [ ] In auto mode, all tools execute without interruption (current behavior)
- [ ] In ask-on-write, only destructive tools trigger authorization
- [ ] In ask-for-all, every tool triggers authorization
- [ ] When executeTools returns early for auth, handleStreamEvent detects this and switches to ModeAuthorize

**Verify:**

```bash
go build ./...
```

### 3.3. Implement authorization response and batch cancellation

**What:** In handleStreamEvent, detect when executeTools returned nil (authorization pending). Display authorization prompt. On user response (y/n with optional instruction): 1) y without text: execute the tool normally, continue loop for next tool. 2) y with text: execute the tool, inject user message (synthetic role) into session, mark remaining tools as CANCELLED, resume stream. 3) n without text: set tool result to error 'REJECTED by user', mark remaining tools as CANCELLED, resume stream. 4) n with text: set tool result to 'REJECTED by user', inject user message, mark remaining tools as CANCELLED, resume stream. The injected user message breaks the assistant turn — the LLM continues from that point.

**Why:** The user needs to accept/reject with optional instructions that feed back into the conversation. Rejected/cancelled tools get clear error messages so the LLM knows what happened.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) handleAuthResponse(accepted bool, instruction string) []config.ToolCallEntry {
	// Re-execute from authPendingIdx with known outcome
	entries := make([]config.ToolCallEntry, len(m.stream.partialTools))
	tracker := tools.NewFileTracker()
	// Execute tools up to authPendingIdx (already done in previous call)
	// ... copy existing entries ...
	// Process the pending tool
	if accepted {
		result := tool.Execute(args)
		// populate entry
	} else {
		entry.Execution.Status = tools.ResultStatusError
		entry.Execution.Error = "REJECTED by user"
	}
	// If instruction text exists, append synthetic user message
	if instruction != "" {
		m.session.appendMsg(config.Message{...Role: RoleSynthetic, Text: instruction, Label: "user instruction"})
	}
	// Cancel remaining tools
	for j := authPendingIdx + 1; j < len(partialTools); j++ {
		entries[j].Execution.Status = tools.ResultStatusError
		entries[j].Execution.Error = "CANCELLED due to user interruption"
	}
	return entries
}
```

**Acceptance Criteria:**

- [ ] Accepted tool executes normally, next tool is considered
- [ ] Accepted tool with instruction: tool executes, instruction injected, remaining tools cancelled, stream resumes with user message in context
- [ ] Rejected tool: result is error, remaining tools cancelled, stream resumes
- [ ] Rejected with instruction: error result + user message injected, remaining tools cancelled, stream resumes

**Verify:**

```bash
go build ./...
```

---

## 4. Interface

- **Pattern:** Presentation / Overlay

**Objective:** Authorization prompt UI: yes/no selection with arrow keys, tab for inline text, shift+tab to return

**Success Criteria:** User can see the tool prompt, press left/right to select yes/no, tab to type instructions, shift+tab to return, enter to submit

```mermaid
ModeAuthorize → StatusBar: [tool(args)]?  ←[Yes] [No]→  → Tab: text input ← → Shift+Tab: back to selection → Enter: submit
```

### 4.1. Add authorization prompt key handling

**What:** Add authorization key bindings and handler in keymap.go and input.go. Keys: left arrow (select no), right arrow (select yes), tab (enter instruction mode), shift+tab (return to y/n selection), enter (submit). In ModeAuthorize, if authInInstruction is true, handle text input. Otherwise, handle selection.

**Why:** The authorization prompt needs its own input handling separate from chat and streaming modes.

**Files:**

- ~ internal/app/keymap.go
- ~ internal/app/input.go

**Snippet:**

```
keyMap additions:
	AuthYes        key.Binding  // right arrow
	AuthNo         key.Binding  // left arrow
	AuthAccept     key.Binding  // enter
	AuthInstruction key.Binding // tab
```

```
handleAuthorizeKey(msg tea.KeyMsg) in input.go:
	switch {
	case key.Matches(msg, keys.AuthYes):
		return m.submitAuthResponse(true)
	case key.Matches(msg, keys.AuthNo):
		return m.submitAuthResponse(false)
	case key.Matches(msg, keys.Tab) && !m.stream.authInInstruction:
		m.stream.authInInstruction = true
		m.recalcLayout()
		return m, nil
	case msg.Shift && key.Matches(msg, keys.Tab) && m.stream.authInInstruction:
		m.stream.authInInstruction = false
		return m, nil
	case key.Matches(msg, keys.Send):
		return m.submitAuthResponse(m.stream.authInstructionText != "")
	}
```

**Acceptance Criteria:**

- [ ] Left/right arrows toggle selection
- [ ] Tab switches to text input
- [ ] Shift+tab switches back to y/n
- [ ] Enter submits with current selection + any text

**Verify:**

```bash
go build ./...
```

### 4.2. Build authorization prompt UI overlay

**What:** Build the authorization prompt component in the footer/status bar area. Display: the tool name and display args, yes/no indicators with current selection highlighted, and optionally a text input line. When authInInstruction is true, show a small textarea for typing instructions. Render via the existing footer or a dedicated overlay in render.go.

**Why:** Users need to see what tool is being authorized, what it will do, and have an intuitive way to accept/reject with optional notes.

**Files:**

- ~ internal/app/render.go
- ~ internal/ui/footer.go

**Snippet:**

```
renderAuthorizePrompt(m Model) string:
	tool := m.toolReg.Get(p.name)
	display := tool.DisplayValue(p.args)
	label := fmt.Sprintf("⚠ Proceed with %s(%s)?", p.name, display)
	selection := "[Yes] [No]"  // or "[Yes] [No]" depending on state
	if m.stream.authInInstruction {
		label += "
" + m.stream.authInstructionText + "_"
	}
	return label
```

**Acceptance Criteria:**

- [ ] Prompt shows tool name and display param
- [ ] Yes/No selection is visually clear
- [ ] Text instruction mode shows a cursor for input

**Verify:**

```bash
go build ./...
```

```bash
visual inspection in TUI
```

### 4.3. Update system prompt for destructive requirement

**What:** Update the system prompt to instruct the LLM that the bash tool requires the 'destructive' boolean field. Explain that true means the command modifies files/state, false means read-only. Emphasize this is mandatory — omitting it causes an error.

**Why:** The LLM needs to know about the new required field so it includes it in every bash call.

**Files:**

- ~ internal/config/sys-prompt.go

**Snippet:**

```
System prompt addition:
"When calling the bash tool, you MUST set the 'destructive' field:
- destructive: true if the command modifies files, writes, deletes, installs, or changes system state
- destructive: false for read-only commands (cat, ls, find, grep, git status, etc.)
Omitting this field will cause the tool call to fail."
```

**Acceptance Criteria:**

- [ ] System prompt mentions the destructive requirement

**Verify:**

```bash
go build ./...
```

### 4.4. Wire authorization pause/resume in handleStreamEvent

**What:** In handleStreamEvent, after executeTools is called, detect if it returned nil (authorization pending). If so: switch to ModeAuthorize, render the prompt, and wait for user input. Do NOT call startStream yet. On resume from ModeAuthorize, complete the tool batch (with accepted/rejected/cancelled results), append the assistant message, and call startStream to resume.

**Why:** handleStreamEvent currently assumes executeTools always returns a complete batch. We need to handle the pause-and-resume flow: partial execution → auth prompt → completion.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
In handleStreamEvent, tool_calls branch:
	toolEntries := (&m).executeTools(m.stream.partialTools)
	if toolEntries == nil {
		// Authorization pending — switch mode and wait
		m.mode = ModeAuthorize
		m.updateViewportContent()
		return m, nil
	}
	// Normal flow: append and resume...
```

**Acceptance Criteria:**

- [ ] When executeTools returns nil, mode switches to ModeAuthorize
- [ ] Viewport updates to show authorization prompt
- [ ] No stream resumption until auth is resolved

**Verify:**

```bash
go build ./...
```

### 4.5. Implement instruction text editing in authorization prompt

**What:** When in instruction mode (after Tab), capture typed characters into authInstructionText. Support backspace, left/right navigation. Show the accumulating text in the status bar alongside the yes/no selection. When Shift+Tab is pressed, preserve the text and return to yes/no view so the user can review before submitting.

**Why:** Users need to write clear instructions (e.g., 'don't delete the config file') alongside their yes/no decision. The back-and-forth between text editing and review should be fluid.

**Files:**

- ~ internal/app/input.go

**Snippet:**

```
In handleAuthorizeKey, when authInInstruction:
	case msg.Type == tea.KeyRunes:
		m.stream.authInstructionText += string(msg.Runes)
	case msg.Type == tea.KeyBackspace:
		runes := []rune(m.stream.authInstructionText)
		if len(runes) > 0 {
			m.stream.authInstructionText = string(runes[:len(runes)-1])
		}
	return m, nil
```

```
Footer rendering shows:
	- Selection: [Yes] [No]
	- Instruction text: "user typed text here|" (with cursor)
```

**Acceptance Criteria:**

- [ ] Typing characters appends to instruction
- [ ] Backspace removes last character
- [ ] Shift+tab preserves text, returns to selection view
- [ ] Enter in instruction mode submits with current selection + text

**Verify:**

```bash
go build ./...
```

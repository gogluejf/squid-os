# Tool Authorization

## Core Problem

Users need granular control over when tool executions require confirmation. The LLM currently executes all tools blindly, which can lead to unintended file modifications or destructive commands.

## Goal

Users can choose between auto-execute, ask-on-write, or ask-for-all modes. The LLM must declare destructive intent on bash calls. Pending tool calls are cancelled when the user adds instructions mid-execution.

---

## 1. Authorization Setting

- **Pattern:** Configuration Flag

**Objective:** Add a user-configurable setting that determines which tools require confirmation before execution.

**Success Criteria:** Settings exposes an Authorization field with three valid values. Invalid values fall back to auto.

```mermaid
Settings.json → Authorization field → parsed at startup → drives authorization gate behavior per tool call
```

### 1.1. Add Authorization field to Settings

**What:** Add Authorization string field to Settings struct in internal/config/settings.go with default value "auto".

**Why:** Stores the user's preference for tool authorization mode: auto, ask-on-write, or ask-for-all.

**Files:**

- ~ internal/config/settings.go

**Snippet:**

```
type Settings struct {
	// ... existing fields ...
	Authorization string \u0060json:"authorization"\u0060 // auto | ask-on-write | ask-for-all
}

func DefaultSettings() Settings {
	return Settings{
		// ... existing defaults ...
		Authorization: "auto",
	}
}

// ValidateAuthorization returns the normalized authorization mode.
func (s Settings) ValidateAuthorization() string {
	switch s.Authorization {
	case "auto", "ask-on-write", "ask-for-all":
		return s.Authorization
	default:
		return "auto"
	}
}
```

**Acceptance Criteria:**

- [ ] Settings struct compiles, defaults to "auto", rejects unknown values by falling back to auto

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.2. Add Authorization enum constants

**What:** Add AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll string constants in settings.go.

**Why:** Avoid magic strings scattered across the codebase. Single source of truth for mode names.

**Files:**

- ~ internal/config/settings.go

**Snippet:**

```
const (
	AuthorizationAuto        = "auto"
	AuthorizationAskOnWrite  = "ask-on-write"
	AuthorizationAskForAll   = "ask-for-all"
)
```

**Acceptance Criteria:**

- [ ] Constants match the three valid values, used in ValidateAuthorization

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 2. Destructive Bash

- **Pattern:** Schema Extension

**Objective:** Add a required destructive boolean field to the bash tool schema so the LLM must declare whether a command modifies disk state.

**Success Criteria:** Bash tool schema requires destructive field, returns error if omitted. Tool tracks destructiveness on the result.

```mermaid
LLM → bash({"command": "rm -rf ...", "destructive": true}) → schema validates required field → Execute checks destructiveness → ToolResult carries Destructive flag
```

### 2.1. Add destructive to bash schema

**What:** Add destructive (boolean, required) to the bash tool JSON schema in internal/tools/bash.go.

**Why:** The LLM must explicitly declare whether a bash command modifies files or system state. No default — omitting it returns an error.

**Files:**

- ~ internal/tools/bash.go

**Snippet:**

```
"properties": {
	"command": {
		"type": "string",
		"description": "The shell command to execute"
	},
	"timeout": {
		"type": "number",
		"description": "Timeout in milliseconds (default 120000)"
	},
	"destructive": {
		"type": "boolean",
		"description": "Must be true if the command modifies files, deletes data, or changes system state. Must be false for read-only commands (cat, ls, grep, find, git status, etc.). This field is required."
	}
},
"required": ["command", "destructive"]
```

**Acceptance Criteria:**

- [ ] Schema is valid JSON, destructive is required, omitting it causes a validation error

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.2. Add Destructive field to ToolResult

**What:** Add Destructive bool to ToolResult struct in internal/tools/tools.go. Set it from args["destructive"] in the bash Execute function.

**Why:** The authorization gate needs to know at execution time whether a tool is destructive, before running it.

**Files:**

- ~ internal/tools/tools.go
- ~ internal/tools/bash.go

**Snippet:**

```
type ToolResult struct {
	Status    string             // ...
	Result    string             // ...
	Error     string             // ...
	Destructive bool             // true if this tool modifies disk state
	Files     []config.FileEntry // ...
}

// In bash Execute:
destructive, ok := args["destructive"].(bool)
if !ok {
	return ToolResult{Status: ResultStatusError, Error: "destructive is required and must be a boolean"}
}
// ... after execution ...
return ToolResult{Status: ResultStatusSuccess, Result: result.String(), Destructive: destructive}
```

**Acceptance Criteria:**

- [ ] Destructive field populated on bash results, error returned when omitted

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.3. Mark inherently destructive tools

**What:** Add IsDestructive func()bool to the Tool struct. Set it true for write_file, edit_file, skill_build. Bash uses the runtime destructive arg.

**Why:** The authorization gate needs a pre-execution way to determine if a tool is destructive, without running it first. For file tools, destructiveness is static. For bash, it comes from the args.

**Files:**

- ~ internal/tools/tools.go

**Snippet:**

```
type Tool struct {
	// ... existing fields ...
	IsDestructive func(args map[string]interface{}) bool // nil = never destructive
}

var WriteFile = Tool{
	// ...
	IsDestructive: func(args map[string]interface{}) bool { return true },
}

var EditFile = Tool{
	// ...
	IsDestructive: func(args map[string]interface{}) bool { return true },
}

var SkillBuild = Tool{
	// ...
	IsDestructive: func(args map[string]interface{}) bool { return true },
}

var Bash = Tool{
	// ...
	IsDestructive: func(args map[string]interface{}) bool {
		d, ok := args["destructive"].(bool)
		return ok && d
	},
}
```

**Acceptance Criteria:**

- [ ] write_file, edit_file, skill_build always report destructive. bash reports based on args. read_file, skill_load, skill_list, set_working_dir, open report false

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 3. Authorization Gate

- **Pattern:** Guard Clause / Interrupt

**Objective:** Intercept tool execution in the stream loop. When authorization mode requires it, pause and ask the user before executing. On rejection, cancel pending tool calls.

**Success Criteria:** In ask-on-write mode, destructive tools pause for confirmation, read-only tools auto-execute. In ask-for-all, every tool pauses. Rejected or instruction-annotated tools cancel remaining pending calls and resume stream.

```mermaid
Stream → tool_calls stop → iterate partialTools → check authorization needed? → yes: enter ModeAuthorize → user responds (y/n + optional instructions) → execute tool or reject → if instructions injected: cancel remaining tools, resume stream immediately. else: continue to next tool
```

### 3.1. Add authorization types and constants

**What:** Create internal/app/authorization.go with AuthorizationResult enum (Accepted, Rejected, AcceptedWithInstructions) and AuthorizationContext struct holding the pending tool, args, user instructions, and result.

**Why:** Encapsulates the authorization state machine: what tool is pending, what the user responded, and whether extra instructions were provided.

**Files:**

- + internal/app/authorization.go

**Snippet:**

```
package app

type AuthResult int
const (
	AuthAccepted           AuthResult = iota // plain yes
	AuthRejected                            // plain no
	AuthAcceptedWithInstructions            // yes + instructions
	AuthRejectedWithInstructions            // no + instructions
)

type AuthorizationContext struct {
	ToolName        string                  // tool being authorized
	Args            map[string]interface{}  // parsed args for display
	ArgsJSON        string                  // raw args for display
	DisplayValue    string                  // from tool.DisplayParam
	Result          AuthResult
	UserInstructions string                // optional text the user attached
	IsDestructive   bool
}

func (c *AuthorizationContext) IsActionable() bool {
	return c.Result != 0
}
```

**Acceptance Criteria:**

- [ ] Struct and types compile, provides all fields needed by the UI and execution logic

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.2. Add authorization mode to streamState

**What:** Add authorizationContext field to streamState in stream.go. Add pendingToolIndex int to track which partialTool is being authorized. Add needsAuthorization helper method on Model.

**Why:** The stream loop needs to know when it's paused waiting for user authorization and which tool is pending.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In streamState struct:
	authorizationCtx *AuthorizationContext // non-nil when paused awaiting auth
	pendingToolIndex int                   // index into partialTools being authorized

// In Model:
func (m Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool {
	authMode := m.settings.ValidateAuthorization()
	switch authMode {
	case config.AuthorizationAskForAll:
		return true
	case config.AuthorizationAskOnWrite:
		if tool != nil && tool.IsDestructive != nil {
			return tool.IsDestructive(args)
		}
		return false
	default: // auto
		return false
	}
}
```

**Acceptance Criteria:**

- [ ] needsAuthorization returns true for all tools in ask-for-all, only destructive in ask-on-write, nothing in auto

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.3. Refactor executeTools to support authorization interruption

**What:** Rewrite executeTools in stream.go to check authorization before each tool execution. If authorization is needed, return a single AuthorizationContext and the caller (handleStreamEvent) pauses the stream. Remaining tools are left unexecuted until the user responds.

**Why:** Currently executeTools runs all tools in a batch. With authorization, we must process one at a time and potentially pause for user input, canceling remaining tools on rejection or instruction injection.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) executeTools(partials []partialTool) []config.ToolCallEntry {
	// Phase 1: Determine if any tool needs authorization
	for i, p := range partials {
		tool := m.toolReg.Get(p.name)
		var args map[string]interface{}
		if p.args != "" {
			_ = json.Unmarshal([]byte(p.args), &args)
		}
		if m.needsAuthorization(tool, args) {
			// Pause here — set up authorization context
			isDestructive := false
			if tool != nil && tool.IsDestructive != nil {
				isDestructive = tool.IsDestructive(args)
			}
			m.stream.authorizationCtx = &AuthorizationContext{
				ToolName:       p.name,
				Args:           args,
				ArgsJSON:       p.args,
				DisplayValue:   tool.DisplayValue(p.args),
				IsDestructive:  isDestructive,
			}
			m.stream.pendingToolIndex = i
			return nil // nil signals caller to enter authorization mode
		}
	}
	// Phase 2: No authorization needed — execute all (existing logic)
	return m.executeToolsUnauthenticated(partials)
}

func (m *Model) executeToolsUnauthenticated(partials []partialTool) []config.ToolCallEntry {
	// ... existing executeTools body (renamed) ...
}
```

**Acceptance Criteria:**

- [ ] When no authorization is needed, behavior is identical to before. When authorization is needed, returns nil and sets authorizationCtx

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.4. Handle authorization response in stream event loop

**What:** In handleStreamEvent, after executeTools returns nil (authorization needed), transition to ModeAuthorize. After user responds: if Accepted → execute that single tool, check next tool. If Rejected or WithInstructions → mark remaining tools as cancelled, build entries with appropriate error/result, resume stream. If instructions attached → inject synthetic user message before resuming.

**Why:** This is the core state transition: stream pauses → user decides → stream resumes with modified context.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In handleStreamEvent, after tool_calls stop reason:
toolEntries := (&m).executeTools(m.stream.partialTools)
if toolEntries == nil {
	// Authorization needed — pause the stream in ModeAuthorize
	(&m).setAuthMode()
	m.updateViewportContent()
	return m, nil // no new command — waiting for user key
}

// setAuthMode switches to authorization mode
func (m *Model) setAuthMode() {
	m.mode = ModeAuthorize
	m.stream.active = false // stop waiting for stream events
	m.textarea.Placeholder = "press y/n to proceed, tab for instructions..."
}

// handleAuthorizationResponse executes after user confirms/rejects
func (m *Model) handleAuthorizationResponse(result AuthResult, instructions string) ([]config.ToolCallEntry, bool) {
	ctx := m.stream.authorizationCtx
	idx := m.stream.pendingToolIndex
	partials := m.stream.partialTools
	entries := make([]config.ToolCallEntry, len(partials))

	if result == AuthAccepted || result == AuthAcceptedWithInstructions {
		// Execute the pending tool
		// ... (single tool execution logic) ...
		entries[idx] = executedEntry

		// If instructions were given, cancel remaining tools and signal resume-with-injection
		if result == AuthAcceptedWithInstructions {
			for j := idx + 1; j < len(partials); j++ {
				entries[j] = config.ToolCallEntry{
					ID: partials[j].id,
					Instruction: struct{...}{Name: partials[j].name, Arguments: partials[j].args},
					Execution: struct{...}{
						Status: ResultStatusError,
						Error:  "cancelled: user provided instructions before this tool could execute",
					},
				}
			}
			return entries, true // true = inject user message
		}
		// Continue to next tool (check if it also needs auth)
		// ... recursive or iterative check ...
	} else {
		// Rejected
		entries[idx] = config.ToolCallEntry{
			// ... with error "rejected by user" ...
		}
		// Cancel remaining
		for j := idx + 1; j < len(partials); j++ {
			// ... cancelled ...
		}
		return entries, result == AuthRejectedWithInstructions
	}
}
```

**Acceptance Criteria:**

- [ ] Accepted tools execute, rejected tools return error, instructions trigger synthetic message injection, remaining tools are cancelled

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.5. Inject synthetic user message on instruction-attached authorization

**What:** When authorization resolves with instructions, append a synthetic user message to the session containing the user's instructions, then resume streaming via startStream().

**Why:** Breaks the assistant turn with user context, allowing the LLM to adjust its behavior before continuing.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) injectAndResume(instructions string) (tea.Model, tea.Cmd) {
	// Append user message with instructions
	userMsg := config.Message{
		ID:        fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:      config.RoleUser,
		CreatedAt: time.Now(),
		Text:      instructions,
		InputTokens: countTokensApprox(instructions),
	}
	m.session.appendMsg(userMsg)

	// Reset authorization state
	m.stream.authorizationCtx = nil
	m.stream.pendingToolIndex = -1

	// Resume stream
	return (&m).startStream()
}
```

**Acceptance Criteria:**

- [ ] User message appears in session, stream resumes with the new message in context

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 4. Authorization UI

- **Pattern:** Overlay Mode / State Machine

**Objective:** Build the authorization prompt that appears in the status bar area: yes/no selection with arrow keys, tab to switch to instruction input, shift+tab to return to yes/no, enter to submit.

**Success Criteria:** The authorization overlay renders in the status line area. User can navigate between yes/no, switch to text input, and submit. All keybindings work correctly and transition back to streaming or chat mode.

```mermaid
ModeAuthorize → render overlay (status line): [←] Yes / No [→] ←→ arrows, tab for text, enter to submit → user key event → if text mode: textarea input → if yes/no mode: arrow selection → enter: resolve authorization → resume stream or chat
```

### 4.1. Add ModeAuthorize to modes

**What:** Add ModeAuthorize to the Mode enum in internal/app/modes.go and its String() method.

**Why:** Needed for the routing in Update/View to handle the authorization overlay as a distinct mode.

**Files:**

- ~ internal/app/modes.go

**Snippet:**

```
const (
	ModeChat          Mode = iota
	ModeStreaming
	ModeModelPicker
	ModeHelp
	ModeFilePicker
	ModeSessionPicker
	ModeSavePrompt
	ModeHistorySearch
	ModeAuthorize   // awaiting user authorization for tool execution
)

func (m Mode) String() string {
	switch m {
	// ...
	case ModeAuthorize:
		return "authorize"
	// ...
	}
```

**Acceptance Criteria:**

- [ ] ModeAuthorize compiles, String() returns "authorize"

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.2. Build authorization prompt component

**What:** Create internal/ui/authorization.go with an AuthorizationPrompt struct that renders the yes/no prompt in the status bar area. Tracks selection (yes=0, no=1) and text mode (false=yes/no, true=text input).

**Why:** Encapsulates the authorization UI: the prompt line showing tool name + args, the yes/no selection, and the text input for instructions.

**Files:**

- + internal/ui/authorization.go

**Snippet:**

```
package ui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"squid-os/internal/style"
)

type AuthorizationPrompt struct {
	ToolName       string
	ArgsJSON       string
	DisplayValue   string
	IsDestructive  bool
	Selection      int // 0=yes, 1=no
	TextMode       bool // true = typing instructions
	TextInput      string
	Width          int
}

func (p AuthorizationPrompt) Render() string {
	width := p.Width
	if width == 0 {
		width = 80
	}

	// Build prompt line
	label := p.ToolName
	if p.DisplayValue != "" {
		label = fmt.Sprintf("%s(%s)", p.ToolName, truncate(p.DisplayValue, 40))
	}

	var destructiveIcon string
	if p.IsDestructive {
		destructiveIcon = "⚠ "
	}

	var prompt string
	if p.TextMode {
		prompt = fmt.Sprintf("%sProceed with %s? [%s] Type instructions...", 
			destructiveIcon, label, 
			p.selectedLabel())
	} else {
		prompt = fmt.Sprintf("%sProceed with %s? [%s]es / [N]o / [T]ab for instructions",
			destructiveIcon, label, p.selectedLabel())
	}

	// Style: highlight selected option
	yesStyle := style.SelectionStyle.Render("Yes")
	noStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No")
	if p.Selection == 0 {
		yesStyle = style.SelectionStyle.Render("Yes")
	} else {
		noStyle = style.SelectionStyle.Render("No")
	}

	return style.StatusLineStyle.Width(width).Render(fmt.Sprintf(
		"  %s  %s / %s  %s",
		label,
		yesStyle,
		noStyle,
		promptSuffix(p.TextMode),
	))
}

func (p AuthorizationPrompt) selectedLabel() string {
	if p.Selection == 0 {
		return "Y"
	}
	return "N"
}

func (p AuthorizationPrompt) promptSuffix(textMode bool) string {
	if textMode {
		return "← enter to submit, shift+tab to review"
	}
	return "←→ select · tab for instructions · enter to confirm"
}
```

**Acceptance Criteria:**

- [ ] Component renders correctly, shows tool name, yes/no selection, and instruction hint

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.3. Add authorization prompt field to Model

**What:** Add authPrompt ui.AuthorizationPrompt to the Model struct in app.go. Populate it from authorizationCtx when entering ModeAuthorize.

**Why:** The Model needs to hold the authorization prompt state so it can render and handle input.

**Files:**

- ~ internal/app/app.go

**Snippet:**

```
// In Model struct:
	authPrompt ui.AuthorizationPrompt // authorization prompt state

// In setAuthMode():
func (m *Model) setAuthMode() {
	m.mode = ModeAuthorize
	m.stream.active = false
	ctx := m.stream.authorizationCtx
	m.authPrompt = ui.AuthorizationPrompt{
		ToolName:      ctx.ToolName,
		ArgsJSON:      ctx.ArgsJSON,
		DisplayValue:  ctx.DisplayValue,
		IsDestructive: ctx.IsDestructive,
		Selection:     0, // default to Yes
		Width:         m.width,
	}
}
```

**Acceptance Criteria:**

- [ ] authPrompt is populated from authorizationCtx, setAuthMode initializes it

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.4. Render authorization overlay in View

**What:** In render.go View(), add a case for ModeAuthorize that renders authPrompt instead of the textarea, replacing the status line area.

**Why:** The authorization prompt replaces the input area during authorization, making it the primary interaction point.

**Files:**

- ~ internal/app/render.go

**Snippet:**

```
// In View(), after the mode switch for overlays:
case ModeAuthorize:
	sections = append(sections, m.authPrompt.Render())

// Skip textarea in auth mode
if m.mode != ModeAuthorize {
	sections = append(sections, m.textarea.View())
}
```

**Acceptance Criteria:**

- [ ] In ModeAuthorize, the authorization prompt replaces the textarea in the layout

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.5. Handle authorization key input

**What:** Add handleAuthorizeKey to input.go: Left/Right arrows toggle Yes/No, Tab switches to text mode, Shift+Tab returns to yes/no, Enter submits. In text mode: normal text input, Enter submits with text, Shift+Tab returns to yes/no.

**Why:** Routes authorization key events to the correct action: selecting yes/no, switching to text, or submitting.

**Files:**

- ~ internal/app/input.go

**Snippet:**

```
func (m Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var result app.AuthResult

	switch {
	case key.Matches(msg, keys.Left):
		if !m.authPrompt.TextMode {
			m.authPrompt.Selection = 0 // Yes
			m.updateViewportContent()
			return m, nil
		}
	case key.Matches(msg, keys.Right):
		if !m.authPrompt.TextMode {
			m.authPrompt.Selection = 1 // No
			m.updateViewportContent()
			return m, nil
		}

	case msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):
		// Tab → switch to text mode
		m.authPrompt.TextMode = true
		return m, nil

	case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):
		// Shift+Tab → return to yes/no mode
		m.authPrompt.TextMode = false
		return m, nil

	case key.Matches(msg, keys.Send):
		if m.authPrompt.TextMode {
			// Submit with text
			text := m.authPrompt.TextInput
			if m.authPrompt.Selection == 0 {
				result = app.AuthAcceptedWithInstructions
			} else {
				result = app.AuthRejectedWithInstructions
			}
			return m.resolveAuthorization(result, text)
		} else {
			// Submit yes/no without text
			if m.authPrompt.Selection == 0 {
				result = app.AuthAccepted
			} else {
				result = app.AuthRejected
			}
			return m.resolveAuthorization(result, "")
		}

	default:
		if m.authPrompt.TextMode && msg.Type == tea.KeyRunes {
			m.authPrompt.TextInput += string(msg.Runes)
			return m, nil
		} else if m.authPrompt.TextMode && msg.Type == tea.KeyBackspace {
			text := m.authPrompt.TextInput
			if len(text) > 0 {
				runes := []rune(text)
				m.authPrompt.TextInput = string(runes[:len(runes)-1])
			}
			return m, nil
		}
	}
	return m, nil
}
```

**Acceptance Criteria:**

- [ ] Left/Right toggles selection, Tab enters text mode, Shift+Tab exits text mode, Enter resolves. Text input works in text mode.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.6. Wire authorization mode in Update dispatch

**What:** In update.go handleKey, add case for ModeAuthorize → handleAuthorizeKey. In handleStreamEvent, when executeTools returns nil, transition to ModeAuthorize.

**Why:** Wires the authorization mode into the main event loop so it can intercept and respond to key events.

**Files:**

- ~ internal/app/update.go

**Snippet:**

```
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	// ...
	case ModeAuthorize:
		return m.handleAuthorizeKey(msg)
	// ...
	}
```

**Acceptance Criteria:**

- [ ] Authorization mode is dispatched in handleKey, key events reach handleAuthorizeKey

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 5. System Prompt Update

- **Pattern:** Instruction Injection

**Objective:** Update the system prompt so the LLM knows it must set the destructive field on every bash call, and understands the semantics of what makes a command destructive.

**Success Criteria:** System prompt clearly defines destructive vs read-only bash commands. LLM consistently sets the correct value.

```mermaid
sys-prompt.go → DefaultAssistantPrompt() includes new bash destructive field instructions → injected into every API request as system message
```

### 5.1. Update system prompt for destructive field

**What:** Add a new section to the default system prompt in internal/config/sys-prompt.go explaining the destructive field for bash: when to set true, when to set false, and that the field is required.

**Why:** The LLM needs explicit instructions to populate the required destructive field correctly on every bash call.

**Files:**

- ~ internal/config/sys-prompt.go

**Snippet:**

```
## Bash Tool
- The bash tool requires a "destructive" boolean parameter on every call.
- Set destructive: true if the command modifies files, deletes data, creates directories, installs packages, or changes system state (e.g., rm, mv, cp, mkdir, chmod, apt-get, pip install, git commit, sed -i).
- Set destructive: false for read-only commands (e.g., cat, ls, grep, find, git status, git diff, wc, head, tail, df, ps, curl GET).
- Omitting the destructive field will result in an error.
```

**Acceptance Criteria:**

- [ ] System prompt contains clear destructive field instructions for bash, examples of true and false cases

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 6. Resolve Authorization Response

- **Pattern:** State Machine / Post-Action Routing

**Objective:** After the user responds to an authorization prompt, execute or reject the tool, handle remaining pending calls, and route back to either continued tool authorization or stream resume.

**Success Criteria:** resolveAuthorization handles all four result types (Accepted, Rejected, AcceptedWithInstructions, RejectedWithInstructions). Pending tools are cancelled when instructions are injected. Stream resumes correctly with or without injected user message.

```mermaid
handleAuthorizeKey Enter → resolveAuthorization(result, instructions) → if Accepted: execute tool → check next partial (needs auth? → ModeAuthorize again / no → continue) → if WithInstructions: cancel remaining, inject user msg, startStream → if Rejected: mark rejected, cancel remaining, resume stream
```

### 6.1. Implement resolveAuthorization

**What:** Add resolveAuthorization method to Model in stream.go. Handles the four auth results: execute+continue for Accepted, mark error for Rejected, execute+cancel+inject for AcceptedWithInstructions, mark error+cancel+inject for RejectedWithInstructions.

**Why:** This is the core post-authorization logic that determines what happens after the user responds — the branching point between continuing, cancelling, and injecting context.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) resolveAuthorization(result AuthResult, instructions string) (tea.Model, tea.Cmd) {
	ctx := m.stream.authorizationCtx
	idx := m.stream.pendingToolIndex
	partials := m.stream.partialTools
	entries := make([]config.ToolCallEntry, len(partials))

	for i, p := range partials {
		entries[i] = config.ToolCallEntry{
			ID: p.id, Type: p.typeStr,
			Instruction: struct{...}{Name: p.name, Arguments: p.args, Tokens: countTokensApprox(p.args)},
		}
	}

	switch result {
	case AuthAccepted:
		entries[idx] = m.executeSingleTool(partials[idx])
		// Save assistant message with this entry, then check next tools
		return m.continueAfterAuth(entries, false, "")

	case AuthRejected:
		entries[idx].Execution.Status = tools.ResultStatusError
		entries[idx].Execution.Error = "rejected by user — tool was not executed"
		// Cancel remaining
		for j := idx + 1; j < len(partials); j++ {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: previous tool was rejected by user"
		}
		return m.continueAfterAuth(entries, false, "")

	case AuthAcceptedWithInstructions:
		entries[idx] = m.executeSingleTool(partials[idx])
		for j := idx + 1; j < len(partials); j++ {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: user provided instructions before this tool could execute"
		}
		return m.continueAfterAuth(entries, true, instructions)

	case AuthRejectedWithInstructions:
		entries[idx].Execution.Status = tools.ResultStatusError
		entries[idx].Execution.Error = "rejected by user — tool was not executed"
		for j := idx + 1; j < len(partials); j++ {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: user provided instructions"
		}
		return m.continueAfterAuth(entries, true, instructions)
	}
	return m, nil
}
```

**Acceptance Criteria:**

- [ ] All four auth results are handled correctly. Single tool executes on accept, error on reject. Remaining tools cancelled on instructions. Stream resumes or user message injected.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 6.2. Implement executeSingleTool and continueAfterAuth

**What:** Add executeSingleTool (extracted from old executeTools batch logic, for one partial) and continueAfterAuth (saves the assistant message with all entries, injects user message if needed, and either resumes stream or checks next tool for authorization).

**Why:** Factored from executeTools so a single tool can be executed outside the batch, and the post-authorization flow can decide whether to resume streaming or continue checking more tools.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) executeSingleTool(p partialTool) config.ToolCallEntry {
	tool := m.toolReg.Get(p.name)
	var args map[string]interface{}
	if p.args != "" {
		_ = json.Unmarshal([]byte(p.args), &args)
	}
	// ... checksum validation from tracker ...
	resultStart := time.Now()
	result := tool.Execute(args)
	entry := config.ToolCallEntry{
		ID: p.id, Type: p.typeStr,
		Instruction: struct{...}{Name: p.name, Arguments: p.args, Tokens: countTokensApprox(p.args)},
		Execution: struct{...}{
			Status: result.Status, Result: result.Result, Error: result.Error,
			Tokens: countTokensApprox(result.Result),
			DurationMs: time.Since(resultStart).Milliseconds(),
			Files: result.Files,
		},
	}
	// Handle set_working_dir side effect
	if p.name == "set_working_dir" && result.Status == tools.ResultStatusSuccess {
		if pathVal, ok := args["path"].(string); ok {
			m.applyWorkingDir(pathVal)
		}
	}
	return entry
}

func (m *Model) continueAfterAuth(entries []config.ToolCallEntry, injectUserMsg bool, instructions string) (tea.Model, tea.Cmd) {
	// Check if there are more tools that need authorization
	if !injectUserMsg {
		// Check remaining tools after the one we just handled
		nextIdx := m.stream.pendingToolIndex + 1
		if nextIdx < len(m.stream.partialTools) {
			remaining := m.stream.partialTools[nextIdx:]
			// Check if any remaining unexecuted tool needs auth
			// ... if yes, set authorizationCtx for next, stay in ModeAuthorize
			// ... if no, execute remaining + merge with entries, save assistant msg, resume stream
		}
	}

	// Save assistant message with all tool entries
	(&m).appendAssistantMsg(config.Message{
		ID: fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role: config.RoleAssistant,
		// ... metrics from stream ...
		ToolCalls: entries,
		StopReason: "tool_calls",
	})

	m.stream.authorizationCtx = nil
	m.stream.pendingToolIndex = -1

	if injectUserMsg && instructions != "" {
		return m.injectAndResume(instructions)
	}
	return (&m).startStream()
}
```

**Acceptance Criteria:**

- [ ] Single tool execution mirrors batch behavior. continueAfterAuth saves message, handles injection, and resumes stream. Remaining tools are checked for authorization.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

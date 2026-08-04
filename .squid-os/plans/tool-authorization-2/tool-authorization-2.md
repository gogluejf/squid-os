# Tool Authorization

## Core Problem

Users need granular control over when tool executions require confirmation. The LLM currently executes all tools blindly, which can lead to unintended file modifications or destructive commands.

## Goal

Users can choose between auto-execute, ask-on-write, or ask-for-all modes. Destructive bash calls require an explicit 'destructive' arg from the LLM. File tools leverage their existing Preview for safe authorization prompts. Pending tool calls are cancelled when the user adds instructions mid-execution, allowing the LLM to re-plan.

---

## 1. Authorization Setting

- **Pattern:** Configuration Flag

**Objective:** Add a user-configurable setting that determines which tools require confirmation before execution.

**Success Criteria:** Settings exposes an Authorization field with three valid values (auto, ask-on-write, ask-for-all). Invalid values fall back to auto.

```mermaid
Settings.json → Authorization field → parsed at startup → drives authorization gate behavior per tool call in executeTools
```

### 1.1. Add Authorization field and constants to Settings

**What:** Add Authorization string field and validation constants to Settings struct in internal/config/settings.go with default value 'auto'.

**Why:** Stores the user's preference for tool authorization mode: auto, ask-on-write, or ask-for-all.

**Files:**

- ~ internal/config/settings.go

**Snippet:**

```
const (
	AuthorizationAuto        = "auto"
	AuthorizationAskOnWrite  = "ask-on-write"
	AuthorizationAskForAll   = "ask-for-all"
)

type Settings struct {
	// ... existing fields ...
	Authorization string `json:"authorization"` // auto | ask-on-write | ask-for-all
}

func DefaultSettings() Settings {
	return Settings{
		// ... existing defaults ...
		Authorization: AuthorizationAuto,
	}
}

// ValidateAuthorization returns the normalized authorization mode, falling back to auto.
func (s Settings) ValidateAuthorization() string {
	switch s.Authorization {
	case AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll:
		return s.Authorization
	default:
		return AuthorizationAuto
	}
}
```

**Acceptance Criteria:**

- [ ] Settings struct compiles, defaults to 'auto', rejects unknown values by falling back to auto

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 2. Destructive Bash

- **Pattern:** Schema Extension

**Objective:** Add a required destructive boolean field to the bash tool schema so the LLM must declare whether a command modifies disk state.

**Success Criteria:** Bash tool schema requires destructive field — omitting it returns an error. The authorization gate reads destructive from args before execution.

```mermaid
LLM → bash({command, destructive: true/false}) → schema validates required field → authorization gate checks args destructiveness before Execute → tool runs or user decides
```

### 2.1. Add destructive to bash schema and validation

**What:** Add destructive (boolean, required) to the bash tool JSON schema in internal/tools/bash.go. In Execute, validate the arg and return error if missing or non-boolean.

**Why:** The LLM must explicitly declare whether a bash command modifies files or system state. No default — omitting it returns an error. This is the only destructiveness signal needed for bash.

**Files:**

- ~ internal/tools/bash.go

**Snippet:**

```
"properties": {
    "command": { "type": "string", ... },
    "timeout": { "type": "number", ... },
    "destructive": {
      "type": "boolean",
      "description": "Must be true if the command modifies files, deletes data, or changes system state (rm, mv, cp, mkdir, chmod, sed -i, apt-get, pip install, git commit). Must be false for read-only commands (cat, ls, grep, find, git status, git diff, wc, head, tail, df, ps, curl GET). This field is required."
    }
  },
  "required": ["command", "destructive"]

// In Execute:
destructive, ok := args["destructive"].(bool)
if !ok {
  return ToolResult{Status: ResultStatusError, Error: "destructive is required and must be a boolean"}
}
```

**Acceptance Criteria:**

- [ ] Schema is valid JSON, destructive is required, omitting it causes a validation error

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.2. Add IsDestructive helper to Tool for pre-execution checks

**What:** Add IsDestructive func(map[string]interface{}) bool to the Tool struct. Set it true for write_file, edit_file (always destructive). For bash, read destructive from args. All other tools return false (or have nil IsDestructive).

**Why:** The authorization gate needs to determine at pre-execution time whether a tool is destructive, without running it. For file tools destructiveness is static. For bash it comes from the args. This leverages the existing Preview on write_file/edit_file — they are always write operations.

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

var Bash = Tool{
	// ...
	IsDestructive: func(args map[string]interface{}) bool {
		d, ok := args["destructive"].(bool)
		return ok && d
	},
}

// ReadFile, Open, SkillLoad, SkillList, SetWorkingDirTool: IsDestructive is nil (never destructive)
```

**Acceptance Criteria:**

- [ ] write_file, edit_file always report destructive. bash reports based on args destructive field. read_file, skill_load, skill_list, set_working_dir, open report false

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 3. Authorization Gate

- **Pattern:** Guard Clause / Interrupt

**Objective:** Intercept tool execution inside the iterate-tools flow. When authorization mode requires it, pause and ask the user before executing. On instruction injection, cancel all remaining pending tools and resume stream with the user's context.

**Success Criteria:** In ask-on-write, destructive tools pause for confirmation and read-only tools auto-execute. In ask-for-all, every tool pauses. On rejection or instruction injection, remaining pending tool calls are cancelled and the stream resumes. The flow iterates tool by tool — not batching — when authorization is active.

```mermaid
handleStreamEvent tool_calls stop → iterate partialTools one at a time → needsAuthorization? → yes: enter ModeAuthorize and pause stream → user responds (y/n + optional text) → execute tool OR mark rejected → if instructions attached: cancel remaining tools, inject user message, startStream → if accepted without instructions: check next tool for auth, repeat
```

### 3.1. Add authorization types and authorization context

**What:** Create internal/app/authorization.go with AuthResult (bool approved + string instructions) and AuthorizationContext holding the pending tool name, args, display value, destructive flag, and result.

**Why:** Simplified auth result: just approved (bool) and instructions (string). Four variants collapse to two fields. The context holds everything needed for the UI prompt and execution decision.

**Files:**

- + internal/app/authorization.go

**Snippet:**

```
package app

type AuthResult struct {
	Approved      bool
	Instructions  string // empty = plain yes/no
}

func (r AuthResult) HasInstructions() bool { return r.Instructions != "" }

type AuthorizationContext struct {
	ToolName        string
	Args            map[string]interface{}
	ArgsJSON        string
	DisplayValue    string
	IsDestructive   bool
	Result          AuthResult
}

func (c *AuthorizationContext) IsActionable() bool {
	return c.Result.Approved || c.Result.Instructions != ""
}
```

**Acceptance Criteria:**

- [ ] Struct and types compile, approved + instructions replaces the four-variant enum

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.2. Add authorization state to stream and Model.needsAuthorization

**What:** Add authorizationCtx *AuthorizationContext and pendingToolIndex int to streamState. Add needsAuthorization method on Model that checks settings authorization mode and tool.IsDestructive.

**Why:** The stream loop needs to track when it's paused awaiting user authorization and which tool is pending. needsAuthorization centralizes the mode-checking logic.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In streamState:
	authorizationCtx *AuthorizationContext // non-nil when paused awaiting auth
	pendingToolIndex int                   // index into partialTools being authorized

func (m Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool {
	switch m.settings.ValidateAuthorization() {
	case config.AuthorizationAskForAll:
		return true
	case config.AuthorizationAskOnWrite:
		return tool != nil && tool.IsDestructive != nil && tool.IsDestructive(args)
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

### 3.3. Refactor executeTools to support authorization interruption per tool

**What:** Rewrite executeTools in stream.go to iterate tools one at a time. Before each tool's Execute, check needsAuthorization. If auth is needed, populate authorizationCtx with the pending tool info, set pendingToolIndex, and return nil (signals caller to enter authorization mode). If no auth needed, execute that tool and continue to the next. Only batch-execute the remaining tools if none of them need auth.

**Why:** Currently executeTools runs all tools in a batch. With authorization we must process one at a time and potentially pause. The key insight: iterate, check, interrupt — not pre-scan then execute. This keeps the flow natural and avoids duplicating the execution logic.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) executeTools(partials []partialTool) []config.ToolCallEntry {
	// Fast path: if auth mode is auto, run everything as before (existing logic)
	if m.settings.ValidateAuthorization() == config.AuthorizationAuto {
		return m.executeToolsBatch(partials)
	}

	// Iterative path: process tool by tool, may pause for authorization
	return m.executeToolsIterative(partials, 0, make([]config.ToolCallEntry, 0, len(partials)))
}

func (m *Model) executeToolsIterative(partials []partialTool, idx int, entries []config.ToolCallEntry) []config.ToolCallEntry {
	if idx >= len(partials) {
		return entries
	}
	p := partials[idx]
	tool := m.toolReg.Get(p.name)
	var args map[string]interface{}
	if p.args != "" {
		json.Unmarshal([]byte(p.args), &args)
	}

	if m.needsAuthorization(tool, args) {
		// Save already-executed entries, set up authorization context, return nil to pause
		m.stream.pendingToolIndex = idx
		isDestructive := false
		if tool != nil && tool.IsDestructive != nil {
			isDestructive = tool.IsDestructive(args)
		}
		m.stream.authorizationCtx = &AuthorizationContext{
			ToolName:      p.name,
			Args:          args,
			ArgsJSON:      p.args,
			DisplayValue:  tool.DisplayValue(p.args),
			IsDestructive: isDestructive,
		}
		return nil // signals handleStreamEvent to enter ModeAuthorize
	}

	// Execute this tool inline, append entry, continue to next
	entry := m.executeSingleTool(p)
	return m.executeToolsIterative(partials, idx+1, append(entries, entry))
}

func (m *Model) executeToolsBatch(partials []partialTool) []config.ToolCallEntry {
	// Existing executeTools body — unchanged for auto mode
}
```

**Acceptance Criteria:**

- [ ] In auto mode, behavior is identical to before. In ask modes, tools are processed one at a time and execution pauses when auth is needed, returning nil

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.4. Implement executeSingleTool and resolveAuthorization

**What:** Add executeSingleTool (extracted from existing executeTools per-tool logic) and resolveAuthorization that handles the user's response. On approval: execute the pending tool, then check remaining tools. On rejection: mark error. If instructions attached: cancel all remaining tools and signal stream resume with synthetic user message injection.

**Why:** executeSingleTool reuses the existing per-tool execution logic (validation, checksum check, side effects). resolveAuthorization is the post-user-response branching point — the core of the authorization flow.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) executeSingleTool(p partialTool) config.ToolCallEntry {
	// (Extracted from existing executeTools loop body — validation, execution, file state merge, set_working_dir side effect)
	// ...
}

func (m *Model) resolveAuthorization(approved bool, instructions string) (tea.Model, tea.Cmd) {
	ctx := m.stream.authorizationCtx
	idx := m.stream.pendingToolIndex
	partials := m.stream.partialTools
	entries := make([]config.ToolCallEntry, len(partials))

	for i, p := range partials {
		entries[i] = m.buildEmptyEntry(p)
	}

	if approved {
		entries[idx] = m.executeSingleTool(partials[idx])
	} else {
		entries[idx].Execution.Status = tools.ResultStatusError
		entries[idx].Execution.Error = "rejected by user — tool was not executed"
	}

	// If instructions were provided, cancel all remaining tools
	cancelRemaining := instructions != ""
	for j := idx + 1; j < len(partials); j++ {
		if cancelRemaining {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: user provided instructions before this tool could execute"
		} else {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: previous tool was not approved"
		}
	}

	return m.continueAfterAuth(entries, instructions)
}
```

**Acceptance Criteria:**

- [ ] Approved tools execute, rejected tools return error, instructions cancel remaining tools and trigger injection

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.5. Implement continueAfterAuth and injectAndResume

**What:** Add continueAfterAuth that saves the assistant message with all tool entries, then either: (a) if instructions provided → inject synthetic user message and resume stream, or (b) if no instructions → check remaining tools starting from pendingToolIndex+1 for authorization. Add injectAndResume that appends a user message and calls startStream().

**Why:** This is the post-resolution routing: either the user wants the LLM to re-plan (instruction injection = new user turn), or we continue checking the remaining tools in the batch for authorization.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
func (m *Model) continueAfterAuth(entries []config.ToolCallEntry, instructions string) (tea.Model, tea.Cmd) {
	if instructions != "" {
		// Save assistant message with tool results, then inject user message
		m.appendAssistantMsg(config.Message{
			ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
			Role:        config.RoleAssistant,
			ToolCalls:   entries,
			StopReason:  "tool_calls",
		})
		m.stream.authorizationCtx = nil
		m.stream.pendingToolIndex = -1
		return m.injectAndResume(instructions)
	}

	// No instructions — check remaining tools for authorization
	nextIdx := m.stream.pendingToolIndex + 1
	remaining := m.stream.partialTools[nextIdx:]
	
	// Check if any remaining tool needs auth
	for _, p := range remaining {
		tool := m.toolReg.Get(p.name)
		var args map[string]interface{}
		if p.args != "" { json.Unmarshal([]byte(p.args), &args) }
		if m.needsAuthorization(tool, args) {
			// Set up auth for next tool, stay in ModeAuthorize
			return m.setAuthModeFor(p, tool, args, nextIdx), nil
		}
	}

	// No more tools need auth — execute remaining and finish
	remainingEntries := m.executeToolsBatch(remaining)
	for i, e := range remainingEntries {
		entries[nextIdx+i] = e
	}

	m.appendAssistantMsg(config.Message{
		ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:        config.RoleAssistant,
		ToolCalls:   entries,
		StopReason:  "tool_calls",
	})
	m.stream.authorizationCtx = nil
	m.stream.pendingToolIndex = -1
	m.stream.reset()
	m.updateViewportContent()
	return (&m).startStream()
}

func (m *Model) injectAndResume(instructions string) (tea.Model, tea.Cmd) {
	userMsg := config.Message{
		ID:        fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:      config.RoleUser,
		CreatedAt: time.Now(),
		Text:      instructions,
	}
	m.session.appendMsg(userMsg)
	m.stream.authorizationCtx = nil
	m.stream.pendingToolIndex = -1
	return (&m).startStream()
}
```

**Acceptance Criteria:**

- [ ] continueAfterAuth handles instruction injection vs. continuing tool checks. injectAndResume appends user message and resumes stream

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.6. Wire authorization into handleStreamEvent tool_calls flow

**What:** In handleStreamEvent, when StopReason is 'tool_calls': call executeTools. If it returns nil (authorization needed), transition to ModeAuthorize. If it returns entries, save assistant message and resume stream as before.

**Why:** This is the integration point where the existing tool_calls flow checks for the nil return from executeTools and enters authorization mode instead of immediately saving and resuming.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In handleStreamEvent, tool_calls section:
if event.StopReason == "tool_calls" && len(m.stream.partialTools) > 0 {
	toolEntries := (&m).executeTools(m.stream.partialTools)
	if toolEntries == nil {
		// Authorization needed — pause stream
		(&m).setAuthMode()
		m.updateViewportContent()
		return m, nil
	}
	// No authorization needed — existing flow
	(&m).appendAssistantMsg(...)
	m.stream.reset()
	m.updateViewportContent()
	return (&m).startStream()
}

func (m *Model) setAuthMode() {
	m.mode = ModeAuthorize
	m.stream.active = false
}
```

**Acceptance Criteria:**

- [ ] handleStreamEvent enters ModeAuthorize when executeTools returns nil, resumes normally otherwise

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 4. Authorization UI

- **Pattern:** Overlay Mode / State Machine

**Objective:** Build the authorization prompt that appears in the status bar area: yes/no selection with arrow keys, tab to switch to instruction input, shift+tab to return to yes/no, enter to submit. For file tools with Preview, show a diff preview alongside the prompt.

**Success Criteria:** The authorization overlay renders in the status line area. User can navigate yes/no, switch to text input for instructions, and submit. File tools show a diff preview of the proposed changes. All keybindings transition back to streaming or chat mode correctly.

```mermaid
ModeAuthorize → render overlay: tool name, args preview, diff (if available), [Yes]/[No] selection ←→, tab for text, enter to submit → key event routes to handleAuthorizeKey → resolves to resolveAuthorization
```

### 4.1. Add ModeAuthorize and AuthorizationPrompt component

**What:** Add ModeAuthorize to the Mode enum in internal/app/modes.go. Create internal/ui/authorization.go with AuthorizationPrompt struct that renders the yes/no prompt, optional diff preview from Preview, and instruction text input. Tracks selection (yes=0, no=1) and text mode.

**Why:** The prompt is the user-facing interface for authorization. It needs to show the tool name, a human-readable summary, a diff preview when available (from tool.Preview), yes/no selection, and a text mode for instructions.

**Files:**

- ~ internal/app/modes.go
- + internal/ui/authorization.go

**Snippet:**

```
// modes.go
const (
	// ... existing ...
	ModeAuthorize // awaiting user authorization for tool execution
)

// authorization.go
type AuthorizationPrompt struct {
	ToolName      string
	DisplayValue  string
	IsDestructive bool
	PreviewDiff   string     // from tool.Preview — empty if no preview available
	Selection     int        // 0=yes, 1=no
	TextMode      bool       // true = typing instructions
	TextInput     string
	Width         int
}

func (p AuthorizationPrompt) Render() string {
	// 1. Tool label + destructive icon
	label := p.ToolName
	if p.DisplayValue != "" {
		label = fmt.Sprintf("%s(%s)", p.ToolName, truncate(p.DisplayValue, 40))
	}
	icon := ""
	if p.IsDestructive { icon = "⚠ " }

	// 2. If PreviewDiff is non-empty, show a condensed diff indicator
	var diffLine string
	if p.PreviewDiff != "" {
		lines := strings.Count(p.PreviewDiff, "
")
		diffLine = fmt.Sprintf(" (%d lines changed)", lines)
	}

	// 3. Yes/No selection with hint
	yesLabel, noLabel := style.SelectionStyle.Render("Yes"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No")
	if p.Selection == 1 {
		yesLabel, noLabel = noLabel, yesLabel
	}

	return style.StatusLineStyle.Width(p.Width).Render(
		fmt.Sprintf("  %s%s%s  %s / %s  [%s]es / [N]o / [T]ab instructions",
			icon, label, diffLine, yesLabel, noLabel, selectedKey(p.Selection)))
}
```

**Acceptance Criteria:**

- [ ] ModeAuthorize compiles, AuthorizationPrompt renders tool name, destructive icon, diff indicator if available, and yes/no selection

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.2. Wire authorization prompt into Model, View, and input dispatch

**What:** Add authPrompt ui.AuthorizationPrompt to Model struct. In setAuthMode, populate it from authorizationCtx — call tool.Preview if available to get diff. In View (render.go), render authPrompt instead of textarea for ModeAuthorize. In input.go, add handleAuthorizeKey (Left/Right toggle, Tab for text mode, Shift+Tab back, Enter resolves). Wire ModeAuthorize case in handleKey dispatch in update.go.

**Why:** This connects the authorization UI to the model, renders it, handles input, and resolves back to the authorization flow. The Preview call populates the diff preview so the user sees what will change.

**Files:**

- ~ internal/app/app.go
- ~ internal/app/render.go
- ~ internal/app/input.go
- ~ internal/app/update.go

**Snippet:**

```
// app.go — in Model struct
	authPrompt ui.AuthorizationPrompt

// app.go — enhanced setAuthMode
func (m *Model) setAuthMode() {
	m.mode = ModeAuthorize
	m.stream.active = false
	ctx := m.stream.authorizationCtx

	// Get diff preview if tool supports it
	var previewDiff string
	if ctx != nil {
		tool := m.toolReg.Get(ctx.ToolName)
		if tool != nil && tool.Preview != nil {
			result := tool.Preview(ctx.Args)
			if result.Status == tools.ResultStatusSuccess && len(result.Files) > 0 {
				previewDiff = result.Files[0].Diff
			}
		}
	}

	m.authPrompt = ui.AuthorizationPrompt{
		ToolName:      ctx.ToolName,
		DisplayValue:  ctx.DisplayValue,
		IsDestructive: ctx.IsDestructive,
		PreviewDiff:   previewDiff,
		Selection:     0,
		Width:         m.width,
	}
}

// render.go
case ModeAuthorize:
	sections = append(sections, m.authPrompt.Render())
// Skip textarea in auth mode
if m.mode != ModeAuthorize {
	sections = append(sections, m.textarea.View())
}

// input.go
func (m Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Left) && !m.authPrompt.TextMode:
		m.authPrompt.Selection = 0
		return m, nil
	case key.Matches(msg, keys.Right) && !m.authPrompt.TextMode:
		m.authPrompt.Selection = 1
		return m, nil
	case msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):
		m.authPrompt.TextMode = true
		return m, nil
	case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):
		m.authPrompt.TextMode = false
		return m, nil
	case key.Matches(msg, keys.Send):
		approved := m.authPrompt.Selection == 0
		return m.resolveAuthorization(approved, m.authPrompt.TextInput)
	default:
		if m.authPrompt.TextMode {
			// Handle runes/backspace for text input
			// ...
		}
	}
	return m, nil
}

// update.go — add case
case ModeAuthorize:
	return m.handleAuthorizeKey(msg)
```

**Acceptance Criteria:**

- [ ] authPrompt is populated with preview diff from tool.Preview. View renders authPrompt in ModeAuthorize. Key input routes to handleAuthorizeKey → resolveAuthorization

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 5. System Prompt Update

- **Pattern:** Instruction Injection

**Objective:** Update the system prompt so the LLM knows it must set the destructive field on every bash call, and understands the semantics of what makes a command destructive.

**Success Criteria:** System prompt clearly defines destructive vs read-only bash commands with examples. LLM consistently sets the correct value.

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

- [ ] System prompt contains clear destructive field instructions for bash with examples of true and false cases

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

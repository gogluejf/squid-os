# EPIC: Tool Authorization
Why: Users need granular control over when tool executions require confirmation. The LLM currently executes all tools blindly, which can lead to unintended file modifications or destructive commands.
Outcomes: Users can choose between auto-execute, ask-on-write, or ask-for-all modes. Destructive bash calls require an explicit 'destructive' arg from the LLM. File tools leverage their existing Preview for safe authorization prompts. Pending tool calls are cancelled when the user adds instructions mid-execution, allowing the LLM to re-plan.

## MILESTONE: 1 - Authorization Setting
Pattern: Configuration Flag
Objective: Add a user-configurable setting that determines which tools require confirmation before execution.
Success: Settings exposes an Authorization field with three valid values (auto, ask-on-write, ask-for-all). Invalid values fall back to auto.
Diagram: Settings.json → Authorization field → parsed at startup → drives authorization gate behavior per tool call in executeTools

### TASK: 1.1 - Add Authorization field and constants to Settings
Layer: Domain
What: Add Authorization string field and validation constants to Settings struct in internal/config/settings.go with default value 'auto'.
Why: Stores the user's preference for tool authorization mode: auto, ask-on-write, or ask-for-all.
Files: ~ internal/config/settings.go
Snippet: const (\n\tAuthorizationAuto        = "auto"\n\tAuthorizationAskOnWrite  = "ask-on-write"\n\tAuthorizationAskForAll   = "ask-for-all"\n)\n\ntype Settings struct {\n\t// ... existing fields ...\n\tAuthorization string `json:"authorization"` // auto | ask-on-write | ask-for-all\n}\n\nfunc DefaultSettings() Settings {\n\treturn Settings{\n\t\t// ... existing defaults ...\n\t\tAuthorization: AuthorizationAuto,\n\t}\n}\n\n// ValidateAuthorization returns the normalized authorization mode, falling back to auto.\nfunc (s Settings) ValidateAuthorization() string {\n\tswitch s.Authorization {\n\tcase AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll:\n\t\treturn s.Authorization\n\tdefault:\n\t\treturn AuthorizationAuto\n\t}\n}
Acceptance: Settings struct compiles, defaults to 'auto', rejects unknown values by falling back to auto
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 2 - Destructive Bash
Pattern: Schema Extension
Objective: Add a required destructive boolean field to the bash tool schema so the LLM must declare whether a command modifies disk state.
Success: Bash tool schema requires destructive field — omitting it returns an error. The authorization gate reads destructive from args before execution.
Diagram: LLM → bash({command, destructive: true/false}) → schema validates required field → authorization gate checks args destructiveness before Execute → tool runs or user decides

### TASK: 2.1 - Add destructive to bash schema and validation
Layer: Infrastructure
What: Add destructive (boolean, required) to the bash tool JSON schema in internal/tools/bash.go. In Execute, validate the arg and return error if missing or non-boolean.
Why: The LLM must explicitly declare whether a bash command modifies files or system state. No default — omitting it returns an error. This is the only destructiveness signal needed for bash.
Files: ~ internal/tools/bash.go
Snippet: "properties": {\n    "command": { "type": "string", ... },\n    "timeout": { "type": "number", ... },\n    "destructive": {\n      "type": "boolean",\n      "description": "Must be true if the command modifies files, deletes data, or changes system state (rm, mv, cp, mkdir, chmod, sed -i, apt-get, pip install, git commit). Must be false for read-only commands (cat, ls, grep, find, git status, git diff, wc, head, tail, df, ps, curl GET). This field is required."\n    }\n  },\n  "required": ["command", "destructive"]\n\n// In Execute:\ndestructive, ok := args["destructive"].(bool)\nif !ok {\n  return ToolResult{Status: ResultStatusError, Error: "destructive is required and must be a boolean"}\n}
Acceptance: Schema is valid JSON, destructive is required, omitting it causes a validation error
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 2.2 - Add IsDestructive helper to Tool for pre-execution checks
Layer: Infrastructure
What: Add IsDestructive func(map[string]interface{}) bool to the Tool struct. Set it true for write_file, edit_file (always destructive). For bash, read destructive from args. All other tools return false (or have nil IsDestructive).
Why: The authorization gate needs to determine at pre-execution time whether a tool is destructive, without running it. For file tools destructiveness is static. For bash it comes from the args. This leverages the existing Preview on write_file/edit_file — they are always write operations.
Files: ~ internal/tools/tools.go
Snippet: type Tool struct {\n\t// ... existing fields ...\n\tIsDestructive func(args map[string]interface{}) bool // nil = never destructive\n}\n\nvar WriteFile = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool { return true },\n}\n\nvar EditFile = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool { return true },\n}\n\nvar Bash = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool {\n\t\td, ok := args["destructive"].(bool)\n\t\treturn ok && d\n\t},\n}\n\n// ReadFile, Open, SkillLoad, SkillList, SetWorkingDirTool: IsDestructive is nil (never destructive)
Acceptance: write_file, edit_file always report destructive. bash reports based on args destructive field. read_file, skill_load, skill_list, set_working_dir, open report false
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 3 - Authorization Gate
Pattern: Guard Clause / Interrupt
Objective: Intercept tool execution inside the iterate-tools flow. When authorization mode requires it, pause and ask the user before executing. On instruction injection, cancel all remaining pending tools and resume stream with the user's context.
Success: In ask-on-write, destructive tools pause for confirmation and read-only tools auto-execute. In ask-for-all, every tool pauses. On rejection or instruction injection, remaining pending tool calls are cancelled and the stream resumes. The flow iterates tool by tool — not batching — when authorization is active.
Diagram: handleStreamEvent tool_calls stop → iterate partialTools one at a time → needsAuthorization? → yes: enter ModeAuthorize and pause stream → user responds (y/n + optional text) → execute tool OR mark rejected → if instructions attached: cancel remaining tools, inject user message, startStream → if accepted without instructions: check next tool for auth, repeat

### TASK: 3.1 - Add authorization types and authorization context
Layer: Domain
What: Create internal/app/authorization.go with AuthResult (bool approved + string instructions) and AuthorizationContext holding the pending tool name, args, display value, destructive flag, and result.
Why: Simplified auth result: just approved (bool) and instructions (string). Four variants collapse to two fields. The context holds everything needed for the UI prompt and execution decision.
Files: + internal/app/authorization.go
Snippet: package app\n\ntype AuthResult struct {\n\tApproved      bool\n\tInstructions  string // empty = plain yes/no\n}\n\nfunc (r AuthResult) HasInstructions() bool { return r.Instructions != "" }\n\ntype AuthorizationContext struct {\n\tToolName        string\n\tArgs            map[string]interface{}\n\tArgsJSON        string\n\tDisplayValue    string\n\tIsDestructive   bool\n\tResult          AuthResult\n}\n\nfunc (c *AuthorizationContext) IsActionable() bool {\n\treturn c.Result.Approved || c.Result.Instructions != ""\n}
Acceptance: Struct and types compile, approved + instructions replaces the four-variant enum
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.2 - Add authorization state to stream and Model.needsAuthorization
Layer: Application
What: Add authorizationCtx *AuthorizationContext and pendingToolIndex int to streamState. Add needsAuthorization method on Model that checks settings authorization mode and tool.IsDestructive.
Why: The stream loop needs to track when it's paused awaiting user authorization and which tool is pending. needsAuthorization centralizes the mode-checking logic.
Files: ~ internal/app/stream.go
Snippet: // In streamState:\n\tauthorizationCtx *AuthorizationContext // non-nil when paused awaiting auth\n\tpendingToolIndex int                   // index into partialTools being authorized\n\nfunc (m Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool {\n\tswitch m.settings.ValidateAuthorization() {\n\tcase config.AuthorizationAskForAll:\n\t\treturn true\n\tcase config.AuthorizationAskOnWrite:\n\t\treturn tool != nil && tool.IsDestructive != nil && tool.IsDestructive(args)\n\tdefault: // auto\n\t\treturn false\n\t}\n}
Acceptance: needsAuthorization returns true for all tools in ask-for-all, only destructive in ask-on-write, nothing in auto
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.3 - Refactor executeTools to support authorization interruption per tool
Layer: Application
What: Rewrite executeTools in stream.go to iterate tools one at a time. Before each tool's Execute, check needsAuthorization. If auth is needed, populate authorizationCtx with the pending tool info, set pendingToolIndex, and return nil (signals caller to enter authorization mode). If no auth needed, execute that tool and continue to the next. Only batch-execute the remaining tools if none of them need auth.
Why: Currently executeTools runs all tools in a batch. With authorization we must process one at a time and potentially pause. The key insight: iterate, check, interrupt — not pre-scan then execute. This keeps the flow natural and avoids duplicating the execution logic.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) executeTools(partials []partialTool) []config.ToolCallEntry {\n\t// Fast path: if auth mode is auto, run everything as before (existing logic)\n\tif m.settings.ValidateAuthorization() == config.AuthorizationAuto {\n\t\treturn m.executeToolsBatch(partials)\n\t}\n\n\t// Iterative path: process tool by tool, may pause for authorization\n\treturn m.executeToolsIterative(partials, 0, make([]config.ToolCallEntry, 0, len(partials)))\n}\n\nfunc (m *Model) executeToolsIterative(partials []partialTool, idx int, entries []config.ToolCallEntry) []config.ToolCallEntry {\n\tif idx >= len(partials) {\n\t\treturn entries\n\t}\n\tp := partials[idx]\n\ttool := m.toolReg.Get(p.name)\n\tvar args map[string]interface{}\n\tif p.args != "" {\n\t\tjson.Unmarshal([]byte(p.args), &args)\n\t}\n\n\tif m.needsAuthorization(tool, args) {\n\t\t// Save already-executed entries, set up authorization context, return nil to pause\n\t\tm.stream.pendingToolIndex = idx\n\t\tisDestructive := false\n\t\tif tool != nil && tool.IsDestructive != nil {\n\t\t\tisDestructive = tool.IsDestructive(args)\n\t\t}\n\t\tm.stream.authorizationCtx = &AuthorizationContext{\n\t\t\tToolName:      p.name,\n\t\t\tArgs:          args,\n\t\t\tArgsJSON:      p.args,\n\t\t\tDisplayValue:  tool.DisplayValue(p.args),\n\t\t\tIsDestructive: isDestructive,\n\t\t}\n\t\treturn nil // signals handleStreamEvent to enter ModeAuthorize\n\t}\n\n\t// Execute this tool inline, append entry, continue to next\n\tentry := m.executeSingleTool(p)\n\treturn m.executeToolsIterative(partials, idx+1, append(entries, entry))\n}\n\nfunc (m *Model) executeToolsBatch(partials []partialTool) []config.ToolCallEntry {\n\t// Existing executeTools body — unchanged for auto mode\n}
Acceptance: In auto mode, behavior is identical to before. In ask modes, tools are processed one at a time and execution pauses when auth is needed, returning nil
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.4 - Implement executeSingleTool and resolveAuthorization
Layer: Application
What: Add executeSingleTool (extracted from existing executeTools per-tool logic) and resolveAuthorization that handles the user's response. On approval: execute the pending tool, then check remaining tools. On rejection: mark error. If instructions attached: cancel all remaining tools and signal stream resume with synthetic user message injection.
Why: executeSingleTool reuses the existing per-tool execution logic (validation, checksum check, side effects). resolveAuthorization is the post-user-response branching point — the core of the authorization flow.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) executeSingleTool(p partialTool) config.ToolCallEntry {\n\t// (Extracted from existing executeTools loop body — validation, execution, file state merge, set_working_dir side effect)\n\t// ...\n}\n\nfunc (m *Model) resolveAuthorization(approved bool, instructions string) (tea.Model, tea.Cmd) {\n\tctx := m.stream.authorizationCtx\n\tidx := m.stream.pendingToolIndex\n\tpartials := m.stream.partialTools\n\tentries := make([]config.ToolCallEntry, len(partials))\n\n\tfor i, p := range partials {\n\t\tentries[i] = m.buildEmptyEntry(p)\n\t}\n\n\tif approved {\n\t\tentries[idx] = m.executeSingleTool(partials[idx])\n\t} else {\n\t\tentries[idx].Execution.Status = tools.ResultStatusError\n\t\tentries[idx].Execution.Error = "rejected by user — tool was not executed"\n\t}\n\n\t// If instructions were provided, cancel all remaining tools\n\tcancelRemaining := instructions != ""\n\tfor j := idx + 1; j < len(partials); j++ {\n\t\tif cancelRemaining {\n\t\t\tentries[j].Execution.Status = tools.ResultStatusError\n\t\t\tentries[j].Execution.Error = "cancelled: user provided instructions before this tool could execute"\n\t\t} else {\n\t\t\tentries[j].Execution.Status = tools.ResultStatusError\n\t\t\tentries[j].Execution.Error = "cancelled: previous tool was not approved"\n\t\t}\n\t}\n\n\treturn m.continueAfterAuth(entries, instructions)\n}
Acceptance: Approved tools execute, rejected tools return error, instructions cancel remaining tools and trigger injection
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.5 - Implement continueAfterAuth and injectAndResume
Layer: Application
What: Add continueAfterAuth that saves the assistant message with all tool entries, then either: (a) if instructions provided → inject synthetic user message and resume stream, or (b) if no instructions → check remaining tools starting from pendingToolIndex+1 for authorization. Add injectAndResume that appends a user message and calls startStream().
Why: This is the post-resolution routing: either the user wants the LLM to re-plan (instruction injection = new user turn), or we continue checking the remaining tools in the batch for authorization.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) continueAfterAuth(entries []config.ToolCallEntry, instructions string) (tea.Model, tea.Cmd) {\n\tif instructions != "" {\n\t\t// Save assistant message with tool results, then inject user message\n\t\tm.appendAssistantMsg(config.Message{\n\t\t\tID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),\n\t\t\tRole:        config.RoleAssistant,\n\t\t\tToolCalls:   entries,\n\t\t\tStopReason:  "tool_calls",\n\t\t})\n\t\tm.stream.authorizationCtx = nil\n\t\tm.stream.pendingToolIndex = -1\n\t\treturn m.injectAndResume(instructions)\n\t}\n\n\t// No instructions — check remaining tools for authorization\n\tnextIdx := m.stream.pendingToolIndex + 1\n\tremaining := m.stream.partialTools[nextIdx:]\n\t\n\t// Check if any remaining tool needs auth\n\tfor _, p := range remaining {\n\t\ttool := m.toolReg.Get(p.name)\n\t\tvar args map[string]interface{}\n\t\tif p.args != "" { json.Unmarshal([]byte(p.args), &args) }\n\t\tif m.needsAuthorization(tool, args) {\n\t\t\t// Set up auth for next tool, stay in ModeAuthorize\n\t\t\treturn m.setAuthModeFor(p, tool, args, nextIdx), nil\n\t\t}\n\t}\n\n\t// No more tools need auth — execute remaining and finish\n\tremainingEntries := m.executeToolsBatch(remaining)\n\tfor i, e := range remainingEntries {\n\t\tentries[nextIdx+i] = e\n\t}\n\n\tm.appendAssistantMsg(config.Message{\n\t\tID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),\n\t\tRole:        config.RoleAssistant,\n\t\tToolCalls:   entries,\n\t\tStopReason:  "tool_calls",\n\t})\n\tm.stream.authorizationCtx = nil\n\tm.stream.pendingToolIndex = -1\n\tm.stream.reset()\n\tm.updateViewportContent()\n\treturn (&m).startStream()\n}\n\nfunc (m *Model) injectAndResume(instructions string) (tea.Model, tea.Cmd) {\n\tuserMsg := config.Message{\n\t\tID:        fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),\n\t\tRole:      config.RoleUser,\n\t\tCreatedAt: time.Now(),\n\t\tText:      instructions,\n\t}\n\tm.session.appendMsg(userMsg)\n\tm.stream.authorizationCtx = nil\n\tm.stream.pendingToolIndex = -1\n\treturn (&m).startStream()\n}
Acceptance: continueAfterAuth handles instruction injection vs. continuing tool checks. injectAndResume appends user message and resumes stream
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.6 - Wire authorization into handleStreamEvent tool_calls flow
Layer: Application
What: In handleStreamEvent, when StopReason is 'tool_calls': call executeTools. If it returns nil (authorization needed), transition to ModeAuthorize. If it returns entries, save assistant message and resume stream as before.
Why: This is the integration point where the existing tool_calls flow checks for the nil return from executeTools and enters authorization mode instead of immediately saving and resuming.
Files: ~ internal/app/stream.go
Snippet: // In handleStreamEvent, tool_calls section:\nif event.StopReason == "tool_calls" && len(m.stream.partialTools) > 0 {\n\ttoolEntries := (&m).executeTools(m.stream.partialTools)\n\tif toolEntries == nil {\n\t\t// Authorization needed — pause stream\n\t\t(&m).setAuthMode()\n\t\tm.updateViewportContent()\n\t\treturn m, nil\n\t}\n\t// No authorization needed — existing flow\n\t(&m).appendAssistantMsg(...)\n\tm.stream.reset()\n\tm.updateViewportContent()\n\treturn (&m).startStream()\n}\n\nfunc (m *Model) setAuthMode() {\n\tm.mode = ModeAuthorize\n\tm.stream.active = false\n}
Acceptance: handleStreamEvent enters ModeAuthorize when executeTools returns nil, resumes normally otherwise
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 4 - Authorization UI
Pattern: Overlay Mode / State Machine
Objective: Build the authorization prompt that appears in the status bar area: yes/no selection with arrow keys, tab to switch to instruction input, shift+tab to return to yes/no, enter to submit. For file tools with Preview, show a diff preview alongside the prompt.
Success: The authorization overlay renders in the status line area. User can navigate yes/no, switch to text input for instructions, and submit. File tools show a diff preview of the proposed changes. All keybindings transition back to streaming or chat mode correctly.
Diagram: ModeAuthorize → render overlay: tool name, args preview, diff (if available), [Yes]/[No] selection ←→, tab for text, enter to submit → key event routes to handleAuthorizeKey → resolves to resolveAuthorization

### TASK: 4.1 - Add ModeAuthorize and AuthorizationPrompt component
Layer: Interface
What: Add ModeAuthorize to the Mode enum in internal/app/modes.go. Create internal/ui/authorization.go with AuthorizationPrompt struct that renders the yes/no prompt, optional diff preview from Preview, and instruction text input. Tracks selection (yes=0, no=1) and text mode.
Why: The prompt is the user-facing interface for authorization. It needs to show the tool name, a human-readable summary, a diff preview when available (from tool.Preview), yes/no selection, and a text mode for instructions.
Files: ~ internal/app/modes.go
Files: + internal/ui/authorization.go
Snippet: // modes.go\nconst (\n\t// ... existing ...\n\tModeAuthorize // awaiting user authorization for tool execution\n)\n\n// authorization.go\ntype AuthorizationPrompt struct {\n\tToolName      string\n\tDisplayValue  string\n\tIsDestructive bool\n\tPreviewDiff   string     // from tool.Preview — empty if no preview available\n\tSelection     int        // 0=yes, 1=no\n\tTextMode      bool       // true = typing instructions\n\tTextInput     string\n\tWidth         int\n}\n\nfunc (p AuthorizationPrompt) Render() string {\n\t// 1. Tool label + destructive icon\n\tlabel := p.ToolName\n\tif p.DisplayValue != "" {\n\t\tlabel = fmt.Sprintf("%s(%s)", p.ToolName, truncate(p.DisplayValue, 40))\n\t}\n\ticon := ""\n\tif p.IsDestructive { icon = "⚠ " }\n\n\t// 2. If PreviewDiff is non-empty, show a condensed diff indicator\n\tvar diffLine string\n\tif p.PreviewDiff != "" {\n\t\tlines := strings.Count(p.PreviewDiff, "\n")\n\t\tdiffLine = fmt.Sprintf(" (%d lines changed)", lines)\n\t}\n\n\t// 3. Yes/No selection with hint\n\tyesLabel, noLabel := style.SelectionStyle.Render("Yes"),\n\t\tlipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No")\n\tif p.Selection == 1 {\n\t\tyesLabel, noLabel = noLabel, yesLabel\n\t}\n\n\treturn style.StatusLineStyle.Width(p.Width).Render(\n\t\tfmt.Sprintf("  %s%s%s  %s / %s  [%s]es / [N]o / [T]ab instructions",\n\t\t\ticon, label, diffLine, yesLabel, noLabel, selectedKey(p.Selection)))\n}
Acceptance: ModeAuthorize compiles, AuthorizationPrompt renders tool name, destructive icon, diff indicator if available, and yes/no selection
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 4.2 - Wire authorization prompt into Model, View, and input dispatch
Layer: Interface
What: Add authPrompt ui.AuthorizationPrompt to Model struct. In setAuthMode, populate it from authorizationCtx — call tool.Preview if available to get diff. In View (render.go), render authPrompt instead of textarea for ModeAuthorize. In input.go, add handleAuthorizeKey (Left/Right toggle, Tab for text mode, Shift+Tab back, Enter resolves). Wire ModeAuthorize case in handleKey dispatch in update.go.
Why: This connects the authorization UI to the model, renders it, handles input, and resolves back to the authorization flow. The Preview call populates the diff preview so the user sees what will change.
Files: ~ internal/app/app.go
Files: ~ internal/app/render.go
Files: ~ internal/app/input.go
Files: ~ internal/app/update.go
Snippet: // app.go — in Model struct\n\tauthPrompt ui.AuthorizationPrompt\n\n// app.go — enhanced setAuthMode\nfunc (m *Model) setAuthMode() {\n\tm.mode = ModeAuthorize\n\tm.stream.active = false\n\tctx := m.stream.authorizationCtx\n\n\t// Get diff preview if tool supports it\n\tvar previewDiff string\n\tif ctx != nil {\n\t\ttool := m.toolReg.Get(ctx.ToolName)\n\t\tif tool != nil && tool.Preview != nil {\n\t\t\tresult := tool.Preview(ctx.Args)\n\t\t\tif result.Status == tools.ResultStatusSuccess && len(result.Files) > 0 {\n\t\t\t\tpreviewDiff = result.Files[0].Diff\n\t\t\t}\n\t\t}\n\t}\n\n\tm.authPrompt = ui.AuthorizationPrompt{\n\t\tToolName:      ctx.ToolName,\n\t\tDisplayValue:  ctx.DisplayValue,\n\t\tIsDestructive: ctx.IsDestructive,\n\t\tPreviewDiff:   previewDiff,\n\t\tSelection:     0,\n\t\tWidth:         m.width,\n\t}\n}\n\n// render.go\ncase ModeAuthorize:\n\tsections = append(sections, m.authPrompt.Render())\n// Skip textarea in auth mode\nif m.mode != ModeAuthorize {\n\tsections = append(sections, m.textarea.View())\n}\n\n// input.go\nfunc (m Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {\n\tswitch {\n\tcase key.Matches(msg, keys.Left) && !m.authPrompt.TextMode:\n\t\tm.authPrompt.Selection = 0\n\t\treturn m, nil\n\tcase key.Matches(msg, keys.Right) && !m.authPrompt.TextMode:\n\t\tm.authPrompt.Selection = 1\n\t\treturn m, nil\n\tcase msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):\n\t\tm.authPrompt.TextMode = true\n\t\treturn m, nil\n\tcase msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):\n\t\tm.authPrompt.TextMode = false\n\t\treturn m, nil\n\tcase key.Matches(msg, keys.Send):\n\t\tapproved := m.authPrompt.Selection == 0\n\t\treturn m.resolveAuthorization(approved, m.authPrompt.TextInput)\n\tdefault:\n\t\tif m.authPrompt.TextMode {\n\t\t\t// Handle runes/backspace for text input\n\t\t\t// ...\n\t\t}\n\t}\n\treturn m, nil\n}\n\n// update.go — add case\ncase ModeAuthorize:\n\treturn m.handleAuthorizeKey(msg)
Acceptance: authPrompt is populated with preview diff from tool.Preview. View renders authPrompt in ModeAuthorize. Key input routes to handleAuthorizeKey → resolveAuthorization
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 5 - System Prompt Update
Pattern: Instruction Injection
Objective: Update the system prompt so the LLM knows it must set the destructive field on every bash call, and understands the semantics of what makes a command destructive.
Success: System prompt clearly defines destructive vs read-only bash commands with examples. LLM consistently sets the correct value.
Diagram: sys-prompt.go → DefaultAssistantPrompt() includes new bash destructive field instructions → injected into every API request as system message

### TASK: 5.1 - Update system prompt for destructive field
Layer: Infrastructure
What: Add a new section to the default system prompt in internal/config/sys-prompt.go explaining the destructive field for bash: when to set true, when to set false, and that the field is required.
Why: The LLM needs explicit instructions to populate the required destructive field correctly on every bash call.
Files: ~ internal/config/sys-prompt.go
Snippet: ## Bash Tool\n- The bash tool requires a "destructive" boolean parameter on every call.\n- Set destructive: true if the command modifies files, deletes data, creates directories, installs packages, or changes system state (e.g., rm, mv, cp, mkdir, chmod, apt-get, pip install, git commit, sed -i).\n- Set destructive: false for read-only commands (e.g., cat, ls, grep, find, git status, git diff, wc, head, tail, df, ps, curl GET).\n- Omitting the destructive field will result in an error.
Acceptance: System prompt contains clear destructive field instructions for bash with examples of true and false cases
Verification: cd /home/goglue/src/squid-os && go build ./...

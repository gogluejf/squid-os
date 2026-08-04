# EPIC: Tool Authorization
Why: Users need granular control over when tool executions require confirmation. The LLM currently executes all tools blindly, which can lead to unintended file modifications or destructive commands.
Outcomes: Users can choose between auto-execute, ask-on-write, or ask-for-all modes. The LLM must declare destructive intent on bash calls. Pending tool calls are cancelled when the user adds instructions mid-execution.

## MILESTONE: 1 - Authorization Setting
Pattern: Configuration Flag
Objective: Add a user-configurable setting that determines which tools require confirmation before execution.
Success: Settings exposes an Authorization field with three valid values. Invalid values fall back to auto.
Diagram: Settings.json → Authorization field → parsed at startup → drives authorization gate behavior per tool call

### TASK: 1.1 - Add Authorization field to Settings
Layer: Domain
What: Add Authorization string field to Settings struct in internal/config/settings.go with default value "auto".
Why: Stores the user's preference for tool authorization mode: auto, ask-on-write, or ask-for-all.
Files: ~ internal/config/settings.go
Snippet: type Settings struct {\n\t// ... existing fields ...\n\tAuthorization string \u0060json:"authorization"\u0060 // auto | ask-on-write | ask-for-all\n}\n\nfunc DefaultSettings() Settings {\n\treturn Settings{\n\t\t// ... existing defaults ...\n\t\tAuthorization: "auto",\n\t}\n}\n\n// ValidateAuthorization returns the normalized authorization mode.\nfunc (s Settings) ValidateAuthorization() string {\n\tswitch s.Authorization {\n\tcase "auto", "ask-on-write", "ask-for-all":\n\t\treturn s.Authorization\n\tdefault:\n\t\treturn "auto"\n\t}\n}
Acceptance: Settings struct compiles, defaults to "auto", rejects unknown values by falling back to auto
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 1.2 - Add Authorization enum constants
Layer: Domain
What: Add AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll string constants in settings.go.
Why: Avoid magic strings scattered across the codebase. Single source of truth for mode names.
Files: ~ internal/config/settings.go
Snippet: const (\n\tAuthorizationAuto        = "auto"\n\tAuthorizationAskOnWrite  = "ask-on-write"\n\tAuthorizationAskForAll   = "ask-for-all"\n)
Acceptance: Constants match the three valid values, used in ValidateAuthorization
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 2 - Destructive Bash
Pattern: Schema Extension
Objective: Add a required destructive boolean field to the bash tool schema so the LLM must declare whether a command modifies disk state.
Success: Bash tool schema requires destructive field, returns error if omitted. Tool tracks destructiveness on the result.
Diagram: LLM → bash({"command": "rm -rf ...", "destructive": true}) → schema validates required field → Execute checks destructiveness → ToolResult carries Destructive flag

### TASK: 2.1 - Add destructive to bash schema
Layer: Infrastructure
What: Add destructive (boolean, required) to the bash tool JSON schema in internal/tools/bash.go.
Why: The LLM must explicitly declare whether a bash command modifies files or system state. No default — omitting it returns an error.
Files: ~ internal/tools/bash.go
Snippet: "properties": {\n\t"command": {\n\t\t"type": "string",\n\t\t"description": "The shell command to execute"\n\t},\n\t"timeout": {\n\t\t"type": "number",\n\t\t"description": "Timeout in milliseconds (default 120000)"\n\t},\n\t"destructive": {\n\t\t"type": "boolean",\n\t\t"description": "Must be true if the command modifies files, deletes data, or changes system state. Must be false for read-only commands (cat, ls, grep, find, git status, etc.). This field is required."\n\t}\n},\n"required": ["command", "destructive"]
Acceptance: Schema is valid JSON, destructive is required, omitting it causes a validation error
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 2.2 - Add Destructive field to ToolResult
Layer: Infrastructure
What: Add Destructive bool to ToolResult struct in internal/tools/tools.go. Set it from args["destructive"] in the bash Execute function.
Why: The authorization gate needs to know at execution time whether a tool is destructive, before running it.
Files: ~ internal/tools/tools.go
Files: ~ internal/tools/bash.go
Snippet: type ToolResult struct {\n\tStatus    string             // ...\n\tResult    string             // ...\n\tError     string             // ...\n\tDestructive bool             // true if this tool modifies disk state\n\tFiles     []config.FileEntry // ...\n}\n\n// In bash Execute:\ndestructive, ok := args["destructive"].(bool)\nif !ok {\n\treturn ToolResult{Status: ResultStatusError, Error: "destructive is required and must be a boolean"}\n}\n// ... after execution ...\nreturn ToolResult{Status: ResultStatusSuccess, Result: result.String(), Destructive: destructive}
Acceptance: Destructive field populated on bash results, error returned when omitted
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 2.3 - Mark inherently destructive tools
Layer: Infrastructure
What: Add IsDestructive func()bool to the Tool struct. Set it true for write_file, edit_file, skill_build. Bash uses the runtime destructive arg.
Why: The authorization gate needs a pre-execution way to determine if a tool is destructive, without running it first. For file tools, destructiveness is static. For bash, it comes from the args.
Files: ~ internal/tools/tools.go
Snippet: type Tool struct {\n\t// ... existing fields ...\n\tIsDestructive func(args map[string]interface{}) bool // nil = never destructive\n}\n\nvar WriteFile = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool { return true },\n}\n\nvar EditFile = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool { return true },\n}\n\nvar SkillBuild = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool { return true },\n}\n\nvar Bash = Tool{\n\t// ...\n\tIsDestructive: func(args map[string]interface{}) bool {\n\t\td, ok := args["destructive"].(bool)\n\t\treturn ok && d\n\t},\n}
Acceptance: write_file, edit_file, skill_build always report destructive. bash reports based on args. read_file, skill_load, skill_list, set_working_dir, open report false
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 3 - Authorization Gate
Pattern: Guard Clause / Interrupt
Objective: Intercept tool execution in the stream loop. When authorization mode requires it, pause and ask the user before executing. On rejection, cancel pending tool calls.
Success: In ask-on-write mode, destructive tools pause for confirmation, read-only tools auto-execute. In ask-for-all, every tool pauses. Rejected or instruction-annotated tools cancel remaining pending calls and resume stream.
Diagram: Stream → tool_calls stop → iterate partialTools → check authorization needed? → yes: enter ModeAuthorize → user responds (y/n + optional instructions) → execute tool or reject → if instructions injected: cancel remaining tools, resume stream immediately. else: continue to next tool

### TASK: 3.1 - Add authorization types and constants
Layer: Domain
What: Create internal/app/authorization.go with AuthorizationResult enum (Accepted, Rejected, AcceptedWithInstructions) and AuthorizationContext struct holding the pending tool, args, user instructions, and result.
Why: Encapsulates the authorization state machine: what tool is pending, what the user responded, and whether extra instructions were provided.
Files: + internal/app/authorization.go
Snippet: package app\n\ntype AuthResult int\nconst (\n\tAuthAccepted           AuthResult = iota // plain yes\n\tAuthRejected                            // plain no\n\tAuthAcceptedWithInstructions            // yes + instructions\n\tAuthRejectedWithInstructions            // no + instructions\n)\n\ntype AuthorizationContext struct {\n\tToolName        string                  // tool being authorized\n\tArgs            map[string]interface{}  // parsed args for display\n\tArgsJSON        string                  // raw args for display\n\tDisplayValue    string                  // from tool.DisplayParam\n\tResult          AuthResult\n\tUserInstructions string                // optional text the user attached\n\tIsDestructive   bool\n}\n\nfunc (c *AuthorizationContext) IsActionable() bool {\n\treturn c.Result != 0\n}
Acceptance: Struct and types compile, provides all fields needed by the UI and execution logic
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.2 - Add authorization mode to streamState
Layer: Application
What: Add authorizationContext field to streamState in stream.go. Add pendingToolIndex int to track which partialTool is being authorized. Add needsAuthorization helper method on Model.
Why: The stream loop needs to know when it's paused waiting for user authorization and which tool is pending.
Files: ~ internal/app/stream.go
Snippet: // In streamState struct:\n\tauthorizationCtx *AuthorizationContext // non-nil when paused awaiting auth\n\tpendingToolIndex int                   // index into partialTools being authorized\n\n// In Model:\nfunc (m Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool {\n\tauthMode := m.settings.ValidateAuthorization()\n\tswitch authMode {\n\tcase config.AuthorizationAskForAll:\n\t\treturn true\n\tcase config.AuthorizationAskOnWrite:\n\t\tif tool != nil && tool.IsDestructive != nil {\n\t\t\treturn tool.IsDestructive(args)\n\t\t}\n\t\treturn false\n\tdefault: // auto\n\t\treturn false\n\t}\n}
Acceptance: needsAuthorization returns true for all tools in ask-for-all, only destructive in ask-on-write, nothing in auto
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.3 - Refactor executeTools to support authorization interruption
Layer: Application
What: Rewrite executeTools in stream.go to check authorization before each tool execution. If authorization is needed, return a single AuthorizationContext and the caller (handleStreamEvent) pauses the stream. Remaining tools are left unexecuted until the user responds.
Why: Currently executeTools runs all tools in a batch. With authorization, we must process one at a time and potentially pause for user input, canceling remaining tools on rejection or instruction injection.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) executeTools(partials []partialTool) []config.ToolCallEntry {\n\t// Phase 1: Determine if any tool needs authorization\n\tfor i, p := range partials {\n\t\ttool := m.toolReg.Get(p.name)\n\t\tvar args map[string]interface{}\n\t\tif p.args != "" {\n\t\t\t_ = json.Unmarshal([]byte(p.args), &args)\n\t\t}\n\t\tif m.needsAuthorization(tool, args) {\n\t\t\t// Pause here — set up authorization context\n\t\t\tisDestructive := false\n\t\t\tif tool != nil && tool.IsDestructive != nil {\n\t\t\t\tisDestructive = tool.IsDestructive(args)\n\t\t\t}\n\t\t\tm.stream.authorizationCtx = &AuthorizationContext{\n\t\t\t\tToolName:       p.name,\n\t\t\t\tArgs:           args,\n\t\t\t\tArgsJSON:       p.args,\n\t\t\t\tDisplayValue:   tool.DisplayValue(p.args),\n\t\t\t\tIsDestructive:  isDestructive,\n\t\t\t}\n\t\t\tm.stream.pendingToolIndex = i\n\t\t\treturn nil // nil signals caller to enter authorization mode\n\t\t}\n\t}\n\t// Phase 2: No authorization needed — execute all (existing logic)\n\treturn m.executeToolsUnauthenticated(partials)\n}\n\nfunc (m *Model) executeToolsUnauthenticated(partials []partialTool) []config.ToolCallEntry {\n\t// ... existing executeTools body (renamed) ...\n}
Acceptance: When no authorization is needed, behavior is identical to before. When authorization is needed, returns nil and sets authorizationCtx
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.4 - Handle authorization response in stream event loop
Layer: Application
What: In handleStreamEvent, after executeTools returns nil (authorization needed), transition to ModeAuthorize. After user responds: if Accepted → execute that single tool, check next tool. If Rejected or WithInstructions → mark remaining tools as cancelled, build entries with appropriate error/result, resume stream. If instructions attached → inject synthetic user message before resuming.
Why: This is the core state transition: stream pauses → user decides → stream resumes with modified context.
Files: ~ internal/app/stream.go
Snippet: // In handleStreamEvent, after tool_calls stop reason:\ntoolEntries := (&m).executeTools(m.stream.partialTools)\nif toolEntries == nil {\n\t// Authorization needed — pause the stream in ModeAuthorize\n\t(&m).setAuthMode()\n\tm.updateViewportContent()\n\treturn m, nil // no new command — waiting for user key\n}\n\n// setAuthMode switches to authorization mode\nfunc (m *Model) setAuthMode() {\n\tm.mode = ModeAuthorize\n\tm.stream.active = false // stop waiting for stream events\n\tm.textarea.Placeholder = "press y/n to proceed, tab for instructions..."\n}\n\n// handleAuthorizationResponse executes after user confirms/rejects\nfunc (m *Model) handleAuthorizationResponse(result AuthResult, instructions string) ([]config.ToolCallEntry, bool) {\n\tctx := m.stream.authorizationCtx\n\tidx := m.stream.pendingToolIndex\n\tpartials := m.stream.partialTools\n\tentries := make([]config.ToolCallEntry, len(partials))\n\n\tif result == AuthAccepted || result == AuthAcceptedWithInstructions {\n\t\t// Execute the pending tool\n\t\t// ... (single tool execution logic) ...\n\t\tentries[idx] = executedEntry\n\n\t\t// If instructions were given, cancel remaining tools and signal resume-with-injection\n\t\tif result == AuthAcceptedWithInstructions {\n\t\t\tfor j := idx + 1; j < len(partials); j++ {\n\t\t\t\tentries[j] = config.ToolCallEntry{\n\t\t\t\t\tID: partials[j].id,\n\t\t\t\t\tInstruction: struct{...}{Name: partials[j].name, Arguments: partials[j].args},\n\t\t\t\t\tExecution: struct{...}{\n\t\t\t\t\t\tStatus: ResultStatusError,\n\t\t\t\t\t\tError:  "cancelled: user provided instructions before this tool could execute",\n\t\t\t\t\t},\n\t\t\t\t}\n\t\t\t}\n\t\t\treturn entries, true // true = inject user message\n\t\t}\n\t\t// Continue to next tool (check if it also needs auth)\n\t\t// ... recursive or iterative check ...\n\t} else {\n\t\t// Rejected\n\t\tentries[idx] = config.ToolCallEntry{\n\t\t\t// ... with error "rejected by user" ...\n\t\t}\n\t\t// Cancel remaining\n\t\tfor j := idx + 1; j < len(partials); j++ {\n\t\t\t// ... cancelled ...\n\t\t}\n\t\treturn entries, result == AuthRejectedWithInstructions\n\t}\n}
Acceptance: Accepted tools execute, rejected tools return error, instructions trigger synthetic message injection, remaining tools are cancelled
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.5 - Inject synthetic user message on instruction-attached authorization
Layer: Application
What: When authorization resolves with instructions, append a synthetic user message to the session containing the user's instructions, then resume streaming via startStream().
Why: Breaks the assistant turn with user context, allowing the LLM to adjust its behavior before continuing.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) injectAndResume(instructions string) (tea.Model, tea.Cmd) {\n\t// Append user message with instructions\n\tuserMsg := config.Message{\n\t\tID:        fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),\n\t\tRole:      config.RoleUser,\n\t\tCreatedAt: time.Now(),\n\t\tText:      instructions,\n\t\tInputTokens: countTokensApprox(instructions),\n\t}\n\tm.session.appendMsg(userMsg)\n\n\t// Reset authorization state\n\tm.stream.authorizationCtx = nil\n\tm.stream.pendingToolIndex = -1\n\n\t// Resume stream\n\treturn (&m).startStream()\n}
Acceptance: User message appears in session, stream resumes with the new message in context
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 4 - Authorization UI
Pattern: Overlay Mode / State Machine
Objective: Build the authorization prompt that appears in the status bar area: yes/no selection with arrow keys, tab to switch to instruction input, shift+tab to return to yes/no, enter to submit.
Success: The authorization overlay renders in the status line area. User can navigate between yes/no, switch to text input, and submit. All keybindings work correctly and transition back to streaming or chat mode.
Diagram: ModeAuthorize → render overlay (status line): [←] Yes / No [→] ←→ arrows, tab for text, enter to submit → user key event → if text mode: textarea input → if yes/no mode: arrow selection → enter: resolve authorization → resume stream or chat

### TASK: 4.1 - Add ModeAuthorize to modes
Layer: Interface
What: Add ModeAuthorize to the Mode enum in internal/app/modes.go and its String() method.
Why: Needed for the routing in Update/View to handle the authorization overlay as a distinct mode.
Files: ~ internal/app/modes.go
Snippet: const (\n\tModeChat          Mode = iota\n\tModeStreaming\n\tModeModelPicker\n\tModeHelp\n\tModeFilePicker\n\tModeSessionPicker\n\tModeSavePrompt\n\tModeHistorySearch\n\tModeAuthorize   // awaiting user authorization for tool execution\n)\n\nfunc (m Mode) String() string {\n\tswitch m {\n\t// ...\n\tcase ModeAuthorize:\n\t\treturn "authorize"\n\t// ...\n\t}
Acceptance: ModeAuthorize compiles, String() returns "authorize"
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 4.2 - Build authorization prompt component
Layer: Interface
What: Create internal/ui/authorization.go with an AuthorizationPrompt struct that renders the yes/no prompt in the status bar area. Tracks selection (yes=0, no=1) and text mode (false=yes/no, true=text input).
Why: Encapsulates the authorization UI: the prompt line showing tool name + args, the yes/no selection, and the text input for instructions.
Files: + internal/ui/authorization.go
Snippet: package ui\n\nimport (\n\t"fmt"\n\t"github.com/charmbracelet/lipgloss"\n\t"squid-os/internal/style"\n)\n\ntype AuthorizationPrompt struct {\n\tToolName       string\n\tArgsJSON       string\n\tDisplayValue   string\n\tIsDestructive  bool\n\tSelection      int // 0=yes, 1=no\n\tTextMode       bool // true = typing instructions\n\tTextInput      string\n\tWidth          int\n}\n\nfunc (p AuthorizationPrompt) Render() string {\n\twidth := p.Width\n\tif width == 0 {\n\t\twidth = 80\n\t}\n\n\t// Build prompt line\n\tlabel := p.ToolName\n\tif p.DisplayValue != "" {\n\t\tlabel = fmt.Sprintf("%s(%s)", p.ToolName, truncate(p.DisplayValue, 40))\n\t}\n\n\tvar destructiveIcon string\n\tif p.IsDestructive {\n\t\tdestructiveIcon = "⚠ "\n\t}\n\n\tvar prompt string\n\tif p.TextMode {\n\t\tprompt = fmt.Sprintf("%sProceed with %s? [%s] Type instructions...", \n\t\t\tdestructiveIcon, label, \n\t\t\tp.selectedLabel())\n\t} else {\n\t\tprompt = fmt.Sprintf("%sProceed with %s? [%s]es / [N]o / [T]ab for instructions",\n\t\t\tdestructiveIcon, label, p.selectedLabel())\n\t}\n\n\t// Style: highlight selected option\n\tyesStyle := style.SelectionStyle.Render("Yes")\n\tnoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No")\n\tif p.Selection == 0 {\n\t\tyesStyle = style.SelectionStyle.Render("Yes")\n\t} else {\n\t\tnoStyle = style.SelectionStyle.Render("No")\n\t}\n\n\treturn style.StatusLineStyle.Width(width).Render(fmt.Sprintf(\n\t\t"  %s  %s / %s  %s",\n\t\tlabel,\n\t\tyesStyle,\n\t\tnoStyle,\n\t\tpromptSuffix(p.TextMode),\n\t))\n}\n\nfunc (p AuthorizationPrompt) selectedLabel() string {\n\tif p.Selection == 0 {\n\t\treturn "Y"\n\t}\n\treturn "N"\n}\n\nfunc (p AuthorizationPrompt) promptSuffix(textMode bool) string {\n\tif textMode {\n\t\treturn "← enter to submit, shift+tab to review"\n\t}\n\treturn "←→ select · tab for instructions · enter to confirm"\n}
Acceptance: Component renders correctly, shows tool name, yes/no selection, and instruction hint
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 4.3 - Add authorization prompt field to Model
Layer: Interface
What: Add authPrompt ui.AuthorizationPrompt to the Model struct in app.go. Populate it from authorizationCtx when entering ModeAuthorize.
Why: The Model needs to hold the authorization prompt state so it can render and handle input.
Files: ~ internal/app/app.go
Snippet: // In Model struct:\n\tauthPrompt ui.AuthorizationPrompt // authorization prompt state\n\n// In setAuthMode():\nfunc (m *Model) setAuthMode() {\n\tm.mode = ModeAuthorize\n\tm.stream.active = false\n\tctx := m.stream.authorizationCtx\n\tm.authPrompt = ui.AuthorizationPrompt{\n\t\tToolName:      ctx.ToolName,\n\t\tArgsJSON:      ctx.ArgsJSON,\n\t\tDisplayValue:  ctx.DisplayValue,\n\t\tIsDestructive: ctx.IsDestructive,\n\t\tSelection:     0, // default to Yes\n\t\tWidth:         m.width,\n\t}\n}
Acceptance: authPrompt is populated from authorizationCtx, setAuthMode initializes it
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 4.4 - Render authorization overlay in View
Layer: Interface
What: In render.go View(), add a case for ModeAuthorize that renders authPrompt instead of the textarea, replacing the status line area.
Why: The authorization prompt replaces the input area during authorization, making it the primary interaction point.
Files: ~ internal/app/render.go
Snippet: // In View(), after the mode switch for overlays:\ncase ModeAuthorize:\n\tsections = append(sections, m.authPrompt.Render())\n\n// Skip textarea in auth mode\nif m.mode != ModeAuthorize {\n\tsections = append(sections, m.textarea.View())\n}
Acceptance: In ModeAuthorize, the authorization prompt replaces the textarea in the layout
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 4.5 - Handle authorization key input
Layer: Interface
What: Add handleAuthorizeKey to input.go: Left/Right arrows toggle Yes/No, Tab switches to text mode, Shift+Tab returns to yes/no, Enter submits. In text mode: normal text input, Enter submits with text, Shift+Tab returns to yes/no.
Why: Routes authorization key events to the correct action: selecting yes/no, switching to text, or submitting.
Files: ~ internal/app/input.go
Snippet: func (m Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {\n\tvar result app.AuthResult\n\n\tswitch {\n\tcase key.Matches(msg, keys.Left):\n\t\tif !m.authPrompt.TextMode {\n\t\t\tm.authPrompt.Selection = 0 // Yes\n\t\t\tm.updateViewportContent()\n\t\t\treturn m, nil\n\t\t}\n\tcase key.Matches(msg, keys.Right):\n\t\tif !m.authPrompt.TextMode {\n\t\t\tm.authPrompt.Selection = 1 // No\n\t\t\tm.updateViewportContent()\n\t\t\treturn m, nil\n\t\t}\n\n\tcase msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):\n\t\t// Tab → switch to text mode\n\t\tm.authPrompt.TextMode = true\n\t\treturn m, nil\n\n\tcase msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):\n\t\t// Shift+Tab → return to yes/no mode\n\t\tm.authPrompt.TextMode = false\n\t\treturn m, nil\n\n\tcase key.Matches(msg, keys.Send):\n\t\tif m.authPrompt.TextMode {\n\t\t\t// Submit with text\n\t\t\ttext := m.authPrompt.TextInput\n\t\t\tif m.authPrompt.Selection == 0 {\n\t\t\t\tresult = app.AuthAcceptedWithInstructions\n\t\t\t} else {\n\t\t\t\tresult = app.AuthRejectedWithInstructions\n\t\t\t}\n\t\t\treturn m.resolveAuthorization(result, text)\n\t\t} else {\n\t\t\t// Submit yes/no without text\n\t\t\tif m.authPrompt.Selection == 0 {\n\t\t\t\tresult = app.AuthAccepted\n\t\t\t} else {\n\t\t\t\tresult = app.AuthRejected\n\t\t\t}\n\t\t\treturn m.resolveAuthorization(result, "")\n\t\t}\n\n\tdefault:\n\t\tif m.authPrompt.TextMode && msg.Type == tea.KeyRunes {\n\t\t\tm.authPrompt.TextInput += string(msg.Runes)\n\t\t\treturn m, nil\n\t\t} else if m.authPrompt.TextMode && msg.Type == tea.KeyBackspace {\n\t\t\ttext := m.authPrompt.TextInput\n\t\t\tif len(text) > 0 {\n\t\t\t\trunes := []rune(text)\n\t\t\t\tm.authPrompt.TextInput = string(runes[:len(runes)-1])\n\t\t\t}\n\t\t\treturn m, nil\n\t\t}\n\t}\n\treturn m, nil\n}
Acceptance: Left/Right toggles selection, Tab enters text mode, Shift+Tab exits text mode, Enter resolves. Text input works in text mode.
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 4.6 - Wire authorization mode in Update dispatch
Layer: Interface
What: In update.go handleKey, add case for ModeAuthorize → handleAuthorizeKey. In handleStreamEvent, when executeTools returns nil, transition to ModeAuthorize.
Why: Wires the authorization mode into the main event loop so it can intercept and respond to key events.
Files: ~ internal/app/update.go
Snippet: func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {\n\tswitch m.mode {\n\t// ...\n\tcase ModeAuthorize:\n\t\treturn m.handleAuthorizeKey(msg)\n\t// ...\n\t}
Acceptance: Authorization mode is dispatched in handleKey, key events reach handleAuthorizeKey
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 5 - System Prompt Update
Pattern: Instruction Injection
Objective: Update the system prompt so the LLM knows it must set the destructive field on every bash call, and understands the semantics of what makes a command destructive.
Success: System prompt clearly defines destructive vs read-only bash commands. LLM consistently sets the correct value.
Diagram: sys-prompt.go → DefaultAssistantPrompt() includes new bash destructive field instructions → injected into every API request as system message

### TASK: 5.1 - Update system prompt for destructive field
Layer: Infrastructure
What: Add a new section to the default system prompt in internal/config/sys-prompt.go explaining the destructive field for bash: when to set true, when to set false, and that the field is required.
Why: The LLM needs explicit instructions to populate the required destructive field correctly on every bash call.
Files: ~ internal/config/sys-prompt.go
Snippet: ## Bash Tool\n- The bash tool requires a "destructive" boolean parameter on every call.\n- Set destructive: true if the command modifies files, deletes data, creates directories, installs packages, or changes system state (e.g., rm, mv, cp, mkdir, chmod, apt-get, pip install, git commit, sed -i).\n- Set destructive: false for read-only commands (e.g., cat, ls, grep, find, git status, git diff, wc, head, tail, df, ps, curl GET).\n- Omitting the destructive field will result in an error.
Acceptance: System prompt contains clear destructive field instructions for bash, examples of true and false cases
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 6 - Resolve Authorization Response
Pattern: State Machine / Post-Action Routing
Objective: After the user responds to an authorization prompt, execute or reject the tool, handle remaining pending calls, and route back to either continued tool authorization or stream resume.
Success: resolveAuthorization handles all four result types (Accepted, Rejected, AcceptedWithInstructions, RejectedWithInstructions). Pending tools are cancelled when instructions are injected. Stream resumes correctly with or without injected user message.
Diagram: handleAuthorizeKey Enter → resolveAuthorization(result, instructions) → if Accepted: execute tool → check next partial (needs auth? → ModeAuthorize again / no → continue) → if WithInstructions: cancel remaining, inject user msg, startStream → if Rejected: mark rejected, cancel remaining, resume stream

### TASK: 6.1 - Implement resolveAuthorization
Layer: Application
What: Add resolveAuthorization method to Model in stream.go. Handles the four auth results: execute+continue for Accepted, mark error for Rejected, execute+cancel+inject for AcceptedWithInstructions, mark error+cancel+inject for RejectedWithInstructions.
Why: This is the core post-authorization logic that determines what happens after the user responds — the branching point between continuing, cancelling, and injecting context.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) resolveAuthorization(result AuthResult, instructions string) (tea.Model, tea.Cmd) {\n\tctx := m.stream.authorizationCtx\n\tidx := m.stream.pendingToolIndex\n\tpartials := m.stream.partialTools\n\tentries := make([]config.ToolCallEntry, len(partials))\n\n\tfor i, p := range partials {\n\t\tentries[i] = config.ToolCallEntry{\n\t\t\tID: p.id, Type: p.typeStr,\n\t\t\tInstruction: struct{...}{Name: p.name, Arguments: p.args, Tokens: countTokensApprox(p.args)},\n\t\t}\n\t}\n\n\tswitch result {\n\tcase AuthAccepted:\n\t\tentries[idx] = m.executeSingleTool(partials[idx])\n\t\t// Save assistant message with this entry, then check next tools\n\t\treturn m.continueAfterAuth(entries, false, "")\n\n\tcase AuthRejected:\n\t\tentries[idx].Execution.Status = tools.ResultStatusError\n\t\tentries[idx].Execution.Error = "rejected by user — tool was not executed"\n\t\t// Cancel remaining\n\t\tfor j := idx + 1; j < len(partials); j++ {\n\t\t\tentries[j].Execution.Status = tools.ResultStatusError\n\t\t\tentries[j].Execution.Error = "cancelled: previous tool was rejected by user"\n\t\t}\n\t\treturn m.continueAfterAuth(entries, false, "")\n\n\tcase AuthAcceptedWithInstructions:\n\t\tentries[idx] = m.executeSingleTool(partials[idx])\n\t\tfor j := idx + 1; j < len(partials); j++ {\n\t\t\tentries[j].Execution.Status = tools.ResultStatusError\n\t\t\tentries[j].Execution.Error = "cancelled: user provided instructions before this tool could execute"\n\t\t}\n\t\treturn m.continueAfterAuth(entries, true, instructions)\n\n\tcase AuthRejectedWithInstructions:\n\t\tentries[idx].Execution.Status = tools.ResultStatusError\n\t\tentries[idx].Execution.Error = "rejected by user — tool was not executed"\n\t\tfor j := idx + 1; j < len(partials); j++ {\n\t\t\tentries[j].Execution.Status = tools.ResultStatusError\n\t\t\tentries[j].Execution.Error = "cancelled: user provided instructions"\n\t\t}\n\t\treturn m.continueAfterAuth(entries, true, instructions)\n\t}\n\treturn m, nil\n}
Acceptance: All four auth results are handled correctly. Single tool executes on accept, error on reject. Remaining tools cancelled on instructions. Stream resumes or user message injected.
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 6.2 - Implement executeSingleTool and continueAfterAuth
Layer: Application
What: Add executeSingleTool (extracted from old executeTools batch logic, for one partial) and continueAfterAuth (saves the assistant message with all entries, injects user message if needed, and either resumes stream or checks next tool for authorization).
Why: Factored from executeTools so a single tool can be executed outside the batch, and the post-authorization flow can decide whether to resume streaming or continue checking more tools.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) executeSingleTool(p partialTool) config.ToolCallEntry {\n\ttool := m.toolReg.Get(p.name)\n\tvar args map[string]interface{}\n\tif p.args != "" {\n\t\t_ = json.Unmarshal([]byte(p.args), &args)\n\t}\n\t// ... checksum validation from tracker ...\n\tresultStart := time.Now()\n\tresult := tool.Execute(args)\n\tentry := config.ToolCallEntry{\n\t\tID: p.id, Type: p.typeStr,\n\t\tInstruction: struct{...}{Name: p.name, Arguments: p.args, Tokens: countTokensApprox(p.args)},\n\t\tExecution: struct{...}{\n\t\t\tStatus: result.Status, Result: result.Result, Error: result.Error,\n\t\t\tTokens: countTokensApprox(result.Result),\n\t\t\tDurationMs: time.Since(resultStart).Milliseconds(),\n\t\t\tFiles: result.Files,\n\t\t},\n\t}\n\t// Handle set_working_dir side effect\n\tif p.name == "set_working_dir" && result.Status == tools.ResultStatusSuccess {\n\t\tif pathVal, ok := args["path"].(string); ok {\n\t\t\tm.applyWorkingDir(pathVal)\n\t\t}\n\t}\n\treturn entry\n}\n\nfunc (m *Model) continueAfterAuth(entries []config.ToolCallEntry, injectUserMsg bool, instructions string) (tea.Model, tea.Cmd) {\n\t// Check if there are more tools that need authorization\n\tif !injectUserMsg {\n\t\t// Check remaining tools after the one we just handled\n\t\tnextIdx := m.stream.pendingToolIndex + 1\n\t\tif nextIdx < len(m.stream.partialTools) {\n\t\t\tremaining := m.stream.partialTools[nextIdx:]\n\t\t\t// Check if any remaining unexecuted tool needs auth\n\t\t\t// ... if yes, set authorizationCtx for next, stay in ModeAuthorize\n\t\t\t// ... if no, execute remaining + merge with entries, save assistant msg, resume stream\n\t\t}\n\t}\n\n\t// Save assistant message with all tool entries\n\t(&m).appendAssistantMsg(config.Message{\n\t\tID: fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),\n\t\tRole: config.RoleAssistant,\n\t\t// ... metrics from stream ...\n\t\tToolCalls: entries,\n\t\tStopReason: "tool_calls",\n\t})\n\n\tm.stream.authorizationCtx = nil\n\tm.stream.pendingToolIndex = -1\n\n\tif injectUserMsg && instructions != "" {\n\t\treturn m.injectAndResume(instructions)\n\t}\n\treturn (&m).startStream()\n}
Acceptance: Single tool execution mirrors batch behavior. continueAfterAuth saves message, handles injection, and resumes stream. Remaining tools are checked for authorization.
Verification: cd /home/goglue/src/squid-os && go build ./...

# EPIC: Tool Authorization Modes
Why: Users need control over what the AI can modify on their filesystem. Different workflows demand different trust levels — from fully automated to fully supervised.
Outcomes: Three authorization modes (auto, ask-on-write, ask-for-all), destructive field enforcement on bash, interactive yes/no confirmation with optional inline instructions, and cancellation of pending tool calls when the user interrupts.

## MILESTONE: 1 - Domain
Pattern: Value Object
Objective: Define authorization modes and destructive classification types
Success: Settings persists authorization mode, config exposes AuthorizeMode type
Diagram: Settings.AuthorizeMode (enum: auto | ask-on-write | ask-for-all) -> loaded at startup -> drives all tool execution decisions

### TASK: 1.1 - Add AuthorizeMode to settings
What: Add AuthorizeMode string type and field to config.Settings, with constants for auto, ask-on-write, ask-for-all. Update DefaultSettings and validation in LoadSettings.
Why: The user needs a configurable setting to select the authorization policy. Defaults to auto (current behavior) for backward compatibility.
Files: ~ internal/config/settings.go
Snippet: const (\n\tAuthorizationAuto      = "auto"\n\tAuthorizationAskOnWrite = "ask-on-write"\n\tAuthorizationAskForAll  = "ask-for-all"\n)\n\ntype AuthorizeMode string\n\nfunc (a AuthorizeMode) IsValid() bool {\n\tswitch a {\n\tcase AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll:\n\t\treturn true\n\t}\n\treturn false\n}
Snippet: Settings struct gains:\n\tAuthorization AuthorizeMode \x60json:"authorization"\x60
Acceptance: Default is auto (backward compatible)
Acceptance: LoadSettings rejects invalid values, falls back to auto
Acceptance: SaveSettings round-trips the field
Verification: go build ./...
Verification: cat ~/.config/squid-os/settings.json | grep authorization

## MILESTONE: 2 - Infrastructure
Pattern: Port/Adapter
Objective: Bash tool declares destructive intent, classification logic for all tools
Success: Bash schema requires destructive bool, each tool is classifiable as destructive or not
Diagram: Tool Schema.destructive (bash only) -> IsDestructive(tool, args) bool -> drives authorization gate

### TASK: 2.1 - Add destructive to bash schema (required, no default)
What: Add the 'destructive' boolean field to the bash tool's JSON schema as a required field. Update the Execute function to return an error if 'destructive' is missing from args. Store the value in ToolResult for downstream consumption.
Why: The LLM must explicitly declare whether a bash command modifies state. No default — the tool fails if omitted, forcing the LLM to be deliberate.
Files: ~ internal/tools/bash.go
Snippet: Schema properties addition:\n"destructive": {\n  "type": "boolean",\n  "description": "true if the command modifies files, writes, deletes, or changes system state. false for read-only commands. REQUIRED - must be explicitly set."\n}\nRequired array addition: "destructive"
Snippet: In Execute:\n\ndestructive, ok := args["destructive"].(bool)\nif !ok {\n\treturn ToolResult{Status: ResultStatusError, Error: "destructive is required and must be a boolean"}\n}
Snippet: ToolResult gains a field to carry destructive classification (or check in caller from args)
Acceptance: bash without destructive arg returns error
Acceptance: bash with destructive: true/false parses and executes normally
Verification: go test ./internal/tools/...
Verification: go build ./...

### TASK: 2.2 - Add IsDestructive classification to Tool
What: Add IsDestructive bool field to the Tool struct. Set it on write_file, edit_file, skill_build (true) and read_file, bash, skill_list, open, set_working_dir (false - bash determined at runtime from args). Create IsDestructiveTool(tool, args) helper that checks both the static classification and, for bash, the runtime args[destructive] value.
Why: The authorization layer needs a single way to determine whether a specific tool call with specific args is destructive or not.
Files: ~ internal/tools/tools.go
Snippet: Tool struct gains:\n\tIsDestructive bool \x60json:"destructive"\x60
Snippet: var IsDestructiveTool = func(t *Tool, args map[string]interface{}) bool {\n\tif t.IsDestructive {\n\t\treturn true\n\t}\n\tif t.Name == "bash" {\n\t\tif d, ok := args["destructive"].(bool); ok {\n\t\t\treturn d\n\t\t}\n\t}\n\treturn false\n}
Acceptance: write_file, edit_file, skill_build always return true
Acceptance: bash with destructive:true returns true
Acceptance: bash with destructive:false returns false
Acceptance: read_file, skill_list, open, set_working_dir always return false
Verification: go build ./...

## MILESTONE: 3 - Application
Pattern: State Machine
Objective: Per-tool authorization gate with batch interruption and user-injected messages
Success: Stream pauses on authorization, user responds, pending tools cancelled with error, injected user message resumes stream
Diagram: stream → tool_calls → ForEach tool: shouldAsk?(mode, args) → yes: ModeAuthorize → user: y/n+text → execute or reject → pending tools: CANCELLED → resume stream with optional user msg

### TASK: 3.1 - Add ModeAuthorize to app modes
What: Add ModeAuthorize to the Mode enum in modes.go. This mode interrupts the streaming flow to present a yes/no confirmation. Add authorization state to streamState: pendingTool (index of tool being authorized), pendingInstruction (text typed by user before confirming), inInstructionMode (bool - whether user is typing text vs selecting y/n).
Why: We need a distinct UI mode that suspends streaming, shows the authorization prompt, and collects user intent before resuming.
Files: ~ internal/app/modes.go
Files: ~ internal/app/stream.go
Snippet: const (\n\t...\n\tModeAuthorize       // Authorization confirmation overlay\n)
Snippet: streamState additions:\n\tauthPendingIdx       int        // index of tool awaiting authorization (-1 = none)\n\tauthInInstruction    bool       // user is typing inline instructions\n\tauthInstructionText  string     // captured instruction text\n\tauthPendingCancelled []int      // indices of tools to mark CANCELLED
Acceptance: ModeAuthorize string returns "authorize"
Acceptance: streamState has new authorization fields
Verification: go build ./...

### TASK: 3.2 - Implement authorization gate in executeTools
What: Restructure executeTools to process tools one at a time when authorization is needed. Before each tool: check the authorization mode (auto/ask-on-write/ask-for-all) and IsDestructiveTool. If authorization is needed, save state into streamState.authPendingIdx, switch to ModeAuthorize, and RETURN from executeTools (stream is paused). On resume from ModeAuthorize (see Task 3.3), continue from the saved index. If user rejected with instructions, execute the tool (or mark rejected), inject user message, cancel remaining tools with status error 'CANCELLED due to user interruption', and return.
Why: The current batch execution model must change: we now gate per-tool, pause the assistant turn, let the user respond, then resume. This is the core behavioral change.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) shouldAuthorize(t *tools.Tool, args map[string]interface{}) bool {\n\tswitch m.settings.Authorization {\n\tcase config.AuthorizationAuto:\n\t\treturn false\n\tcase config.AuthorizationAskForAll:\n\t\treturn true\n\tcase config.AuthorizationAskOnWrite:\n\t\treturn tools.IsDestructiveTool(t, args)\n\t}\n\treturn false\n}
Snippet: In executeTools loop, before tool execution:\n\tif m.shouldAuthorize(tool, args) {\n\t\t// Set up authorization state and return\n\t\tm.stream.authPendingIdx = i\n\t\tm.stream.authInInstruction = false\n\t\tm.stream.authInstructionText = ""\n\t\treturn nil // signal to handleStreamEvent that we need auth\n\t}
Acceptance: In auto mode, all tools execute without interruption (current behavior)
Acceptance: In ask-on-write, only destructive tools trigger authorization
Acceptance: In ask-for-all, every tool triggers authorization
Acceptance: When executeTools returns early for auth, handleStreamEvent detects this and switches to ModeAuthorize
Verification: go build ./...

### TASK: 3.3 - Implement authorization response and batch cancellation
What: In handleStreamEvent, detect when executeTools returned nil (authorization pending). Display authorization prompt. On user response (y/n with optional instruction): 1) y without text: execute the tool normally, continue loop for next tool. 2) y with text: execute the tool, inject user message (synthetic role) into session, mark remaining tools as CANCELLED, resume stream. 3) n without text: set tool result to error 'REJECTED by user', mark remaining tools as CANCELLED, resume stream. 4) n with text: set tool result to 'REJECTED by user', inject user message, mark remaining tools as CANCELLED, resume stream. The injected user message breaks the assistant turn — the LLM continues from that point.
Why: The user needs to accept/reject with optional instructions that feed back into the conversation. Rejected/cancelled tools get clear error messages so the LLM knows what happened.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) handleAuthResponse(accepted bool, instruction string) []config.ToolCallEntry {\n\t// Re-execute from authPendingIdx with known outcome\n\tentries := make([]config.ToolCallEntry, len(m.stream.partialTools))\n\ttracker := tools.NewFileTracker()\n\t// Execute tools up to authPendingIdx (already done in previous call)\n\t// ... copy existing entries ...\n\t// Process the pending tool\n\tif accepted {\n\t\tresult := tool.Execute(args)\n\t\t// populate entry\n\t} else {\n\t\tentry.Execution.Status = tools.ResultStatusError\n\t\tentry.Execution.Error = "REJECTED by user"\n\t}\n\t// If instruction text exists, append synthetic user message\n\tif instruction != "" {\n\t\tm.session.appendMsg(config.Message{...Role: RoleSynthetic, Text: instruction, Label: "user instruction"})\n\t}\n\t// Cancel remaining tools\n\tfor j := authPendingIdx + 1; j < len(partialTools); j++ {\n\t\tentries[j].Execution.Status = tools.ResultStatusError\n\t\tentries[j].Execution.Error = "CANCELLED due to user interruption"\n\t}\n\treturn entries\n}
Acceptance: Accepted tool executes normally, next tool is considered
Acceptance: Accepted tool with instruction: tool executes, instruction injected, remaining tools cancelled, stream resumes with user message in context
Acceptance: Rejected tool: result is error, remaining tools cancelled, stream resumes
Acceptance: Rejected with instruction: error result + user message injected, remaining tools cancelled, stream resumes
Verification: go build ./...

## MILESTONE: 4 - Interface
Pattern: Presentation / Overlay
Objective: Authorization prompt UI: yes/no selection with arrow keys, tab for inline text, shift+tab to return
Success: User can see the tool prompt, press left/right to select yes/no, tab to type instructions, shift+tab to return, enter to submit
Diagram: ModeAuthorize → StatusBar: [tool(args)]?  ←[Yes] [No]→  → Tab: text input ← → Shift+Tab: back to selection → Enter: submit

### TASK: 4.1 - Add authorization prompt key handling
What: Add authorization key bindings and handler in keymap.go and input.go. Keys: left arrow (select no), right arrow (select yes), tab (enter instruction mode), shift+tab (return to y/n selection), enter (submit). In ModeAuthorize, if authInInstruction is true, handle text input. Otherwise, handle selection.
Why: The authorization prompt needs its own input handling separate from chat and streaming modes.
Files: ~ internal/app/keymap.go
Files: ~ internal/app/input.go
Snippet: keyMap additions:\n\tAuthYes        key.Binding  // right arrow\n\tAuthNo         key.Binding  // left arrow\n\tAuthAccept     key.Binding  // enter\n\tAuthInstruction key.Binding // tab
Snippet: handleAuthorizeKey(msg tea.KeyMsg) in input.go:\n\tswitch {\n\tcase key.Matches(msg, keys.AuthYes):\n\t\treturn m.submitAuthResponse(true)\n\tcase key.Matches(msg, keys.AuthNo):\n\t\treturn m.submitAuthResponse(false)\n\tcase key.Matches(msg, keys.Tab) && !m.stream.authInInstruction:\n\t\tm.stream.authInInstruction = true\n\t\tm.recalcLayout()\n\t\treturn m, nil\n\tcase msg.Shift && key.Matches(msg, keys.Tab) && m.stream.authInInstruction:\n\t\tm.stream.authInInstruction = false\n\t\treturn m, nil\n\tcase key.Matches(msg, keys.Send):\n\t\treturn m.submitAuthResponse(m.stream.authInstructionText != "")\n\t}
Acceptance: Left/right arrows toggle selection
Acceptance: Tab switches to text input
Acceptance: Shift+tab switches back to y/n
Acceptance: Enter submits with current selection + any text
Verification: go build ./...

### TASK: 4.2 - Build authorization prompt UI overlay
What: Build the authorization prompt component in the footer/status bar area. Display: the tool name and display args, yes/no indicators with current selection highlighted, and optionally a text input line. When authInInstruction is true, show a small textarea for typing instructions. Render via the existing footer or a dedicated overlay in render.go.
Why: Users need to see what tool is being authorized, what it will do, and have an intuitive way to accept/reject with optional notes.
Files: ~ internal/app/render.go
Files: ~ internal/ui/footer.go
Snippet: renderAuthorizePrompt(m Model) string:\n\ttool := m.toolReg.Get(p.name)\n\tdisplay := tool.DisplayValue(p.args)\n\tlabel := fmt.Sprintf("⚠ Proceed with %s(%s)?", p.name, display)\n\tselection := "[Yes] [No]"  // or "[Yes] [No]" depending on state\n\tif m.stream.authInInstruction {\n\t\tlabel += "\n" + m.stream.authInstructionText + "_"\n\t}\n\treturn label
Acceptance: Prompt shows tool name and display param
Acceptance: Yes/No selection is visually clear
Acceptance: Text instruction mode shows a cursor for input
Verification: go build ./...
Verification: visual inspection in TUI

### TASK: 4.3 - Update system prompt for destructive requirement
What: Update the system prompt to instruct the LLM that the bash tool requires the 'destructive' boolean field. Explain that true means the command modifies files/state, false means read-only. Emphasize this is mandatory — omitting it causes an error.
Why: The LLM needs to know about the new required field so it includes it in every bash call.
Files: ~ internal/config/sys-prompt.go
Snippet: System prompt addition:\n"When calling the bash tool, you MUST set the 'destructive' field:\n- destructive: true if the command modifies files, writes, deletes, installs, or changes system state\n- destructive: false for read-only commands (cat, ls, find, grep, git status, etc.)\nOmitting this field will cause the tool call to fail."
Acceptance: System prompt mentions the destructive requirement
Verification: go build ./...

### TASK: 4.4 - Wire authorization pause/resume in handleStreamEvent
What: In handleStreamEvent, after executeTools is called, detect if it returned nil (authorization pending). If so: switch to ModeAuthorize, render the prompt, and wait for user input. Do NOT call startStream yet. On resume from ModeAuthorize, complete the tool batch (with accepted/rejected/cancelled results), append the assistant message, and call startStream to resume.
Why: handleStreamEvent currently assumes executeTools always returns a complete batch. We need to handle the pause-and-resume flow: partial execution → auth prompt → completion.
Files: ~ internal/app/stream.go
Snippet: In handleStreamEvent, tool_calls branch:\n\ttoolEntries := (&m).executeTools(m.stream.partialTools)\n\tif toolEntries == nil {\n\t\t// Authorization pending — switch mode and wait\n\t\tm.mode = ModeAuthorize\n\t\tm.updateViewportContent()\n\t\treturn m, nil\n\t}\n\t// Normal flow: append and resume...
Acceptance: When executeTools returns nil, mode switches to ModeAuthorize
Acceptance: Viewport updates to show authorization prompt
Acceptance: No stream resumption until auth is resolved
Verification: go build ./...

### TASK: 4.5 - Implement instruction text editing in authorization prompt
What: When in instruction mode (after Tab), capture typed characters into authInstructionText. Support backspace, left/right navigation. Show the accumulating text in the status bar alongside the yes/no selection. When Shift+Tab is pressed, preserve the text and return to yes/no view so the user can review before submitting.
Why: Users need to write clear instructions (e.g., 'don't delete the config file') alongside their yes/no decision. The back-and-forth between text editing and review should be fluid.
Files: ~ internal/app/input.go
Snippet: In handleAuthorizeKey, when authInInstruction:\n\tcase msg.Type == tea.KeyRunes:\n\t\tm.stream.authInstructionText += string(msg.Runes)\n\tcase msg.Type == tea.KeyBackspace:\n\t\trunes := []rune(m.stream.authInstructionText)\n\t\tif len(runes) > 0 {\n\t\t\tm.stream.authInstructionText = string(runes[:len(runes)-1])\n\t\t}\n\treturn m, nil
Snippet: Footer rendering shows:\n\t- Selection: [Yes] [No]\n\t- Instruction text: "user typed text here|" (with cursor)
Acceptance: Typing characters appends to instruction
Acceptance: Backspace removes last character
Acceptance: Shift+tab preserves text, returns to selection view
Acceptance: Enter in instruction mode submits with current selection + text
Verification: go build ./...

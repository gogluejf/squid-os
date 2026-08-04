# EPIC: Agent Sub-session: Multi-context streaming with child agents
Why: The current app supports only a single chat session with a single stream. Models need to delegate work to sub-agents with their own context, tools, and skills — spawning child sessions that the user can toggle between while the parent tool call blocks awaiting results.
Outcomes: agent_call tool spawns isolated child sessions with full tool support, user toggles between parent and child views, child file state is isolated, headless mode reuses the same stream engine

## MILESTONE: 1 - Session Architecture
Pattern: Entity-Context, State Migration
Objective: Move session ownership and stream state from Model into chatSession so each session is independently streamable, then add multi-session management to Model.
Success: Model manages multiple chatSession instances keyed by ID, each with its own streamState, active session drives the viewport, single session path is backward compatible.
Diagram: Model { sessions map[ID]*chatSession, activeSessionID, textarea, viewport, mode }. chatSession { file config.SessionFile, stream streamState, renderedMessages, undoStack, parentID }. Mode derived from activeSession.stream.active + global overlays.

### TASK: 1.1 - Move streamState into chatSession
Layer: Domain
What: Move streamState from Model into chatSession struct. Add ParentID and ToolCallID fields to chatSession for child session tracking.
Why: Each session needs its own independent stream state so multiple sessions can stream concurrently. Parent/child linkage enables the agent_call tool to track ownership.
Files: ~ internal/app/chat_session.go
Snippet: type chatSession struct {\n    file             config.SessionFile\n    renderedMessages []string // glamour cache, 1:1 with file.Messages\n    renderedWidth    int\n    undoStack        [][]config.Message\n    stream           streamState  // moved from Model — each session has its own\n    ParentID         string       // parent session ID (empty string for root sessions)\n    ToolCallID       string       // originating tool call ID from parent (empty for root)\n}
Acceptance: chatSession compiles with embedded streamState
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.2 - Replace Model.session with session map
Layer: Application
What: Replace single chatSession field on Model with map[string]*chatSession and activeSessionID string. Add helper methods activeSession() and setActiveSession().
Why: Model must manage multiple sessions for parent-child toggling with O(1) lookup.
Files: ~ internal/app/app.go
Snippet: // On Model — replace: session chatSession\nSessions        map[string]*chatSession\nActiveSessionID string\n\nfunc (m *Model) activeSession() *chatSession {\n    if s, ok := m.Sessions[m.ActiveSessionID]; ok {\n        return s\n    }\n    return nil\n}\n\nfunc (m *Model) addSession(cs *chatSession) {\n    if m.Sessions == nil {\n        m.Sessions = make(map[string]*chatSession)\n    }\n    m.Sessions[cs.file.Session.ID] = cs\n}
Snippet: // In New() — initialize with single session:\nSessions:        map[string]*chatSession{sess.file.Session.ID: &sess},\nActiveSessionID: sess.file.Session.ID,
Acceptance: Model compiles with sessions map, activeSession() returns correct session
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.3 - Update all Model methods to use activeSession()
Layer: Application
What: Rewrite every reference to m.session and m.stream across all app files to use m.activeSession() and m.activeSession().stream.
Why: After the structural change all existing code must route through the active session.
Files: ~ internal/app/stream.go
Files: ~ internal/app/session.go
Files: ~ internal/app/render.go
Files: ~ internal/app/input.go
Snippet: // Systematic replacements across stream.go, session.go, render.go, input.go:\n// m.session.xxx          -> m.activeSession().xxx\n// m.stream.xxx           -> m.activeSession().stream.xxx\n// m.session.file.Messages -> m.activeSession().file.Messages
Snippet: // Example from sendMessage():\ncs := m.activeSession()\ncs.appendMsg(userMsg)\ncs.undoStack = nil\napiMsgs := chat.BuildAPIMessages(m.paths, m.settings, cs.file.Messages)
Snippet: // Example from handleStreamEvent():\ncs := m.activeSession()\nif event.Done {\n    if cs.stream.userCancelled { ... }\n}
Acceptance: All references compile via activeSession(), app runs identically
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.4 - Derive Model.mode from active session state
Layer: Application
What: Derive m.mode from activeSession().stream.active combined with overlay modes. Update setStreamMode() and setChatMode() for active session.
Why: With multiple sessions mode depends on which session is active. Overlay modes remain global.
Files: ~ internal/app/stream.go
Files: ~ internal/app/modes.go
Snippet: // setStreamMode() and setChatMode() operate on active session stream only\nfunc (m *Model) setStreamMode() {\n    cs := m.activeSession()\n    cs.stream.reset()\n    cs.stream.active = true\n    cs.stream.metrics.Start = time.Now()\n    m.mode = ModeStreaming\n    m.textarea.Placeholder = "ctrl+c to cancel..."\n}\n\nfunc (m *Model) setChatMode() tea.Cmd {\n    cs := m.activeSession()\n    cs.stream.active = false\n    m.textarea.Placeholder = "Type a message..."\n    m.mode = ModeChat\n    m.textarea.Focus()\n    m.recalcLayout()\n    return textarea.Blink\n}
Snippet: // computeMode() derives effective mode from overlays + active session\nfunc (m *Model) computeMode() Mode {\n    switch {\n    case m.mode == ModeHelp:\n        return ModeHelp\n    case m.mode == ModeModelPicker:\n        return ModeModelPicker\n    case m.mode == ModeSessionPicker:\n        return ModeSessionPicker\n    case m.mode == ModeFilePicker:\n        return ModeFilePicker\n    case m.mode == ModeSavePrompt:\n        return ModeSavePrompt\n    case m.mode == ModeHistorySearch:\n        return ModeHistorySearch\n    case m.cmdPalette.Visible:\n        return ModeChat\n    }\n    if cs := m.activeSession(); cs != nil && cs.stream.active {\n        return ModeStreaming\n    }\n    return ModeChat\n}
Acceptance: Mode reflects active session streaming state correctly
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 2 - agent_call Tool
Pattern: Command, Synchronous Delegation
Objective: Implement the agent_call tool that spawns child sessions, runs them to completion (blocking the tool executor), and returns the result to the parent.
Success: Model can call agent_call with prompt/tools/skills/skill_default, child session runs with full tool loop, file state isolated, result flows back as tool execution output.
Diagram: executeTools() detects agent_call → spawnChildSession() → runStreamLoop(child) [direct channel loop, no Bubble Tea] → child accumulates messages + tool results → loop returns final text → set Execution.Result on parent tool entry.

### TASK: 2.1 - Define agent_call tool schema
Layer: Domain
What: Create agent_call Tool struct with schema: prompt (string), tools (array of strings), skills (array of strings), skill_default (string). Register it in the tools init list.
Why: The model needs a callable tool definition to spawn sub-agents with configurable tool and skill access.
Files: ~ internal/tools/tools.go
Snippet: var AgentCall = Tool{\n    Name:         "agent_call",\n    Description:  "Spawn a child agent session with its own context window, tools, and skills. " +\n        "The child runs to completion and returns its final output. " +\n        "Use for focused sub-tasks that need their own context.",\n    Schema: []byte(`{\n        "type": "object",\n        "properties": {\n            "prompt": {"type": "string", "description": "The prompt/task for the sub-agent"},\n            "tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names the child can use (e.g. read_file, bash). Empty = no tools."},\n            "skills": {"type": "array", "items": {"type": "string"}, "description": "Skill names available to the child agent."},\n            "skill_default": {"type": "string", "description": "Default skill to auto-load into child session on init."}\n        },\n        "required": ["prompt"]\n    }`),\n    Execute: func(args map[string]interface{}) ToolResult {\n        // Handled specially in executeTools — not here\n        return ToolResult{Status: ResultStatusError, Error: "agent_call is processed inline in executeTools"}\n    },\n}
Acceptance: agent_call appears in tools.GetTools(), schema is valid JSON
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.2 - Implement spawnChildSession and runStreamLoop
Layer: Application
What: Create spawnChildSession() that initializes a child chatSession with ParentID, system prompt, environment, filtered tools, and skill_default. Create runStreamLoop(*chatSession) that drives engine.Stream directly, handles tool_calls restart, and returns final output.
Why: Child sessions run inside executeTools which blocks Bubble Tea — they need a direct channel loop. spawnChildSession sets up isolation, runStreamLoop drives the full cycle.
Files: ~ internal/app/stream.go
Files: ~ internal/app/chat_session.go
Snippet: // spawnChildSession creates and initializes a child session\nfunc (m *Model) spawnChildSession(parentID string, prompt string,\n    toolNames []string, skillNames []string, skillDefault string) *chatSession {\n    cs := &chatSession{\n        ParentID: parentID,\n    }\n    cs.file = config.NewSessionFile(m.settings.Provider, m.settings.Model,\n        m.settings.Thinking, m.settings.SystemPromptFile, m.workingDir)\n\n    // Push system prompt + environment messages (reuse clear() logic)\n    // Inject skill_default if set: load skill and push as system/internal msg\n    // Append prompt as user message\n    cs.appendMsg(config.Message{\n        ID:   "msg_1",\n        Role: config.RoleUser,\n        Text: prompt,\n    })\n\n    // Filter tools to allowed list\n    cs.allowedTools = filterTools(tools.GetTools(), toolNames)\n\n    m.addSession(cs)\n    return cs\n}
Snippet: // runStreamLoop drives a session stream to completion via direct channel loop\n// Used by both child agents (inside executeTools) and headless mode.\nfunc (m *Model) runStreamLoop(cs *chatSession) string {\n    var result strings.Builder\n\n    for {\n        apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, cs.file.Messages)\n        engine := chat.NewEngine(\n            config.ResolveChatURL(m.endpoints, m.settings.Provider),\n            m.settings.Model, m.settings.Thinking,\n        )\n\n        ctx, cancel := context.WithCancel(context.Background())\n        ch := engine.Stream(ctx, apiMsgs, cs.allowedTools)\n\n        for event := range ch {\n            cancel()\n            if event.Error != nil {\n                result.WriteString("[agent error: " + event.Error.Error() + "]")\n                return result.String()\n            }\n            if event.Done {\n                if event.StopReason == "tool_calls" && len(cs.stream.partialTools) > 0 {\n                    // Execute tools on this child, append assistant msg, outer loop restarts stream\n                    toolEntries := m.executeToolsFor(cs, cs.stream.partialTools)\n                    m.appendAssistantMsgFor(cs, toolEntries)\n                    cs.stream.reset()\n                    continue OUTER // restart stream with tool results in history\n                }\n                // Normal completion — save final assistant msg\n                m.appendAssistantMsgFor(cs, nil)\n                return result.String()\n            }\n            if event.Text != "" {\n                result.WriteString(event.Text)\n                cs.stream.AddTextChunk(event.Text)\n            }\n            if event.Thinking != "" {\n                cs.stream.AddThinkChunk(event.Thinking)\n            }\n            if event.ToolCallDelta != "" {\n                // Accumulate partial tool state (same logic as TUI handleStreamEvent)\n                accumulateToolCallDelta(&cs.stream, event)\n            }\n        }\n    }\n}
Acceptance: spawnChildSession creates valid session, runStreamLoop completes full stream cycle with tool support
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.3 - Implement agent_call Execute in executeTools
Layer: Application
What: In executeTools, detect agent_call by name. Parse args, call spawnChildSession and runStreamLoop sequentially for each agent. Collect results and return as Execution.Result. Save child session to disk after completion.
Why: This bridges the tool framework with the new session architecture — the integration point where parent tool execution spawns child agents.
Files: ~ internal/app/stream.go
Snippet: // In executeTools() — special case for agent_call\nif p.name == "agent_call" {\n    var argsStruct struct {\n        Prompt       string   `json:"prompt"`\n        Tools        []string `json:"tools"`\n        Skills       []string `json:"skills"`\n        SkillDefault string   `json:"skill_default"`\n    }\n    json.Unmarshal([]byte(p.args), &argsStruct)\n\n    // Spawn child session and run to completion (blocking)\n    child := m.spawnChildSession(m.ActiveSessionID, argsStruct.Prompt,\n        argsStruct.Tools, argsStruct.Skills, argsStruct.SkillDefault)\n\n    // Tag the child with the originating tool call ID\n    child.ToolCallID = p.id\n\n    result := m.runStreamLoop(child)\n\n    // Save child session to subfolder under parent\n    parentName := m.activeSession().file.Session.Title\n    if parentName == "" {\n        parentName = "unsaved"\n    }\n    _ = config.SaveChildSession(m.paths, parentName,\n        child.file.Session.ID, child.file)\n\n    entries[i].Execution.Status = tools.ResultStatusSuccess\n    entries[i].Execution.Result = result\n    continue // skip normal tool execution path\n}
Acceptance: agent_call spawns child, runs to completion, returns result, child is saved
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 3 - Multi-session TUI
Pattern: Viewport Multiplexing, Key-driven Navigation
Objective: Enable the user to toggle between parent and child session views, render the active session's viewport, and derive mode from the active session state.
Success: ctrl+tab cycles between active session and its children, viewport shows correct session, mode reflects active session streaming state, textarea binds to active session.
Diagram: handleKey() routes session-switch key → rotate activeSessionID through parent+children → updateViewportContent() uses active session → mode = activeSession.stream.active ? ModeStreaming : ModeChat → textarea binds to active session.

### TASK: 3.1 - Add session switch keybinding
Layer: Interface
What: Add ctrl+tab keybinding that cycles activeSessionID between parent and its children. Add session indicator to header showing current context (Parent vs Agent).
Why: User needs to toggle between parent and child views to monitor agent progress.
Files: ~ internal/app/keymap.go
Files: ~ internal/app/input.go
Files: ~ internal/app/render.go
Snippet: // New key binding in keymap.go\nSwitchSession: key.NewBinding(\n    key.WithKeys("ctrl+tab"),\n    key.WithHelp("ctrl+tab", "switch session"),\n),\n\n// In handleChatKey():\ncase key.Matches(msg, keys.SwitchSession):\n    return m.switchSession(), nil\n\nfunc (m *Model) switchSession() tea.Model {\n    // Build ordered list: current active first, then children by spawn order\n    var ordered []string\n    for id, cs := range m.Sessions {\n        if cs.ParentID == m.ActiveSessionID || id == m.ActiveSessionID {\n            ordered = append(ordered, id)\n        }\n    }\n    if len(ordered) < 2 {\n        return m\n    }\n    // Rotate to next in cycle\n    for i, id := range ordered {\n        if id == m.ActiveSessionID {\n            m.ActiveSessionID = ordered[(i+1)%len(ordered)]\n            break\n        }\n    }\n    m.updateViewportContent()\n    m.recalcLayout()\n    return m\n}
Acceptance: ctrl+tab cycles sessions, viewport updates, header shows context
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.2 - Update viewport and footer to bind to active session
Layer: Interface
What: Rewrite updateViewportContent() to render activeSession. Update buildFooterData() for per-session token counts. Update handleKey to route input to active session.
Why: Viewport, footer, and input must reflect whichever session is active for correct user experience.
Files: ~ internal/app/render.go
Files: ~ internal/app/input.go
Snippet: // updateViewportContent() — route to active session\nfunc (m *Model) updateViewportContent() {\n    cs := m.activeSession()\n    if cs == nil {\n        return\n    }\n    // Same render logic as before, but all m.session/m.stream refs become cs/cs.stream\n    // Render cs.file.Messages, then if cs.stream.active render live streaming\n}\n\n// buildFooterData() — per-session tokens\nfunc (m Model) buildFooterData() ui.FooterData {\n    cs := m.activeSession()\n    sessionIn := cs.totalInputTokens()\n    sessionOut := cs.totalOutputTokens()\n    streamOut := cs.stream.metrics.TotalOutputTokens()\n    return ui.FooterData{\n        Model:            modelBasename(m.settings.Model),\n        Provider:         m.settings.Provider,\n        TotalTokens:      sessionIn + sessionOut + streamOut,\n        TotalInputTokens: sessionIn,\n        TotalOutTokens:   sessionOut + streamOut,\n        Streaming:        cs.stream.active,\n        ThinkingOn:       m.settings.Thinking,\n        TokPerSec:        cs.stream.metrics.AvgTokenPerSec(),\n        WorkingDir:       m.workingDir,\n    }\n}\n\n// Header — show session context label for visual indication\nfunc (m Model) sessionLabel() string {\n    cs := m.activeSession()\n    if cs == nil || cs.ParentID == "" {\n        return "Orchestrator"\n    }\n    // Show truncated first user msg as label\n    for _, msg := range cs.file.Messages {\n        if msg.Role == config.RoleUser {\n            label := msg.Text\n            if len(label) > 40 {\n                label = label[:37] + "..."\n            }\n            return "Agent: " + label\n        }\n    }\n    return "Agent"\n}
Acceptance: Viewport renders correct session, footer shows per-session stats
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 4 - Event Loop & Stream Multiplexing
Pattern: Tagged Event Routing, Direct Channel Loop
Objective: Refactor the Bubble Tea event loop to route stream events to the correct session, and add a direct channel-based stream loop for child agents that bypasses Bubble Tea.
Success: Tagged stream events route to correct session, direct stream loop processes child tool loops synchronously, active child state visible in TUI via chatSession.stream.
Diagram: waitForStreamEvent(sid, ch) → taggedStreamEvent{sid, event}. Update() routes: if sid == activeSessionID → handleStreamEvent(activeSession). Child loop: runStreamLoop(sess) → range ch → processEvent(sess, event) → handles tool_calls restart inline.

### TASK: 4.1 - Add tagged stream events and session-aware routing
Layer: Application
What: Replace streamEventMsg with taggedStreamEvent including sessionID. Update waitForStreamEvent to accept sessionID. Update Update() to route events to the correct session.
Why: Bubble Tea processes one message at a time — tagging enables routing to the correct session. Non-active sessions accumulate state silently.
Files: ~ internal/app/events.go
Files: ~ internal/app/update.go
Files: ~ internal/app/stream.go
Snippet: // Replace streamEventMsg with tagged version\ntype taggedStreamEvent struct {\n    sessionID string\n    event     chat.StreamEvent\n}\n\n// waitForStreamEventFor blocks on a session-specific channel\nfunc waitForStreamEventFor(sid string, ch <-chan chat.StreamEvent) tea.Cmd {\n    return func() tea.Msg {\n        event, ok := <-ch\n        if !ok {\n            return taggedStreamEvent{sid, chat.StreamEvent{Done: true}}\n        }\n        return taggedStreamEvent{sid, event}\n    }\n}\n\n// In sendMessage() — tag the stream command\ncs := m.activeSession()\nreturn m, tea.Batch(waitForStreamEventFor(cs.file.Session.ID, ch), streamTickCmd())
Snippet: // In Update() — new case for tagged events\ncase taggedStreamEvent:\n    if msg.sessionID == m.ActiveSessionID {\n        // Active session: full processing + render\n        return m.processStreamEventFor(m.activeSession(), msg.event)\n    }\n    // Non-active session: accumulate stream state silently\n    if cs, ok := m.Sessions[msg.sessionID]; ok {\n        m.processStreamEventSilent(cs, msg.event)\n    }\n    return m, nil
Acceptance: Events route to correct session, non-active sessions accumulate silently
Verification: cd ~/src/squid-os && go build ./...

### TASK: 4.2 - Refactor handleStreamEvent to target chatSession
Layer: Application
What: Extract stream event processing into processStreamEventFor(*chatSession, event) that reads/writes cs.stream and cs.file. Keep executeTools on Model but accept target session for FileState.
Why: handleStreamEvent must operate on any session. This also makes it reusable by runStreamLoop and headless mode.
Files: ~ internal/app/stream.go
Snippet: // processStreamEventFor — operates on any session, used by TUI event loop\nfunc (m *Model) processStreamEventFor(cs *chatSession, event chat.StreamEvent) (tea.Model, tea.Cmd) {\n    // All m.stream refs -> cs.stream, all m.session refs -> cs\n    if event.Error != nil {\n        text, image, _ := cs.cancelTruncate()\n        if text != "" {\n            m.textarea.SetValue(text)\n        }\n        cs.stream.reset()\n        cmd := m.setChatMode()\n        m.updateViewportContent()\n        return m, cmd\n    }\n    if event.Done {\n        if len(cs.stream.partialTools) > 0 {\n            toolEntries := m.executeToolsFor(cs, cs.stream.partialTools)\n            m.appendAssistantMsgFor(cs, toolEntries)\n            cs.stream.reset()\n            m.updateViewportContent()\n            return m, m.startStreamFor(cs)\n        }\n        // Normal completion\n        m.appendAssistantMsgFor(cs, nil)\n        cs.stream.reset()\n        blinkCmd := m.setChatMode()\n        m.updateViewportContent()\n        return m, blinkCmd\n    }\n    // Accumulate text/thinking/tool deltas into cs.stream\n    // ... (existing logic with m.stream -> cs.stream)\n    sid := cs.file.Session.ID\n    return m, waitForStreamEventFor(sid, cs.stream.ch)\n}
Snippet: // processStreamEventSilent — accumulates state without rendering\nfunc (m *Model) processStreamEventSilent(cs *chatSession, event chat.StreamEvent) {\n    if event.Done {\n        if len(cs.stream.partialTools) > 0 {\n            toolEntries := m.executeToolsFor(cs, cs.stream.partialTools)\n            m.appendAssistantMsgFor(cs, toolEntries)\n            cs.stream.reset()\n            // Auto-restart for silent session\n            m.startStreamForSilent(cs)\n        } else {\n            m.appendAssistantMsgFor(cs, nil)\n            cs.stream.reset()\n        }\n        return\n    }\n    if event.Text != "" {\n        cs.stream.AddTextChunk(event.Text)\n    }\n    if event.Thinking != "" {\n        cs.stream.AddThinkChunk(event.Thinking)\n    }\n    if event.ToolCallDelta != "" {\n        accumulateToolCallDelta(&cs.stream, event)\n    }\n}\n\n// executeToolsFor — same as executeTools but targets specific session\nfunc (m *Model) executeToolsFor(cs *chatSession, partials []partialTool) []config.ToolCallEntry {\n    if cs.file.FileState == nil {\n        cs.file.FileState = make(map[string]config.FileStateEntry)\n    }\n    sessionState := cs.file.FileState\n    // ... same execution logic, but writes to cs.file.FileState\n    // ... agent_call special-cased here for nested sub-agents\n}
Acceptance: processStreamEventFor works on any session, executeToolsFor accumulates into target FileState
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 5 - Session Persistence
Pattern: Hierarchical Storage
Objective: Save child sessions in a sub-folder under the parent session name so the session directory stays organized.
Success: Child sessions save to sessions/<parent-name>/<child-id>.chat.json, load/restore works for children, parent session listing is unaffected.
Diagram: SaveSession(parentName, childID, file) → sessions/<parent-name>/<child-id>.chat.json. New helper: SaveChildSession(paths, parentName, childID, sf). LoadChildSession(paths, parentName, childID).

### TASK: 5.1 - Add ParentID to Session and child persistence helpers
Layer: Infrastructure
What: Add ParentID field to config.Session. Create SaveChildSession, LoadChildSession, and ChildSessionPath helpers for sessions/parent-name/child-id.chat.json storage.
Why: Child sessions need structured storage under their parent to keep the sessions directory clean and traceable.
Files: ~ internal/config/session.go
Snippet: // Add to Session struct in config/session.go\ntype Session struct {\n    ID               string `json:"id"`\n    Title            string `json:"title"`\n    ParentID         string `json:"parent_id,omitempty"` // new field\n    CreatedAt        string `json:"created_at"`\n    // ... rest unchanged\n}
Snippet: // Child session persistence helpers\nfunc ChildSessionDir(p Paths, parentName string) string {\n    return filepath.Join(p.Sessions, parentName)\n}\n\nfunc ChildSessionPath(p Paths, parentName, childID string) string {\n    return filepath.Join(ChildSessionDir(p, parentName), childID+"-agent.chat.json")\n}\n\nfunc SaveChildSession(p Paths, parentName, childID string, sf SessionFile) error {\n    sf.Session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)\n    if err := os.MkdirAll(ChildSessionDir(p, parentName), 0755); err != nil {\n        return err\n    }\n    data, err := json.MarshalIndent(sf, "", "  ")\n    if err != nil {\n        return err\n    }\n    return os.WriteFile(ChildSessionPath(p, parentName, childID), data, 0644)\n}\n\nfunc LoadChildSession(p Paths, parentName, childID string) (SessionFile, error) {\n    data, err := os.ReadFile(ChildSessionPath(p, parentName, childID))\n    if err != nil {\n        return SessionFile{}, err\n    }\n    var sf SessionFile\n    return sf, json.Unmarshal(data, &sf)\n}
Acceptance: Child sessions save to subfolder, load correctly, ParentID is persisted
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 6 - Headless Unification
Pattern: Shared Core, Eliminate Duplication
Objective: Replace the bare-bones headless stream loop with the same session/stream processing engine used by the TUI, enabling headless mode to support tools and thinking natively.
Success: headless.Run() uses chatSession + runStreamLoop, processes tool calls, supports thinking output, single stream processing code path for both TUI and headless.
Diagram: headless.Run() → create chatSession → append user message → runStreamLoop(sess) [same as child agent] → tool_calls → executeTools() → restart stream → on done, print accumulated text to stdout.

### TASK: 6.1 - Refactor headless.Run to use shared stream engine
Layer: Application
What: Replace bare-bones headless stream loop with chatSession creation plus runStreamLoop. Headless creates a session, appends user prompt, calls runStreamLoop, and prints accumulated output with native tool support.
Why: Eliminates duplicate stream loop. Headless and child agents share the same engine with full tool support.
Files: ~ internal/headless/headless.go
Snippet: // headless.go — replaced bare stream loop with shared session engine\nfunc Run(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, prompt, imagePath string) error {\n    // Build a minimal Model for stream processing (no TUI components needed)\n    m := buildHeadlessModel(paths, settings, endpoints)\n\n    cs := m.activeSession()\n    cs.appendMsg(config.Message{\n        ID:          "msg_1",\n        Role:        config.RoleUser,\n        Text:        prompt,\n        ImagePath:   imagePath,\n        InputTokens: countTokensApprox(prompt),\n    })\n\n    // Run the same loop used by child agents — full tool support\n    result := m.runStreamLoop(cs)\n\n    fmt.Println(result)\n    return nil\n}\n\n// buildHeadlessModel creates a minimal Model with initialized session and tools\nfunc buildHeadlessModel(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig) *app.Model {\n    wd, _ := os.Getwd()\n    var sess chatSession\n    sess.clear(settings, paths, wd)\n    m := &app.Model{\n        Sessions:        map[string]*chatSession{sess.file.Session.ID: &sess},\n        ActiveSessionID: sess.file.Session.ID,\n        settings:        settings,\n        endpoints:       endpoints,\n        paths:           paths,\n        workingDir:      wd,\n    }\n    m.toolReg = tools.GetRegistry()\n    return m\n}
Acceptance: headless.Run() processes tools natively, go build passes
Verification: cd ~/src/squid-os && go build ./...

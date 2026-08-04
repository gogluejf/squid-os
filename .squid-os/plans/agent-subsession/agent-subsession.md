# Agent Sub-session: Multi-context streaming with child agents

## Core Problem

The current app supports only a single chat session with a single stream. Models need to delegate work to sub-agents with their own context, tools, and skills — spawning child sessions that the user can toggle between while the parent tool call blocks awaiting results.

## Goal

agent_call tool spawns isolated child sessions with full tool support, user toggles between parent and child views, child file state is isolated, headless mode reuses the same stream engine

---

## 1. Session Architecture

- **Pattern:** Entity-Context, State Migration

**Objective:** Move session ownership and stream state from Model into chatSession so each session is independently streamable, then add multi-session management to Model.

**Success Criteria:** Model manages multiple chatSession instances keyed by ID, each with its own streamState, active session drives the viewport, single session path is backward compatible.

```mermaid
Model { sessions map[ID]*chatSession, activeSessionID, textarea, viewport, mode }. chatSession { file config.SessionFile, stream streamState, renderedMessages, undoStack, parentID }. Mode derived from activeSession.stream.active + global overlays.
```

### 1.1. Move streamState into chatSession

**What:** Move streamState from Model into chatSession struct. Add ParentID and ToolCallID fields to chatSession for child session tracking.

**Why:** Each session needs its own independent stream state so multiple sessions can stream concurrently. Parent/child linkage enables the agent_call tool to track ownership.

**Files:**

- ~ internal/app/chat_session.go

**Snippet:**

```
type chatSession struct {
    file             config.SessionFile
    renderedMessages []string // glamour cache, 1:1 with file.Messages
    renderedWidth    int
    undoStack        [][]config.Message
    stream           streamState  // moved from Model — each session has its own
    ParentID         string       // parent session ID (empty string for root sessions)
    ToolCallID       string       // originating tool call ID from parent (empty for root)
}
```

**Acceptance Criteria:**

- [ ] chatSession compiles with embedded streamState

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 1.2. Replace Model.session with session map

**What:** Replace single chatSession field on Model with map[string]*chatSession and activeSessionID string. Add helper methods activeSession() and setActiveSession().

**Why:** Model must manage multiple sessions for parent-child toggling with O(1) lookup.

**Files:**

- ~ internal/app/app.go

**Snippet:**

```
// On Model — replace: session chatSession
Sessions        map[string]*chatSession
ActiveSessionID string

func (m *Model) activeSession() *chatSession {
    if s, ok := m.Sessions[m.ActiveSessionID]; ok {
        return s
    }
    return nil
}

func (m *Model) addSession(cs *chatSession) {
    if m.Sessions == nil {
        m.Sessions = make(map[string]*chatSession)
    }
    m.Sessions[cs.file.Session.ID] = cs
}
```

```
// In New() — initialize with single session:
Sessions:        map[string]*chatSession{sess.file.Session.ID: &sess},
ActiveSessionID: sess.file.Session.ID,
```

**Acceptance Criteria:**

- [ ] Model compiles with sessions map, activeSession() returns correct session

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 1.3. Update all Model methods to use activeSession()

**What:** Rewrite every reference to m.session and m.stream across all app files to use m.activeSession() and m.activeSession().stream.

**Why:** After the structural change all existing code must route through the active session.

**Files:**

- ~ internal/app/stream.go
- ~ internal/app/session.go
- ~ internal/app/render.go
- ~ internal/app/input.go

**Snippet:**

```
// Systematic replacements across stream.go, session.go, render.go, input.go:
// m.session.xxx          -> m.activeSession().xxx
// m.stream.xxx           -> m.activeSession().stream.xxx
// m.session.file.Messages -> m.activeSession().file.Messages
```

```
// Example from sendMessage():
cs := m.activeSession()
cs.appendMsg(userMsg)
cs.undoStack = nil
apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, cs.file.Messages)
```

```
// Example from handleStreamEvent():
cs := m.activeSession()
if event.Done {
    if cs.stream.userCancelled { ... }
}
```

**Acceptance Criteria:**

- [ ] All references compile via activeSession(), app runs identically

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 1.4. Derive Model.mode from active session state

**What:** Derive m.mode from activeSession().stream.active combined with overlay modes. Update setStreamMode() and setChatMode() for active session.

**Why:** With multiple sessions mode depends on which session is active. Overlay modes remain global.

**Files:**

- ~ internal/app/stream.go
- ~ internal/app/modes.go

**Snippet:**

```
// setStreamMode() and setChatMode() operate on active session stream only
func (m *Model) setStreamMode() {
    cs := m.activeSession()
    cs.stream.reset()
    cs.stream.active = true
    cs.stream.metrics.Start = time.Now()
    m.mode = ModeStreaming
    m.textarea.Placeholder = "ctrl+c to cancel..."
}

func (m *Model) setChatMode() tea.Cmd {
    cs := m.activeSession()
    cs.stream.active = false
    m.textarea.Placeholder = "Type a message..."
    m.mode = ModeChat
    m.textarea.Focus()
    m.recalcLayout()
    return textarea.Blink
}
```

```
// computeMode() derives effective mode from overlays + active session
func (m *Model) computeMode() Mode {
    switch {
    case m.mode == ModeHelp:
        return ModeHelp
    case m.mode == ModeModelPicker:
        return ModeModelPicker
    case m.mode == ModeSessionPicker:
        return ModeSessionPicker
    case m.mode == ModeFilePicker:
        return ModeFilePicker
    case m.mode == ModeSavePrompt:
        return ModeSavePrompt
    case m.mode == ModeHistorySearch:
        return ModeHistorySearch
    case m.cmdPalette.Visible:
        return ModeChat
    }
    if cs := m.activeSession(); cs != nil && cs.stream.active {
        return ModeStreaming
    }
    return ModeChat
}
```

**Acceptance Criteria:**

- [ ] Mode reflects active session streaming state correctly

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

---

## 2. agent_call Tool

- **Pattern:** Command, Synchronous Delegation

**Objective:** Implement the agent_call tool that spawns child sessions, runs them to completion (blocking the tool executor), and returns the result to the parent.

**Success Criteria:** Model can call agent_call with prompt/tools/skills/skill_default, child session runs with full tool loop, file state isolated, result flows back as tool execution output.

```mermaid
executeTools() detects agent_call → spawnChildSession() → runStreamLoop(child) [direct channel loop, no Bubble Tea] → child accumulates messages + tool results → loop returns final text → set Execution.Result on parent tool entry.
```

### 2.1. Define agent_call tool schema

**What:** Create agent_call Tool struct with schema: prompt (string), tools (array of strings), skills (array of strings), skill_default (string). Register it in the tools init list.

**Why:** The model needs a callable tool definition to spawn sub-agents with configurable tool and skill access.

**Files:**

- ~ internal/tools/tools.go

**Snippet:**

```
var AgentCall = Tool{
    Name:         "agent_call",
    Description:  "Spawn a child agent session with its own context window, tools, and skills. " +
        "The child runs to completion and returns its final output. " +
        "Use for focused sub-tasks that need their own context.",
    Schema: []byte(`{
        "type": "object",
        "properties": {
            "prompt": {"type": "string", "description": "The prompt/task for the sub-agent"},
            "tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names the child can use (e.g. read_file, bash). Empty = no tools."},
            "skills": {"type": "array", "items": {"type": "string"}, "description": "Skill names available to the child agent."},
            "skill_default": {"type": "string", "description": "Default skill to auto-load into child session on init."}
        },
        "required": ["prompt"]
    }`),
    Execute: func(args map[string]interface{}) ToolResult {
        // Handled specially in executeTools — not here
        return ToolResult{Status: ResultStatusError, Error: "agent_call is processed inline in executeTools"}
    },
}
```

**Acceptance Criteria:**

- [ ] agent_call appears in tools.GetTools(), schema is valid JSON

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 2.2. Implement spawnChildSession and runStreamLoop

**What:** Create spawnChildSession() that initializes a child chatSession with ParentID, system prompt, environment, filtered tools, and skill_default. Create runStreamLoop(*chatSession) that drives engine.Stream directly, handles tool_calls restart, and returns final output.

**Why:** Child sessions run inside executeTools which blocks Bubble Tea — they need a direct channel loop. spawnChildSession sets up isolation, runStreamLoop drives the full cycle.

**Files:**

- ~ internal/app/stream.go
- ~ internal/app/chat_session.go

**Snippet:**

```
// spawnChildSession creates and initializes a child session
func (m *Model) spawnChildSession(parentID string, prompt string,
    toolNames []string, skillNames []string, skillDefault string) *chatSession {
    cs := &chatSession{
        ParentID: parentID,
    }
    cs.file = config.NewSessionFile(m.settings.Provider, m.settings.Model,
        m.settings.Thinking, m.settings.SystemPromptFile, m.workingDir)

    // Push system prompt + environment messages (reuse clear() logic)
    // Inject skill_default if set: load skill and push as system/internal msg
    // Append prompt as user message
    cs.appendMsg(config.Message{
        ID:   "msg_1",
        Role: config.RoleUser,
        Text: prompt,
    })

    // Filter tools to allowed list
    cs.allowedTools = filterTools(tools.GetTools(), toolNames)

    m.addSession(cs)
    return cs
}
```

```
// runStreamLoop drives a session stream to completion via direct channel loop
// Used by both child agents (inside executeTools) and headless mode.
func (m *Model) runStreamLoop(cs *chatSession) string {
    var result strings.Builder

    for {
        apiMsgs := chat.BuildAPIMessages(m.paths, m.settings, cs.file.Messages)
        engine := chat.NewEngine(
            config.ResolveChatURL(m.endpoints, m.settings.Provider),
            m.settings.Model, m.settings.Thinking,
        )

        ctx, cancel := context.WithCancel(context.Background())
        ch := engine.Stream(ctx, apiMsgs, cs.allowedTools)

        for event := range ch {
            cancel()
            if event.Error != nil {
                result.WriteString("[agent error: " + event.Error.Error() + "]")
                return result.String()
            }
            if event.Done {
                if event.StopReason == "tool_calls" && len(cs.stream.partialTools) > 0 {
                    // Execute tools on this child, append assistant msg, outer loop restarts stream
                    toolEntries := m.executeToolsFor(cs, cs.stream.partialTools)
                    m.appendAssistantMsgFor(cs, toolEntries)
                    cs.stream.reset()
                    continue OUTER // restart stream with tool results in history
                }
                // Normal completion — save final assistant msg
                m.appendAssistantMsgFor(cs, nil)
                return result.String()
            }
            if event.Text != "" {
                result.WriteString(event.Text)
                cs.stream.AddTextChunk(event.Text)
            }
            if event.Thinking != "" {
                cs.stream.AddThinkChunk(event.Thinking)
            }
            if event.ToolCallDelta != "" {
                // Accumulate partial tool state (same logic as TUI handleStreamEvent)
                accumulateToolCallDelta(&cs.stream, event)
            }
        }
    }
}
```

**Acceptance Criteria:**

- [ ] spawnChildSession creates valid session, runStreamLoop completes full stream cycle with tool support

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 2.3. Implement agent_call Execute in executeTools

**What:** In executeTools, detect agent_call by name. Parse args, call spawnChildSession and runStreamLoop sequentially for each agent. Collect results and return as Execution.Result. Save child session to disk after completion.

**Why:** This bridges the tool framework with the new session architecture — the integration point where parent tool execution spawns child agents.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In executeTools() — special case for agent_call
if p.name == "agent_call" {
    var argsStruct struct {
        Prompt       string   `json:"prompt"`
        Tools        []string `json:"tools"`
        Skills       []string `json:"skills"`
        SkillDefault string   `json:"skill_default"`
    }
    json.Unmarshal([]byte(p.args), &argsStruct)

    // Spawn child session and run to completion (blocking)
    child := m.spawnChildSession(m.ActiveSessionID, argsStruct.Prompt,
        argsStruct.Tools, argsStruct.Skills, argsStruct.SkillDefault)

    // Tag the child with the originating tool call ID
    child.ToolCallID = p.id

    result := m.runStreamLoop(child)

    // Save child session to subfolder under parent
    parentName := m.activeSession().file.Session.Title
    if parentName == "" {
        parentName = "unsaved"
    }
    _ = config.SaveChildSession(m.paths, parentName,
        child.file.Session.ID, child.file)

    entries[i].Execution.Status = tools.ResultStatusSuccess
    entries[i].Execution.Result = result
    continue // skip normal tool execution path
}
```

**Acceptance Criteria:**

- [ ] agent_call spawns child, runs to completion, returns result, child is saved

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

---

## 3. Multi-session TUI

- **Pattern:** Viewport Multiplexing, Key-driven Navigation

**Objective:** Enable the user to toggle between parent and child session views, render the active session's viewport, and derive mode from the active session state.

**Success Criteria:** ctrl+tab cycles between active session and its children, viewport shows correct session, mode reflects active session streaming state, textarea binds to active session.

```mermaid
handleKey() routes session-switch key → rotate activeSessionID through parent+children → updateViewportContent() uses active session → mode = activeSession.stream.active ? ModeStreaming : ModeChat → textarea binds to active session.
```

### 3.1. Add session switch keybinding

**What:** Add ctrl+tab keybinding that cycles activeSessionID between parent and its children. Add session indicator to header showing current context (Parent vs Agent).

**Why:** User needs to toggle between parent and child views to monitor agent progress.

**Files:**

- ~ internal/app/keymap.go
- ~ internal/app/input.go
- ~ internal/app/render.go

**Snippet:**

```
// New key binding in keymap.go
SwitchSession: key.NewBinding(
    key.WithKeys("ctrl+tab"),
    key.WithHelp("ctrl+tab", "switch session"),
),

// In handleChatKey():
case key.Matches(msg, keys.SwitchSession):
    return m.switchSession(), nil

func (m *Model) switchSession() tea.Model {
    // Build ordered list: current active first, then children by spawn order
    var ordered []string
    for id, cs := range m.Sessions {
        if cs.ParentID == m.ActiveSessionID || id == m.ActiveSessionID {
            ordered = append(ordered, id)
        }
    }
    if len(ordered) < 2 {
        return m
    }
    // Rotate to next in cycle
    for i, id := range ordered {
        if id == m.ActiveSessionID {
            m.ActiveSessionID = ordered[(i+1)%len(ordered)]
            break
        }
    }
    m.updateViewportContent()
    m.recalcLayout()
    return m
}
```

**Acceptance Criteria:**

- [ ] ctrl+tab cycles sessions, viewport updates, header shows context

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 3.2. Update viewport and footer to bind to active session

**What:** Rewrite updateViewportContent() to render activeSession. Update buildFooterData() for per-session token counts. Update handleKey to route input to active session.

**Why:** Viewport, footer, and input must reflect whichever session is active for correct user experience.

**Files:**

- ~ internal/app/render.go
- ~ internal/app/input.go

**Snippet:**

```
// updateViewportContent() — route to active session
func (m *Model) updateViewportContent() {
    cs := m.activeSession()
    if cs == nil {
        return
    }
    // Same render logic as before, but all m.session/m.stream refs become cs/cs.stream
    // Render cs.file.Messages, then if cs.stream.active render live streaming
}

// buildFooterData() — per-session tokens
func (m Model) buildFooterData() ui.FooterData {
    cs := m.activeSession()
    sessionIn := cs.totalInputTokens()
    sessionOut := cs.totalOutputTokens()
    streamOut := cs.stream.metrics.TotalOutputTokens()
    return ui.FooterData{
        Model:            modelBasename(m.settings.Model),
        Provider:         m.settings.Provider,
        TotalTokens:      sessionIn + sessionOut + streamOut,
        TotalInputTokens: sessionIn,
        TotalOutTokens:   sessionOut + streamOut,
        Streaming:        cs.stream.active,
        ThinkingOn:       m.settings.Thinking,
        TokPerSec:        cs.stream.metrics.AvgTokenPerSec(),
        WorkingDir:       m.workingDir,
    }
}

// Header — show session context label for visual indication
func (m Model) sessionLabel() string {
    cs := m.activeSession()
    if cs == nil || cs.ParentID == "" {
        return "Orchestrator"
    }
    // Show truncated first user msg as label
    for _, msg := range cs.file.Messages {
        if msg.Role == config.RoleUser {
            label := msg.Text
            if len(label) > 40 {
                label = label[:37] + "..."
            }
            return "Agent: " + label
        }
    }
    return "Agent"
}
```

**Acceptance Criteria:**

- [ ] Viewport renders correct session, footer shows per-session stats

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

---

## 4. Event Loop & Stream Multiplexing

- **Pattern:** Tagged Event Routing, Direct Channel Loop

**Objective:** Refactor the Bubble Tea event loop to route stream events to the correct session, and add a direct channel-based stream loop for child agents that bypasses Bubble Tea.

**Success Criteria:** Tagged stream events route to correct session, direct stream loop processes child tool loops synchronously, active child state visible in TUI via chatSession.stream.

```mermaid
waitForStreamEvent(sid, ch) → taggedStreamEvent{sid, event}. Update() routes: if sid == activeSessionID → handleStreamEvent(activeSession). Child loop: runStreamLoop(sess) → range ch → processEvent(sess, event) → handles tool_calls restart inline.
```

### 4.1. Add tagged stream events and session-aware routing

**What:** Replace streamEventMsg with taggedStreamEvent including sessionID. Update waitForStreamEvent to accept sessionID. Update Update() to route events to the correct session.

**Why:** Bubble Tea processes one message at a time — tagging enables routing to the correct session. Non-active sessions accumulate state silently.

**Files:**

- ~ internal/app/events.go
- ~ internal/app/update.go
- ~ internal/app/stream.go

**Snippet:**

```
// Replace streamEventMsg with tagged version
type taggedStreamEvent struct {
    sessionID string
    event     chat.StreamEvent
}

// waitForStreamEventFor blocks on a session-specific channel
func waitForStreamEventFor(sid string, ch <-chan chat.StreamEvent) tea.Cmd {
    return func() tea.Msg {
        event, ok := <-ch
        if !ok {
            return taggedStreamEvent{sid, chat.StreamEvent{Done: true}}
        }
        return taggedStreamEvent{sid, event}
    }
}

// In sendMessage() — tag the stream command
cs := m.activeSession()
return m, tea.Batch(waitForStreamEventFor(cs.file.Session.ID, ch), streamTickCmd())
```

```
// In Update() — new case for tagged events
case taggedStreamEvent:
    if msg.sessionID == m.ActiveSessionID {
        // Active session: full processing + render
        return m.processStreamEventFor(m.activeSession(), msg.event)
    }
    // Non-active session: accumulate stream state silently
    if cs, ok := m.Sessions[msg.sessionID]; ok {
        m.processStreamEventSilent(cs, msg.event)
    }
    return m, nil
```

**Acceptance Criteria:**

- [ ] Events route to correct session, non-active sessions accumulate silently

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

### 4.2. Refactor handleStreamEvent to target chatSession

**What:** Extract stream event processing into processStreamEventFor(*chatSession, event) that reads/writes cs.stream and cs.file. Keep executeTools on Model but accept target session for FileState.

**Why:** handleStreamEvent must operate on any session. This also makes it reusable by runStreamLoop and headless mode.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// processStreamEventFor — operates on any session, used by TUI event loop
func (m *Model) processStreamEventFor(cs *chatSession, event chat.StreamEvent) (tea.Model, tea.Cmd) {
    // All m.stream refs -> cs.stream, all m.session refs -> cs
    if event.Error != nil {
        text, image, _ := cs.cancelTruncate()
        if text != "" {
            m.textarea.SetValue(text)
        }
        cs.stream.reset()
        cmd := m.setChatMode()
        m.updateViewportContent()
        return m, cmd
    }
    if event.Done {
        if len(cs.stream.partialTools) > 0 {
            toolEntries := m.executeToolsFor(cs, cs.stream.partialTools)
            m.appendAssistantMsgFor(cs, toolEntries)
            cs.stream.reset()
            m.updateViewportContent()
            return m, m.startStreamFor(cs)
        }
        // Normal completion
        m.appendAssistantMsgFor(cs, nil)
        cs.stream.reset()
        blinkCmd := m.setChatMode()
        m.updateViewportContent()
        return m, blinkCmd
    }
    // Accumulate text/thinking/tool deltas into cs.stream
    // ... (existing logic with m.stream -> cs.stream)
    sid := cs.file.Session.ID
    return m, waitForStreamEventFor(sid, cs.stream.ch)
}
```

```
// processStreamEventSilent — accumulates state without rendering
func (m *Model) processStreamEventSilent(cs *chatSession, event chat.StreamEvent) {
    if event.Done {
        if len(cs.stream.partialTools) > 0 {
            toolEntries := m.executeToolsFor(cs, cs.stream.partialTools)
            m.appendAssistantMsgFor(cs, toolEntries)
            cs.stream.reset()
            // Auto-restart for silent session
            m.startStreamForSilent(cs)
        } else {
            m.appendAssistantMsgFor(cs, nil)
            cs.stream.reset()
        }
        return
    }
    if event.Text != "" {
        cs.stream.AddTextChunk(event.Text)
    }
    if event.Thinking != "" {
        cs.stream.AddThinkChunk(event.Thinking)
    }
    if event.ToolCallDelta != "" {
        accumulateToolCallDelta(&cs.stream, event)
    }
}

// executeToolsFor — same as executeTools but targets specific session
func (m *Model) executeToolsFor(cs *chatSession, partials []partialTool) []config.ToolCallEntry {
    if cs.file.FileState == nil {
        cs.file.FileState = make(map[string]config.FileStateEntry)
    }
    sessionState := cs.file.FileState
    // ... same execution logic, but writes to cs.file.FileState
    // ... agent_call special-cased here for nested sub-agents
}
```

**Acceptance Criteria:**

- [ ] processStreamEventFor works on any session, executeToolsFor accumulates into target FileState

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

---

## 5. Session Persistence

- **Pattern:** Hierarchical Storage

**Objective:** Save child sessions in a sub-folder under the parent session name so the session directory stays organized.

**Success Criteria:** Child sessions save to sessions/<parent-name>/<child-id>.chat.json, load/restore works for children, parent session listing is unaffected.

```mermaid
SaveSession(parentName, childID, file) → sessions/<parent-name>/<child-id>.chat.json. New helper: SaveChildSession(paths, parentName, childID, sf). LoadChildSession(paths, parentName, childID).
```

### 5.1. Add ParentID to Session and child persistence helpers

**What:** Add ParentID field to config.Session. Create SaveChildSession, LoadChildSession, and ChildSessionPath helpers for sessions/parent-name/child-id.chat.json storage.

**Why:** Child sessions need structured storage under their parent to keep the sessions directory clean and traceable.

**Files:**

- ~ internal/config/session.go

**Snippet:**

```
// Add to Session struct in config/session.go
type Session struct {
    ID               string `json:"id"`
    Title            string `json:"title"`
    ParentID         string `json:"parent_id,omitempty"` // new field
    CreatedAt        string `json:"created_at"`
    // ... rest unchanged
}
```

```
// Child session persistence helpers
func ChildSessionDir(p Paths, parentName string) string {
    return filepath.Join(p.Sessions, parentName)
}

func ChildSessionPath(p Paths, parentName, childID string) string {
    return filepath.Join(ChildSessionDir(p, parentName), childID+"-agent.chat.json")
}

func SaveChildSession(p Paths, parentName, childID string, sf SessionFile) error {
    sf.Session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
    if err := os.MkdirAll(ChildSessionDir(p, parentName), 0755); err != nil {
        return err
    }
    data, err := json.MarshalIndent(sf, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(ChildSessionPath(p, parentName, childID), data, 0644)
}

func LoadChildSession(p Paths, parentName, childID string) (SessionFile, error) {
    data, err := os.ReadFile(ChildSessionPath(p, parentName, childID))
    if err != nil {
        return SessionFile{}, err
    }
    var sf SessionFile
    return sf, json.Unmarshal(data, &sf)
}
```

**Acceptance Criteria:**

- [ ] Child sessions save to subfolder, load correctly, ParentID is persisted

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

---

## 6. Headless Unification

- **Pattern:** Shared Core, Eliminate Duplication

**Objective:** Replace the bare-bones headless stream loop with the same session/stream processing engine used by the TUI, enabling headless mode to support tools and thinking natively.

**Success Criteria:** headless.Run() uses chatSession + runStreamLoop, processes tool calls, supports thinking output, single stream processing code path for both TUI and headless.

```mermaid
headless.Run() → create chatSession → append user message → runStreamLoop(sess) [same as child agent] → tool_calls → executeTools() → restart stream → on done, print accumulated text to stdout.
```

### 6.1. Refactor headless.Run to use shared stream engine

**What:** Replace bare-bones headless stream loop with chatSession creation plus runStreamLoop. Headless creates a session, appends user prompt, calls runStreamLoop, and prints accumulated output with native tool support.

**Why:** Eliminates duplicate stream loop. Headless and child agents share the same engine with full tool support.

**Files:**

- ~ internal/headless/headless.go

**Snippet:**

```
// headless.go — replaced bare stream loop with shared session engine
func Run(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, prompt, imagePath string) error {
    // Build a minimal Model for stream processing (no TUI components needed)
    m := buildHeadlessModel(paths, settings, endpoints)

    cs := m.activeSession()
    cs.appendMsg(config.Message{
        ID:          "msg_1",
        Role:        config.RoleUser,
        Text:        prompt,
        ImagePath:   imagePath,
        InputTokens: countTokensApprox(prompt),
    })

    // Run the same loop used by child agents — full tool support
    result := m.runStreamLoop(cs)

    fmt.Println(result)
    return nil
}

// buildHeadlessModel creates a minimal Model with initialized session and tools
func buildHeadlessModel(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig) *app.Model {
    wd, _ := os.Getwd()
    var sess chatSession
    sess.clear(settings, paths, wd)
    m := &app.Model{
        Sessions:        map[string]*chatSession{sess.file.Session.ID: &sess},
        ActiveSessionID: sess.file.Session.ID,
        settings:        settings,
        endpoints:       endpoints,
        paths:           paths,
        workingDir:      wd,
    }
    m.toolReg = tools.GetRegistry()
    return m
}
```

**Acceptance Criteria:**

- [ ] headless.Run() processes tools natively, go build passes

**Verify:**

```bash
cd ~/src/squid-os && go build ./...
```

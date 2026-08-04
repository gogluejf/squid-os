# Architecture Refactor Plan

## Why

The current code has three layers tangled together in `app/`:

1. **Domain** — session state (messages, inference config, tools, skills)
2. **Inference loop** — stream processing, tool execution, restart cycle
3. **TUI** — rendering, undo, notifications, auto-save, Bubble Tea

Everything lives in `app/stream.go`, `app/chat_session.go`, and `app/session.go` as methods on `Model` or `chatSession`. This means:

- **CLI/headless** can't run the inference loop — it's coupled to Bubble Tea
- **Subagents** can't have independent sessions — `chatSession` pulls from global `Settings`
- **Reloading a session** doesn't know what tools/skills it used — they're not persisted
- **`handleStreamEvent`** (~300 lines) mixes stream processing + rendering + saving + auth

## Goals

1. **Pure `chat.Session`** — owns its own state (inference, tools, skills, messages). No dependency on `Settings`, `UI`, or `Model`. CLI and subagents can create and run sessions independently.

2. **Pure `chat/loop.go`** — stream processing and tool execution without UI. `RunLoop()` drives a session to completion. Used by CLI, subagents, and headless.

3. **Thin `app/stream.go`** — TUI wrapper that calls loop functions and handles rendering/saving/notifications. ~30 lines instead of ~500.

4. **Per-session tools/skills** — stored on `config.Session` (like `InferenceConfig`). A saved session knows what it was allowed to use. Reload is deterministic.

5. **Multi-session ready** — `Model` manages `map[string]*UISession`. Subagents get their own `UISession`. Event routing uses session IDs.

## The Split

| Layer | Package | What it owns | What it doesn't touch |
|-------|---------|-------------|---------------------|
| **Persisted format** | `config` | `SessionDoc`, `SessionMeta`, `InferenceConfig`, `SessionTools`, `SessionSkills` | Nothing — pure JSON structs + file I/O |
| **Domain** | `chat` | `Session` (wraps `SessionDoc` + `streamState`), `SessionConfig` | No UI, no Bubble Tea, no disk save |
| **Inference loop** | `chat/loop` | `ProcessStreamEvent`, `ExecuteTools`, `RunLoop` | No rendering, no save, no notifications |
| **TUI wrapper** | `app/ui_session` | `*chat.Session` + render cache + undo + `LoadFromDoc` | No disk I/O, no event loop |
| **TUI stream** | `app/stream` | Delegates to `chat/loop`, adds render + save + notify + auth | No core processing logic |
| **TUI persistence** | `app/persistence` | `saveAs`, `autoSave` (calls `session.Save()` + notification) | No picker, no mode changes |
| **TUI session UI** | `app/tui_session` | `clearSession`, `toggleIncognito`, `openSessionPicker` (UI components) | No core session logic |

## Rule of Thumb

- If it reads/writes `Doc.Session` or `Stream` → **`chat/`**
- If it calls `config.SaveSessionDoc()` or `config.LoadSessionDoc()` → **`chat/`** (domain owns its persistence)
- If it touches `viewport`, `textarea`, `mode`, `notification`, `component` → **`app/`**
- If it processes `StreamEvent` without rendering → **`chat/loop`**
- If it takes a `LoopResult` and renders/saves/notifies → **`app/stream`**

## Implementation Order

### Phase 1: Config changes (small, low risk)
- [ ] 1.1 Add `SessionTools` and `SessionSkills` structs to `config/session.go`
- [ ] 1.2 Add `Tools SessionTools` and `Skills SessionSkills` fields to `config.Session`
- [ ] 1.3 Rename `SessionFile` → `SessionDoc`, `Session` → `SessionMeta` in `config/session.go`
- [ ] 1.4 Rename `NewSessionFile` → `NewSessionDoc`, update signature to take `InferenceConfig` + tools/skills
- [ ] 1.5 Rename `SaveSession` → `SaveSessionDoc`, `LoadSession` → `LoadSessionDoc`
- [ ] 1.6 Update all callers of renamed config functions across the codebase
- [ ] 1.7 `go build ./...` passes

### Phase 2: Extract streamState to chat (mechanical move)
- [ ] 2.1 Create `chat/stream_state.go` — move `streamState`, `partialTool`, `StreamMetrics` from `app/stream.go`
- [ ] 2.2 Update imports in `app/stream.go` to reference `chat.streamState`
- [ ] 2.3 `go build ./...` passes

### Phase 3: Create chat.Session and chat/session.go (new file, no deletions yet)
- [ ] 3.1 Create `chat/session.go` with `Session` struct, `SessionConfig`, `NewSession()`, `LoadSession()`
- [ ] 3.2 Implement session data methods: `Append`, `TruncateTo`, `TruncateToUser`, `CancelTruncate`, `HasUserMessage`, `TotalInputTokens`, `TotalOutputTokens`, `Messages()`
- [ ] 3.3 Implement `BuildMessages()` (extract from current `chat_session.go` / engine logic)
- [ ] 3.4 Implement `GetTools()` / `SetTools()` / `GetSkills()` / `SetSkills()` — resolve from Doc.Session.Tools/Current
- [ ] 3.5 Implement inference methods: `CurrentInference`, `SetInference`, `PushConfigChange`, `PushSystemPromptChange`, `PushThinkingSwitch`
- [ ] 3.6 Implement `Save()` — calls `config.SaveSessionDoc()`
- [ ] 3.7 `go build ./...` passes

### Phase 4: Extract loop from stream.go (THE HARD PART)
- [ ] 4.1 Create `chat/loop.go` with `LoopAction` constants and `LoopResult` struct
- [ ] 4.2 Extract `ProcessStreamEvent(*Session, StreamEvent) LoopResult` from `handleStreamEvent` — pure processing only
- [ ] 4.3 Extract `ExecuteTools(*Session, *Registry, Paths) LoopResult` from `resumeToolExecution` — pure execution only
- [ ] 4.4 Extract `SaveAssistantMsg(*Session, Message) int` and `FlushToolMessage(*Session, int)` and `recomputeSequenceStats`
- [ ] 4.5 Implement `StartStream(*Session, Paths, EndpointsConfig) (chan, CancelFunc)` — creates engine + starts stream from session's own inference config
- [ ] 4.6 Implement `RunLoop(*Session, Paths, EndpointsConfig, *Registry) string` — full blocking loop
- [ ] 4.7 Rewrite `app/stream.go` — `handleStreamEvent`/`resumeToolExecution` become thin wrappers calling loop functions + TUI concerns
- [ ] 4.8 `go build ./...` passes, TUI works identically

### Phase 5: Create UISession and split TUI concerns
- [ ] 5.1 Create `app/ui_session.go` with `UISession` struct (embeds `*chat.Session` + render cache + undo)
- [ ] 5.2 Implement `LoadFromDoc`, `destroyLastSequence`, `undoDestroy`, `invalidateRender*`
- [ ] 5.3 Create `app/persistence.go` — move `saveAs`, `autoSave` from `session.go`
- [ ] 5.4 Create `app/tui_session.go` — move `clearSession`, `toggleIncognito`, `openSaveSessionPrompt`, `openSessionPicker` from `session.go`
- [ ] 5.5 `go build ./...` passes

### Phase 6: Update Model to multi-session
- [ ] 6.1 Replace `session chatSession` with `Sessions map[string]*UISession` + `ActiveSessionID string` in `app/app.go`
- [ ] 6.2 Add `activeSession() *UISession` helper
- [ ] 6.3 Update `New()` to create `map[string]*UISession` with single session
- [ ] 6.4 Update all `m.session.xxx` → `m.activeSession().xxx` across `stream.go`, `render.go`, `input.go`, `persistence.go`, `tui_session.go`
- [ ] 6.5 Update `app/events.go` — tag stream events with session ID
- [ ] 6.6 Update `app/update.go` — route stream events to correct session
- [ ] 6.7 `go build ./...` passes, TUI works identically

### Phase 7: Cleanup
- [ ] 7.1 Delete `app/chat_session.go`
- [ ] 7.2 Delete `app/session.go`
- [ ] 7.3 Update `headless/headless.go` to use `chat.NewSession()` + `loop.RunLoop()`
- [ ] 7.4 Final `go build ./...` passes, full TUI test, headless test

## Objects

### config.SessionFile
```go
type SessionDoc struct { // used to be SessionFile
    Version     int
    Session     Session
    Messages    []Message
    TotalTokens int
    FileState   map[string]FileStateEntry
}
```

### config.Session
```go
type SessionMeta struct { // used to be Session
    ID               string
    Title            string
    CreatedAt        string
    UpdatedAt        string
    Inference        SessionInference
    SystemPromptFile string
    WorkingDir       string
    Skill            SessionSkill
    Tools            SessionTools    `json:"tools,omitempty"`
    Skills           SessionSkills   `json:"skills,omitempty"`
}
```

### config.SessionTools
```go
type SessionTools struct {
    Initial []string `json:"initial"`
    Current []string `json:"current"`
}
```

### config.SessionSkills
```go
type SessionSkills struct {
    Initial []string `json:"initial"`
    Current []string `json:"current"`
}
```

### config.InferenceConfig
```go
type InferenceConfig struct {
    Provider string
    Model    string
    Thinking ThinkingConfig
}
```

### chat.Session (NEW - pure runtime object)
```go
type Session struct {
    Doc    config.SessionDoc
    Stream streamState
}

// SessionConfig — optional transport struct, may be useful for subagent/CLI
type SessionConfig struct {
    Provider         string
    Model            string
    Thinking         config.ThinkingConfig
    SystemPromptFile string
    Tools            []string   // tool names (nil = use app defaults)
    Skills           []string   // skill names (nil = use app defaults)
    WorkingDir       string
}
```

### streamState (moved from app to chat)
```go
type streamState struct {
    id               string
    text             string
    thinking         string
    inThinking       bool
    active           bool
    markdown         string
    markdownEnd      int
    metrics          StreamMetrics
    cancelFn         context.CancelFunc
    ch               <-chan StreamEvent
    userCancelled    bool
    partialTools     []partialTool
    tokenCount       int
    stopwatch        util.Stopwatch
}
```

### app.UISession (NEW - thin TUI wrapper)
```go
type UISession struct {
    *chat.Session
    renderedMessages []string
    renderedWidth    int
    undoStack        [][]config.Message
}
```

### app.Model
```go
type Model struct {
    textarea        textarea.Model
    viewport        viewport.Model
    mode            Mode
    ready           bool
    width           int
    height          int
    allCommands     []component.PickerItem
    historySearch   ui.HistorySearchOverlay
    modelEntries    []provider.ModelEntry
    pickerPayload   interface{}
    activeComponent component.Component

    Sessions        map[string]*UISession
    ActiveSessionID string

    toolReg         *tools.Registry
    settings        config.Settings
    endpoints       config.EndpointsConfig
    paths           config.Paths
    history         config.History
    workingDir      string
    historyIdx      int
    draft           string
    attachedImage   string
    notification    ui.Notification
    incognito       bool
    sessionSnapshot *UISession
    expanded        bool
}
```

## Files

### config/session.go
- SessionDoc, SessionMeta, SessionInference, InferenceConfig, SessionSkill, SessionTools, SessionSkills
- Message, ContentMetrics, SequenceStat, ToolCallEntry, FileEntry, FileStateEntry
- NewSessionDoc(inf InferenceConfig, sysPrompt, workingDir string, tools, skills []string) SessionDoc
- SaveSessionDoc(p Paths, name string, sd SessionDoc) error
- LoadSessionDoc(p Paths, name string) (SessionDoc, error)
- ListSessions(p Paths) []SessionInfo
- SessionPath(p Paths, name string) string

### chat/session.go (NEW)
- Session struct, SessionConfig
- NewSession(cfg SessionConfig, paths config.Paths) *Session
- LoadSession(sd config.SessionDoc) *Session
- (s *Session) Append(msg config.Message)
- (s *Session) TruncateTo(n int)
- (s *Session) TruncateToUser() (text, image string)
- (s *Session) CancelTruncate() (text, image string, truncated bool)
- (s *Session) HasUserMessage() bool
- (s *Session) TotalInputTokens() int
- (s *Session) TotalOutputTokens() int
- (s *Session) BuildMessages(paths config.Paths) []goai_provider.Message
- (s *Session) GetTools() []tools.Tool
- (s *Session) GetSkills() []string
- (s *Session) CurrentInference() config.InferenceConfig
- (s *Session) SetInference(cfg config.InferenceConfig)
- (s *Session) PushConfigChange(provider, model string, thinking ThinkingConfig)
- (s *Session) PushSystemPromptChange(oldFile, newFile string, paths config.Paths)
- (s *Session) PushThinkingSwitch(thinking ThinkingConfig)
- (s *Session) SetTools(names []string)
- (s *Session) SetSkills(names []string)
- (s *Session) Save(p Paths, name string) error
- (s *Session) Messages() []config.Message

### chat/engine.go (UNCHANGED)
- Engine struct, StreamEvent, ToolCall
- NewEngine(settings, model, thinking) *Engine
- (e *Engine) Stream(ctx, messages, toolDefs) <-chan StreamEvent
- BuildGoAIMessages(paths, settings, messages) []goai_provider.Message
- MarshalToolsJSON(ts []tools.Tool) ([]byte, error)
- RepairArgs(args string) (string, bool)

### chat/stream_state.go (NEW - extracted from app/stream.go)
- streamState struct, partialTool struct, StreamMetrics
- (ss *streamState) reset()
- (ss *streamState) AddTextChunk(text string)
- (ss *streamState) AddThinkChunk(think string)
- (ss *streamState) AddToolCallChunk(delta string)
- (ss *streamState) toStreamingToolCalls() []ui.StreamingToolCall

### chat/loop.go (NEW - extracted from app/stream.go)
- LoopAction: LoopContinue, LoopToolCalls, LoopDone, LoopError
- LoopResult { Action, Output, Error, PartialTools }
- ProcessStreamEvent(s *Session, event StreamEvent) LoopResult
- ExecuteTools(s *Session, toolReg *tools.Registry, paths config.Paths) LoopResult
- SaveAssistantMsg(s *Session, msg config.Message) int
- FlushToolMessage(s *Session, msgIdx int)
- recomputeSequenceStats(messages []config.Message)
- StartStream(s *Session, paths config.Paths, endpoints config.EndpointsConfig) (<-chan StreamEvent, context.CancelFunc)
- RunLoop(s *Session, paths, endpoints, toolReg) string  // full blocking loop, no UI

### chat/thinking.go (UNCHANGED)
### chat/image.go (UNCHANGED)

### app/ui_session.go (NEW - from chat_session.go TUI bits)
- UISession struct
- (u *UISession) LoadFromDoc(sd config.SessionDoc)
- (u *UISession) destroyLastSequence() (text, image string)
- (u *UISession) undoDestroy() (textarea, image string, ok bool)
- (u *UISession) invalidateRenderFrom(i int)
- (u *UISession) invalidateRenderAll()
- (u *UISession) invalidateRenderAt(i int)

### app/stream.go (REWRITTEN - TUI wrapper around chat/loop.go)
- (m *Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd)
- (m *Model) resumeToolExecution() (tea.Model, tea.Cmd)
- (m *Model) startStream() (tea.Model, tea.Cmd)
- (m *Model) sendMessage() (tea.Model, tea.Cmd)
- (m *Model) setStreamMode()
- (m *Model) setChatMode() tea.Cmd
- (m *Model) setAuthMode() tea.Cmd
- (m *Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool

### app/persistence.go (NEW - from session.go)
- (m *Model) saveAs(name string, silent bool) (Model, tea.Cmd)
- (m *Model) autoSave() (Model, tea.Cmd)

### app/tui_session.go (NEW - from session.go)
- (m *Model) clearSession() (Model, tea.Cmd)
- (m *Model) toggleIncognito() (Model, tea.Cmd)
- (m *Model) openSaveSessionPrompt() (Model, tea.Cmd)
- (m *Model) openSessionPicker() (Model, tea.Cmd)

### app/app.go (MODIFIED)
- Model struct (Sessions map[string]*UISession, ActiveSessionID string)
- New(paths, settings, endpoints, history, initialDoc, incognito) Model
- (m Model) Init() tea.Cmd
- (m *Model) setNotification(level, msg)
- (m *Model) clearNotification()
- (m *Model) setComponent(c)
- (m *Model) applyWorkingDir(path string)
- (m *Model) activeSession() *UISession

### app/events.go (UNCHANGED)
### app/update.go (UNCHANGED)
### app/render.go (UNCHANGED)
### app/input.go (UNCHANGED)
### app/modes.go (UNCHANGED)

## Deleted
- app/chat_session.go
- app/session.go

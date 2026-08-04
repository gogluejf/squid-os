# Architecture Refactor Plan

## Why

The current code has three layers tangled together in `app/`:

1. **Domain** — session state (messages, inference config, allowed tools, allowed skills, loaded skill)
2. **Inference loop** — stream processing, tool execution, restart cycle
3. **TUI** — rendering, undo, notifications, auto-save, Bubble Tea, auth prompts

Everything lives in `app/stream.go`, `app/chat_session.go`, and `app/session.go` as methods on `Model` or `chatSession`. This means:

- **CLI/headless** can't run the inference loop — it's coupled to Bubble Tea
- **Subagents** can't have independent sessions — `chatSession` pulls from global `Settings`
- **Reloading a session** doesn't know what tools/skills it was allowed to use — they're not persisted
- **`handleStreamEvent`** mixes stream processing + rendering + saving + auth
- **Tool execution** mixes pure execution, file-state tracking, auth UI, render invalidation, and autosave

## Goals

1. **Pure `chat.Session`** — owns its own persisted doc and pure runtime stream state. No dependency on `Settings`, `UI`, `Model`, Bubble Tea, render cache, or textarea.

2. **Pure `chat/loop.go`** — stream processing and tool execution without UI. `RunLoop()` drives a session to completion. Used by CLI, subagents, and headless.

3. **Thin `app/stream.go`** — TUI wrapper that calls loop functions and handles rendering, saving, notifications, auth components, and mode changes.

4. **Per-session tools/skills availability** — stored on `config.SessionMeta` like inference config. A saved session knows what tools and skills were available/allowed.

5. **Loaded skill stays separate** — `Skill` is the current/pending loaded skill state. `Skills` is the available/allowed skill set. They are different concepts and must not be merged.

6. **Multi-session ready** — `Model` manages `map[string]*UISession`. Each `UISession` wraps a pure `chat.Session` plus TUI-only runtime state. Event routing uses session IDs.

## The Split

| Layer | Package | What it owns | What it doesn't touch |
|-------|---------|-------------|---------------------|
| **Persisted format** | `config` | `SessionDoc`, `SessionMeta`, `InferenceConfig`, `SessionTools`, `SessionSkills`, `SessionSkill` | UI, loop, tools registry |
| **Domain** | `chat` | `Session` (`SessionDoc` + pure `StreamState`), `SessionConfig`, session mutation helpers | Bubble Tea, render cache, notifications, textarea |
| **Loop protocol** | `chat` | `LoopResult`, `ToolExecResult`, `AuthRequest`, `AuthDecision` | Actual UI prompt implementation |
| **Inference loop** | `chat` | `ProcessStreamEvent`, `ExecuteTools`, `StartStream`, `RunLoop` | Rendering, autosave notification, Bubble Tea modes |
| **TUI session wrapper** | `app` | `UISession` = `*chat.Session` + `UIStreamState` + render cache + undo | Disk I/O, pure loop internals |
| **TUI stream wrapper** | `app` | Calls `chat` loop/protocol functions, opens auth prompt, renders, autosaves, updates modes | Core stream/tool execution logic |
| **TUI persistence** | `app` | `saveAs`, `autoSave` orchestration + notification | Picker/session UI |
| **TUI session UI** | `app` | `clearSession`, `toggleIncognito`, `openSessionPicker`, prompts | Core session mutation logic |

## Rule of Thumb

- If it mutates messages, inference config, file state, tools availability, skills availability, loaded skill state, or pure stream accumulation → **`chat/`**
- If it calls provider stream APIs or executes tools without UI → **`chat/`**
- If it defines protocol between pure loop and wrapper (`NeedAuth`, `Done`, `ToolCalls`, etc.) → **`chat/`**
- If it touches `viewport`, `textarea`, `mode`, `notification`, `component`, markdown cache, stopwatch, Bubble Tea commands → **`app/`**
- If it opens an auth prompt → **`app/`**
- If it decides that auth is needed or returns an `AuthRequest` → **`chat/`**
- If it saves to disk through `config.SaveSessionDoc()` → **`chat.Session.Save()`**, with TUI notification handled in **`app/`**

## Important Clarifications

### Skill vs Skills

These are separate concepts:

```go
type SessionMeta struct {
    Skill  SessionSkill  `json:"skill"`            // loaded/current/pending skill state
    Tools  SessionTools  `json:"tools,omitempty"`  // available/allowed tool names
    Skills SessionSkills `json:"skills,omitempty"` // available/allowed skill names
}
```

- `Skill.Current` = currently loaded skill.
- `Skill.Next` = pending loaded skill change for the next user turn.
- `Skills.Current` = skills available/allowed to this session.
- `Tools.Current` = tools available/allowed to this session.
- Do **not** fallback or migrate `Skill.Current` into `Skills.Current`; they mean different things.

### Stream state split

Pure stream state belongs in `chat.Session`:

```go
type Session struct {
    Doc    config.SessionDoc
    Stream StreamState
}
```

UI stream state belongs in `app.UISession`:

```go
type UISession struct {
    *chat.Session
    UIStream UIStreamState
    renderedMessages []string
    renderedWidth    int
    undoStack        [][]config.Message
}
```

### Truncation ownership

Core truncation belongs to `chat.Session` because it mutates message state:

```go
Append()
TruncateTo()
TruncateToUser()
CancelTruncate()
HasUserMessage()
Messages()
TotalInputTokens()
TotalOutputTokens()
```

TUI undo/render behavior belongs to `UISession`:

```go
destroyLastSequence()
undoDestroy()
invalidateRenderFrom()
invalidateRenderAll()
invalidateRenderAt()
```

`CancelTruncate()` may return textarea text/image. CLI/headless can ignore those return values.

### Auth protocol

Auth is part of the pure loop protocol, but the UI prompt is not.

`chat.ExecuteTools()` should return an auth request when it needs user approval. `app/stream.go` opens the Bubble Tea `Question` component, collects a decision, and resumes `ExecuteTools()` with an `AuthDecision`.

## Implementation Order

### Phase 1: Config changes and migration

- [ ] 1.1 Add `SessionTools` and `SessionSkills` structs to `config/session.go`
- [ ] 1.2 Add `Tools SessionTools` and `Skills SessionSkills` fields to `config.SessionMeta`
- [ ] 1.3 Keep `Skill SessionSkill` as loaded/current/pending skill state
- [ ] 1.4 Rename `SessionFile` → `SessionDoc`, `Session` → `SessionMeta` in `config/session.go`
- [ ] 1.5 Rename `NewSessionFile` → `NewSessionDoc`, update signature to take `InferenceConfig` + allowed tools/skills
- [ ] 1.6 Rename `SaveSession` → `SaveSessionDoc`, `LoadSession` → `LoadSessionDoc`
- [ ] 1.7 Add migration for old saved sessions to populate tools/skills availability fields without changing loaded `Skill`
- [ ] 1.8 Update all callers of renamed config functions across the codebase
- [ ] 1.9 `go build ./...` passes

### Phase 2: Split stream state

- [ ] 2.1 Create `chat/metrics.go` — move `StreamMetrics` from `app/metrics.go` to `chat`
- [ ] 2.2 Create `chat/stream_state.go` with pure `StreamState` and `PartialTool`
- [ ] 2.3 Move pure methods to `chat.StreamState`: `Reset`, `AddTextChunk`, `AddThinkChunk`, `AddToolCallChunk`
- [ ] 2.4 Create `app/ui_stream_state.go` with `UIStreamState`
- [ ] 2.5 Keep UI-only fields in `UIStreamState`: stream ID, markdown cache, markdown end, cancel func, stream channel, token count, stopwatch, auth pause state, msg index
- [ ] 2.6 Update `app/stream.go` and render code to use `session.Stream` for pure state and `session.UIStream` for UI state
- [ ] 2.7 `go build ./...` passes

### Phase 3: Create `chat.Session` and `app.UISession`

- [ ] 3.1 Create `chat/session.go` with `Session`, `SessionConfig`, `NewSession()`, `LoadSession()`
- [ ] 3.2 `Session` owns `Doc config.SessionDoc` and `Stream chat.StreamState`
- [ ] 3.3 Implement core session methods: `Append`, `TruncateTo`, `TruncateToUser`, `CancelTruncate`, `HasUserMessage`, `TotalInputTokens`, `TotalOutputTokens`, `Messages()`
- [ ] 3.4 Implement `BuildMessages()` using current pure `chat.BuildAPIMessages(messages)` behavior
- [ ] 3.5 Implement `GetTools()` / `SetTools()` from `Doc.Session.Tools.Current`
- [ ] 3.6 Implement `GetSkills()` / `SetSkills()` from `Doc.Session.Skills.Current` for available/allowed skills only
- [ ] 3.7 Implement loaded skill helpers separately for `Doc.Session.Skill.Current` / `Next`
- [ ] 3.8 Implement inference methods: `CurrentInference`, `SetInference`, `PushConfigChange`, `PushSystemPromptChange`, `PushThinkingSwitch`
- [ ] 3.9 Implement `Save()` — calls `config.SaveSessionDoc()`
- [ ] 3.10 Create `app/ui_session.go` with `UISession` struct embedding `*chat.Session`, plus `UIStreamState`, render cache, undo stack
- [ ] 3.11 Move `LoadFromDoc`, `destroyLastSequence`, `undoDestroy`, `invalidateRender*` to `UISession`
- [ ] 3.12 `go build ./...` passes

### Phase 4: Extract loop from `app/stream.go` in small buildable sub-phases

#### Phase 4A: Move pure stream/metrics usage to `chat`
- [ ] 4A.1 Replace app-local pure stream fields with `chat.StreamState`
- [ ] 4A.2 Keep auth/UI fields in `app.UIStreamState`
- [ ] 4A.3 `go build ./...` passes

#### Phase 4B: Extract stream event accumulation/classification
- [ ] 4B.1 Create `chat/loop.go` with `LoopAction` constants and `LoopResult`
- [ ] 4B.2 Implement `ProcessStreamEvent(s *Session, event StreamEvent) LoopResult`
- [ ] 4B.3 `ProcessStreamEvent` handles token accumulation, thinking accumulation, tool-call delta accumulation, final tool-call classification, done/error classification
- [ ] 4B.4 `ProcessStreamEvent` does not render, autosave, set notifications, set textarea, or open auth prompts
- [ ] 4B.5 `app/stream.go` handles returned result and performs UI work
- [ ] 4B.6 `go build ./...` passes

#### Phase 4C: Extract assistant save helpers
- [ ] 4C.1 Move `SaveAssistantMsg(s *Session, msg config.Message) int` to `chat/loop.go` or `chat/session.go`
- [ ] 4C.2 Move `FlushToolMessage(s *Session, msgIdx int)` to `chat`
- [ ] 4C.3 Move `RecomputeSequenceStats(messages []config.Message)` to `chat`
- [ ] 4C.4 App wrapper calls `UISession.invalidateRenderFrom(msgIdx)` after flush/save
- [ ] 4C.5 `go build ./...` passes

#### Phase 4D: Extract tool execution without auth UI
- [ ] 4D.1 Add `ToolExecAction` and `ToolExecResult` protocol structs in `chat`
- [ ] 4D.2 Extract JSON parsing, argument repair, tool lookup, file-change validation, execution, file-state merge to `chat.ExecuteTools`
- [ ] 4D.3 Pass `*tools.Registry` into `ExecuteTools`
- [ ] 4D.4 Keep render invalidation, autosave, notification, and mode changes in app wrapper
- [ ] 4D.5 `go build ./...` passes

#### Phase 4E: Add auth pause/resume protocol
- [ ] 4E.1 Add `AuthRequest` and `AuthDecision` structs in `chat`
- [ ] 4E.2 `ExecuteTools` returns `ToolExecNeedAuth` with `AuthRequest` when approval is required
- [ ] 4E.3 App wrapper opens Bubble Tea `Question` from the `AuthRequest`
- [ ] 4E.4 App wrapper resumes `ExecuteTools` with `AuthDecision`
- [ ] 4E.5 User-provided auth instructions append a user message and stop remaining tool execution as today
- [ ] 4E.6 `go build ./...` passes

#### Phase 4F: Extract `StartStream`
- [ ] 4F.1 Implement `StartStream(s *Session, endpoints config.EndpointsConfig) (<-chan StreamEvent, context.CancelFunc)`
- [ ] 4F.2 Use session's current inference config, not global settings
- [ ] 4F.3 Use session's allowed/current tools, not global unconditional tools
- [ ] 4F.4 App wrapper stores returned channel/cancel func in `UIStreamState`
- [ ] 4F.5 `go build ./...` passes

#### Phase 4G: Add blocking `RunLoop`
- [ ] 4G.1 Implement `RunLoop(s *Session, endpoints config.EndpointsConfig, toolReg *tools.Registry) string`
- [ ] 4G.2 `RunLoop` uses `StartStream`, `ProcessStreamEvent`, `ExecuteTools`
- [ ] 4G.3 No TUI, no Bubble Tea, no render cache, no notifications
- [ ] 4G.4 `go build ./...` passes

### Phase 5: Split TUI persistence and session UI concerns

- [ ] 5.1 Create `app/persistence.go` — move `saveAs`, `autoSave` from `session.go`
- [ ] 5.2 `saveAs` calls `activeSession().Save()` and handles notification/settings
- [ ] 5.3 Create `app/tui_session.go` — move `clearSession`, `toggleIncognito`, `openSaveSessionPrompt`, `openSessionPicker` from `session.go`
- [ ] 5.4 Update picker preview/load to use `UISession.LoadFromDoc`
- [ ] 5.5 `go build ./...` passes

### Phase 6: Update Model to multi-session

- [ ] 6.1 Replace `session chatSession` with `Sessions map[string]*UISession` + `ActiveSessionID string` in `app/app.go`
- [ ] 6.2 Add `activeSession() *UISession` helper
- [ ] 6.3 Update `New()` to create `map[string]*UISession` with a single active session
- [ ] 6.4 Update all `m.session.xxx` → `m.activeSession().xxx` across app files
- [ ] 6.5 Update `app/events.go` — tag stream events with session ID
- [ ] 6.6 Update `app/update.go` — route stream events and ticks to the correct session
- [ ] 6.7 `go build ./...` passes, TUI works identically

### Phase 7: Cleanup and headless

- [ ] 7.1 Delete old `app/chat_session.go` once all methods are moved
- [ ] 7.2 Delete old `app/session.go` once split files are complete
- [ ] 7.3 Update `headless/headless.go` to use `chat.NewSession()` + `chat.RunLoop()`
- [ ] 7.4 Final `go build ./...` passes
- [ ] 7.5 Full TUI smoke test
- [ ] 7.6 Headless smoke test

## Objects

### config.SessionDoc

```go
type SessionDoc struct {
    Version     int
    Session     SessionMeta
    Messages    []Message
    TotalTokens int
    FileState   map[string]FileStateEntry
}
```

### config.SessionMeta

```go
type SessionMeta struct {
    ID               string
    Title            string
    CreatedAt        string
    UpdatedAt        string
    Inference        SessionInference
    SystemPromptFile string
    WorkingDir       string
    Skill            SessionSkill  `json:"skill"`
    Tools            SessionTools  `json:"tools,omitempty"`
    Skills           SessionSkills `json:"skills,omitempty"`
}
```

### config.SessionSkill

```go
type SessionSkill struct {
    Current string  `json:"current"`
    Next    *string `json:"next"`
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

### chat.Session

```go
type Session struct {
    Doc    config.SessionDoc
    Stream StreamState
}

type SessionConfig struct {
    Provider         string
    Model            string
    Thinking         config.ThinkingConfig
    SystemPromptFile string
    Tools            []string
    Skills           []string
    WorkingDir       string
}
```

### chat.StreamState

```go
type StreamState struct {
    Text          string
    Thinking      string
    InThinking    bool
    Active        bool
    Metrics       StreamMetrics
    PartialTools  []PartialTool
    UserCancelled bool
}
```

### chat.PartialTool

```go
type PartialTool struct {
    ID      string
    Type    string
    Name    string
    Args    string
    Chars   int
    FirstAt time.Time
    DoneAt  time.Time
}
```

### app.UIStreamState

```go
type UIStreamState struct {
    ID               string
    Markdown         string
    MarkdownEnd      int
    CancelFn         context.CancelFunc
    Ch               <-chan chat.StreamEvent
    TokenCount       int
    Stopwatch        util.Stopwatch
    AuthorizationReq *chat.AuthRequest
    PendingDecision  *chat.AuthDecision
    PendingToolIndex int
    MsgIdx           int
}
```

### chat auth protocol

```go
type AuthRequest struct {
    ToolName      string
    Args          map[string]interface{}
    ArgsJSON      string
    DisplayValue  string
    IsDestructive bool
    ToolIndex     int
    MsgIdx        int
}

type AuthDecision struct {
    Approved     bool
    Instructions string
}
```

### chat loop protocol

```go
type LoopAction int

const (
    LoopContinue LoopAction = iota
    LoopToolCalls
    LoopDone
    LoopError
)

type LoopResult struct {
    Action LoopAction
    Error  error
    MsgIdx int
}

type ToolExecAction int

const (
    ToolExecContinue ToolExecAction = iota
    ToolExecNeedAuth
    ToolExecDone
)

type ToolExecResult struct {
    Action           ToolExecAction
    MsgIdx           int
    ToolIndex        int
    AuthRequest      *AuthRequest
    CapturedUserText string
}
```

### app.UISession

```go
type UISession struct {
    *chat.Session
    UIStream UIStreamState
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

- `SessionDoc`, `SessionMeta`, `SessionInference`, `InferenceConfig`, `SessionSkill`, `SessionTools`, `SessionSkills`
- `Message`, `ContentMetrics`, `SequenceStat`, `ToolCallEntry`, `FileEntry`, `FileStateEntry`
- `NewSessionDoc(inf InferenceConfig, sysPrompt, workingDir string, tools, skills []string) SessionDoc`
- `SaveSessionDoc(p Paths, name string, sd SessionDoc) error`
- `LoadSessionDoc(p Paths, name string) (SessionDoc, error)`
- Migration for missing tools/skills availability fields
- `ListSessions(p Paths) []SessionInfo`
- `SessionPath(p Paths, name string) string`

### chat/session.go

- `Session` struct, `SessionConfig`
- `NewSession(cfg SessionConfig, paths config.Paths) *Session`
- `LoadSession(sd config.SessionDoc) *Session`
- `(s *Session) Append(msg config.Message)`
- `(s *Session) TruncateTo(n int)`
- `(s *Session) TruncateToUser() (text, image string)`
- `(s *Session) CancelTruncate() (text, image string, truncated bool)`
- `(s *Session) HasUserMessage() bool`
- `(s *Session) TotalInputTokens() int`
- `(s *Session) TotalOutputTokens() int`
- `(s *Session) BuildMessages() []goai_provider.Message`
- `(s *Session) GetTools() []tools.Tool`
- `(s *Session) SetTools(names []string)`
- `(s *Session) GetSkills() []string` — available/allowed skills
- `(s *Session) SetSkills(names []string)` — available/allowed skills
- `(s *Session) CurrentSkill() string` — loaded skill
- `(s *Session) SetCurrentSkill(name string)` — loaded skill
- `(s *Session) PendingSkill() *string`
- `(s *Session) SetPendingSkill(name string)`
- `(s *Session) CurrentInference() config.InferenceConfig`
- `(s *Session) SetInference(cfg config.InferenceConfig)`
- `(s *Session) PushConfigChange(provider, model string, thinking config.ThinkingConfig)`
- `(s *Session) PushSystemPromptChange(oldFile, newFile string, paths config.Paths)`
- `(s *Session) PushThinkingSwitch(thinking config.ThinkingConfig)`
- `(s *Session) Save(p config.Paths, name string) error`
- `(s *Session) Messages() []config.Message`

### chat/metrics.go

- `StreamMetrics`
- All stream timing/token methods currently in `app/metrics.go`

### chat/stream_state.go

- `StreamState`
- `PartialTool`
- `(ss *StreamState) Reset()`
- `(ss *StreamState) AddTextChunk(text string)`
- `(ss *StreamState) AddThinkChunk(think string)`
- `(ss *StreamState) AddToolCallChunk(delta string)`

### chat/loop.go

- `LoopAction`, `LoopResult`
- `ToolExecAction`, `ToolExecResult`
- `AuthRequest`, `AuthDecision`
- `ProcessStreamEvent(s *Session, event StreamEvent) LoopResult`
- `ExecuteTools(s *Session, toolReg *tools.Registry, decision *AuthDecision) ToolExecResult`
- `SaveAssistantMsg(s *Session, msg config.Message) int`
- `FlushToolMessage(s *Session, msgIdx int)`
- `RecomputeSequenceStats(messages []config.Message)`
- `StartStream(s *Session, endpoints config.EndpointsConfig) (<-chan StreamEvent, context.CancelFunc)`
- `RunLoop(s *Session, endpoints config.EndpointsConfig, toolReg *tools.Registry) string`

### chat/engine.go

- `Engine` struct, `StreamEvent`, `ToolCall`
- `NewEngine(settings, model, thinking) *Engine`
- `(e *Engine) Stream(ctx, messages, toolDefs) <-chan StreamEvent`
- `BuildAPIMessages(messages []config.Message) []goai_provider.Message`
- `MarshalToolsJSON(ts []tools.Tool) ([]byte, error)`
- `RepairArgs(args string) (string, bool)`

### chat/thinking.go

Unchanged.

### chat/image.go

Unchanged.

### app/ui_stream_state.go

- `UIStreamState`
- Reset helpers for UI-only stream fields
- Conversion from `chat.PartialTool` to `[]ui.StreamingToolCall` if rendering needs UI types

### app/ui_session.go

- `UISession` struct
- `(u *UISession) LoadFromDoc(sd config.SessionDoc)`
- `(u *UISession) destroyLastSequence() (text, image string)`
- `(u *UISession) undoDestroy() (textarea, image string, ok bool)`
- `(u *UISession) invalidateRenderFrom(i int)`
- `(u *UISession) invalidateRenderAll()`
- `(u *UISession) invalidateRenderAt(i int)`

### app/stream.go

- `(m *Model) handleStreamEvent(event chat.StreamEvent) (tea.Model, tea.Cmd)` — wrapper around `chat.ProcessStreamEvent`
- `(m *Model) resumeToolExecution(decision *chat.AuthDecision) (tea.Model, tea.Cmd)` — wrapper around `chat.ExecuteTools`
- `(m *Model) startStream() (tea.Model, tea.Cmd)` — wrapper around `chat.StartStream`
- `(m *Model) sendMessage() (tea.Model, tea.Cmd)`
- `(m *Model) setStreamMode()`
- `(m *Model) setChatMode() tea.Cmd`
- `(m *Model) setAuthMode(req *chat.AuthRequest) tea.Cmd`
- `(m *Model) needsAuthorization(tool *tools.Tool, args map[string]interface{}) bool` may move to `chat` or remain as a policy callback until auth extraction is complete

### app/persistence.go

- `(m *Model) saveAs(name string, silent bool) (Model, tea.Cmd)`
- `(m *Model) autoSave() (Model, tea.Cmd)`

### app/tui_session.go

- `(m *Model) clearSession() (Model, tea.Cmd)`
- `(m *Model) toggleIncognito() (Model, tea.Cmd)`
- `(m *Model) openSaveSessionPrompt() (Model, tea.Cmd)`
- `(m *Model) openSessionPicker() (Model, tea.Cmd)`

### app/app.go

- `Model` struct with `Sessions map[string]*UISession`, `ActiveSessionID string`
- `New(paths, settings, endpoints, history, initialDoc, incognito) Model`
- `(m Model) Init() tea.Cmd`
- `(m *Model) setNotification(level, msg)`
- `(m *Model) clearNotification()`
- `(m *Model) setComponent(c)`
- `(m *Model) applyWorkingDir(path string)`
- `(m *Model) activeSession() *UISession`

### app/events.go

- Update stream event/tick messages to carry session ID in Phase 6

### app/update.go

- Route stream event/tick messages to the correct `UISession` in Phase 6

### app/render.go

- Render from `activeSession()` and its `UIStreamState`

### app/input.go

- Replace direct `m.session` access with `activeSession()` during Phase 6

### app/modes.go

Unchanged except for any direct session references.

## Deleted

- `app/chat_session.go` after migration to `chat.Session` + `app.UISession`
- `app/session.go` after migration to `app/persistence.go` + `app/tui_session.go`

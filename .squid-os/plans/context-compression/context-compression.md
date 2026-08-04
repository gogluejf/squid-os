# EPIC: Dynamic Context Compression
Why: Long conversations exceed context windows. Current BuildAPIMessages is a dumb pass-through with no compression, wasting tokens on repeated file reads, skill loads, and stale assistant messages.
Outcomes: Compressed context that fits within window. Stable KV cache by compressing in meaningful chunks. User-visible compression state (summarized content, dedup indicators).

## MILESTONE: 1 - Settings & Config
Pattern: Data Model, Config Migration
Objective: Add summarizer provider/model config and compression thresholds to Settings. Migrate from flat Provider/Model to structured Primary/Summarizer roles.
Success: Settings struct supports primary + summarizer model roles. Compression thresholds configurable. Existing sessions backward compatible.
Diagram: graph TD
    A[Settings struct] --> B[ModelRole: Provider, Model, Thinking]
    A --> C[CompressionSettings: Enabled, Summarizer, Light, Aggressive]
    B --> D[Primary role replaces flat Provider/Model]
    C --> E[LightThreshold: Trigger%, Target%, MinTail]
    C --> F[AggressiveThreshold: Trigger%, Target%, MinTail]
    G[LoadSettings] -->|new format| H[unmarshal directly]
    G -->|old flat format| I[migrate to Primary]

### TASK: 1.1 - Add compression settings to Settings struct
Type: feature
What: Add CompressionSettings struct to config/settings.go with Enabled, Summarizer role, and threshold configs. Migrate existing flat Provider/Model into Primary role.
Why: Settings needs to drive compression behavior. Existing flat fields need to become Primary role for backward compatibility.
Files: ~ internal/config/settings.go
Snippet: type ModelRole struct {\n\tProvider string `json:"provider"`\n\tModel    string `json:"model"`\n\tThinking bool   `json:"thinking,omitempty"`\n}\ntype CompressionThreshold struct {\n\tTrigger         int `json:"trigger"`\n\tTargetReduction int `json:"target_reduction"`\n\tMinTailMessages int `json:"min_tail_messages"`\n}\ntype CompressionSettings struct {\n\tEnabled    bool                 `json:"enabled"`\n\tSummarizer ModelRole            `json:"summarizer"`\n\tLight      CompressionThreshold `json:"light"`\n\tAggressive CompressionThreshold `json:"aggressive"`\n}
Snippet: type Settings struct {\n\tPrimary     ModelRole           `json:"primary"`\n\tCompression CompressionSettings `json:"compression"`\n\t// ... existing fields (MaxHistory, AutoSave, etc.) ...\n}
Snippet: func DefaultSettings() Settings {\n\treturn Settings{\n\t\tPrimary: ModelRole{Provider: "vllm"},\n\t\tCompression: CompressionSettings{\n\t\t\tEnabled: false,\n\t\t\tLight: CompressionThreshold{Trigger: 70, TargetReduction: 20, MinTailMessages: 5},\n\t\t\tAggressive: CompressionThreshold{Trigger: 90, TargetReduction: 20, MinTailMessages: 10},\n\t\t},\n\t\t// ... existing defaults ...\n\t}\n}
Snippet: func LoadSettings(p Paths) Settings {\n\ts := DefaultSettings()\n\tdata, err := os.ReadFile(p.SettingsFile())\n\tif err != nil { return s }\n\tif err := json.Unmarshal(data, &s); err == nil { return s }\n\t// Migrate old flat format\n\tvar legacy struct {\n\t\tProvider string `json:"provider"`\n\t\tModel    string `json:"model"`\n\t\tThinking bool   `json:"thinking"`\n\t}\n\t_ = json.Unmarshal(data, &legacy)\n\ts.Primary = ModelRole{Provider: legacy.Provider, Model: legacy.Model, Thinking: legacy.Thinking}\n\treturn s\n}
Acceptance: DefaultSettings returns Primary with existing defaults and Compression.Enabled = false
Acceptance: LoadSettings migrates old flat Provider/Model/Thinking into Primary
Acceptance: SaveSettings round-trips the new struct without data loss
Acceptance: Existing sessions without compression fields load with defaults
Verification: go build ./...

### TASK: 1.2 - Update all references to flat Provider and Model fields
Type: refactor
What: Update all code that reads settings.Provider and settings.Model to use settings.Primary.Provider and settings.Primary.Model across app, chat, and config packages.
Why: Settings struct changed from flat fields to nested ModelRole. All callers need updating to avoid compile errors.
Files: ~ internal/app/app.go
Files: ~ internal/app/stream.go
Files: ~ internal/chat/engine.go
Files: ~ internal/ui/footer.go
Snippet: // settings.Provider -> settings.Primary.Provider\n// settings.Model -> settings.Primary.Model  \n// settings.Thinking -> settings.Primary.Thinking\n// chat.NewEngine(chatURL, m.settings.Primary.Model, m.settings.Primary.Thinking)\n// modelBasename(m.settings.Primary.Model) in footer\n// ResolveChatURL(m.endpoints, m.settings.Primary.Provider) in stream
Acceptance: All references to flat Provider/Model/Thinking updated
Acceptance: ResolveChatURL uses Primary.Provider
Acceptance: NewEngine uses Primary.Model and Primary.Thinking
Acceptance: Footer display shows correct model name from Primary
Verification: go build ./...

## MILESTONE: 2 - Compression Data Model
Pattern: Value Object, Session Metadata
Objective: Define the data structures for individual message summaries and session-level compressed summaries. Add fields to Message and SessionFile.
Success: Message has optional Summary with reason. SessionFile has optional CompressedSummary. Both persist through save/load.
Diagram: classDiagram
    class Message {
        +string ID
        +string Role
        +string Text
        +*MessageSummary Summary
    }
    class MessageSummary {
        +string Text
        +int Tokens
        +CompressionAction Action
    }
    class CompressionAction {
        +string Reason
    }
    class SessionFile {
        +*CompressedSummary CompressedSummary
    }
    class CompressedSummary {
        +string Context
        +int Tokens
        +string CompressedUpToMsgID
    }
    Message --> MessageSummary
    MessageSummary --> CompressionAction
    SessionFile --> CompressedSummary

### TASK: 2.1 - Add compression fields to Message and SessionFile
Type: feature
What: Add Summary field to Message and CompressedSummary field to SessionFile in config/session.go
Why: Messages need to store individual summaries for UI. SessionFile needs session-level summary for cross-message compression.
Files: ~ internal/config/session.go
Snippet: type CompressionAction struct {\n\tReason string `json:"reason"` // dedup-file, dedup-skill, summarized\n}
Snippet: type MessageSummary struct {\n\tText   string            `json:"text"`\n\tTokens int               `json:"tokens"`\n\tAction CompressionAction `json:"action,omitempty"`\n}
Snippet: type CompressedSummary struct {\n\tContext             string `json:"context"`\n\tTokens              int    `json:"tokens"`\n\tCompressedUpToMsgID string `json:"compressed_up_to_msg_id"`\n}
Snippet: // Added to Message struct:\n\tSummary *MessageSummary `json:"summary,omitempty"`
Snippet: // Added to SessionFile struct:\n\tCompressedSummary *CompressedSummary `json:"compressed_summary,omitempty"`
Snippet: // Added to ToolCallEntry.Execution:\n\tSkipExecutionSummary string `json:"skip_execution_summary,omitempty"`
Acceptance: Message with nil Summary is backward compatible
Acceptance: SessionFile with nil CompressedSummary serializes without the field
Acceptance: Non-nil Summary persists through SaveSession/LoadSession round-trip
Acceptance: Non-nil CompressedSummary persists through SaveSession/LoadSession round-trip
Acceptance: CompressionAction.Reason supports dedup-file, dedup-skill, summarized, session-compressed
Acceptance: ToolCallEntry.Execution has SkipExecutionSummary for deduped tool calls
Acceptance: SkipExecutionSummary persists through SaveSession/LoadSession round-trip
Verification: go build ./...

## MILESTONE: 3 - Compressor Engine
Pattern: Strategy Pattern, Pipeline
Objective: Build the compression engine that runs dedup rules, checks thresholds, and triggers summarization. Plugs into BuildAPIMessages as a pre-processing step.
Success: Compressor.Run() returns filtered results. Dedup for read_file/skill_load works. Token estimation works. Light 70% and aggressive 90% thresholds trigger correctly with proper tail protection.
Diagram: flowchart TD
        A[Compressor.Run messages] --> B[DedupPass]
        B --> C[remove dup read_file keep last per path]
        B --> D[remove dup skill_load keep last per name]
        C --> E[EstimateTokens]
        D --> E
        E --> F{token % of context window}
        F -->|disabled or 0 window| G[pass through no change]
        F -->|"~70-89%"| H[lightCompress]
        F -->|"~90%+"| I[aggressiveCompress]
        H --> J[summarize oldest one-by-one]
        J --> K{target freed or last N?}
        K -->|yes| L[return actions]
        K -->|no| J
        I --> M[find cutoff frees 20% protect last N]
        M --> N[SummarizeBatch all before cutoff]
        N --> O[set SessionSummary]
        O --> L

### TASK: 3.1 - Build summarizer API client
Type: feature
What: Create summarizer.go in compress package that calls the summarizer model endpoint for individual message and batch session summaries.
Why: The compressor needs to call a separate model endpoint to generate summaries using the configured summarizer provider and model.
Files: + internal/compress/summarizer.go
Snippet: type Summarizer struct {\n\tClient  *http.Client\n\tChatURL string\n\tModel   string\n}\nfunc NewSummarizer(endpoints config.EndpointsConfig, role config.ModelRole) *Summarizer
Snippet: func (s *Summarizer) SummarizeMessage(ctx context.Context, role, text string) (string, int, error)\nfunc (s *Summarizer) SummarizeBatch(ctx context.Context, msgs []struct{ Role, Text string }) (string, int, error)
Snippet: // Uses non-streaming chat completion with a system prompt asking for concise summary.\n// 30 second timeout. Returns (summary text, token count, error).
Acceptance: SummarizeMessage makes non-streaming chat completion call to summarizer endpoint
Acceptance: SummarizeMessage returns summary text and approximate token count
Acceptance: SummarizeBatch returns single combined summary
Acceptance: Handles endpoint errors gracefully - returns error for compressor to handle
Acceptance: Uses 30 second timeout
Verification: go build ./...

### TASK: 3.2 - Build compressor engine with dedup, token estimation, and threshold logic
Type: feature
What: Create Compressor struct and Run() method with dedup rules, token estimation, and light/aggressive summarization passes.
Why: Core compression logic that transforms message history before sending to API.
Files: + internal/compress/compressor.go
Files: + internal/compress/dedup.go
Files: + internal/compress/tokenizer.go
Snippet: type Compressor struct {\n\tPaths     config.Paths\n\tEndpoints config.EndpointsConfig\n\tSettings  config.Settings\n}
Snippet: type CompressionResult struct {\n\tActions        []config.CompressionAction\n\tSessionSummary *config.CompressedSummary\n}
Snippet: func (c *Compressor) Run(messages []config.Message) *CompressionResult {\n\tif !c.Settings.Compression.Enabled || c.Settings.ContextWindow == 0 {\n\t\treturn &CompressionResult{}\n\t}\n\tactions := c.dedupToolEntries(messages)\n\ttokens := estimateTokens(messages, actions)  // shared function\n\tpct := tokens * 100 / c.Settings.ContextWindow\n\tif pct >= c.Settings.Compression.Aggressive.Trigger {\n\t\tactions = append(actions, c.aggressiveCompress(messages, actions)...)\n\t} else if pct >= c.Settings.Compression.Light.Trigger {\n\t\tactions = append(actions, c.lightCompress(messages, actions)...)\n\t}\n\treturn &CompressionResult{Actions: actions}\n}
Snippet: // dedupToolEntries: scan assistant msgs with tool calls.\n// For read_file keep last execution per path, earlier get SkipExecutionSummary\n// For skill_load keep last execution per name, earlier get SkipExecutionSummary\n// Does NOT remove messages — just marks tool entries with skip summary.
Snippet: // lightCompress: summarize oldest msgs one by one until target freed or last N reached. Updates msg.Summary in-place.\n// aggressiveCompress: find cutoff that frees 20% tokens respecting MinTailMessages. SummarizeBatch all before cutoff. Sets SessionSummary.
Acceptance: Dedup keeps last read_file per path, marks earlier tool entries with SkipExecutionSummary
Acceptance: Dedup keeps last skill_load per skill name, marks earlier tool entries with SkipExecutionSummary
Acceptance: Does NOT remove messages from sequence — only marks tool entries as skipped
Acceptance: Token estimation uses shared estimateTokens() function
Acceptance: Light compression summarizes oldest until target met or last N reached
Acceptance: Aggressive compression creates CompressedSummary with cutoff respecting MinTailMessages
Acceptance: Uses configured Summarizer Provider/Model
Acceptance: Disabled compression returns empty result (pass-through)
Acceptance: ContextWindow=0 means no compression
Acceptance: Summarizer API failure falls back to uncompressed
Verification: go build ./...

### TASK: 3.2b - Add estimateTokens to chat_session
Type: feature
What: Add estimateTokens() method to chat_session that walks all messages respecting CompressedSummary cutoff, skipped tool entries, and Summary text. Used by both compressor threshold checking and footer display.
Why: Single source of truth for token counting across the compression pipeline and footer display. Footer and compressor use the same logic.
Files: ~ internal/app/chat_session.go
Files: ~ internal/app/render.go
Snippet: // estimateTokens walks all messages and returns total token count\n// accounting for compression state:\n//  - If CompressedSummary exists, skip all msgs before CompressedUpToMsgID,\n//    count the CompressedSummary.Context tokens instead\n//  - For each message: if msg.Summary exists, count Summary.Text tokens\n//  - For each tool call entry: if SkipExecutionSummary set, count placeholder tokens\n//    instead of full execution result\n//  - For other tool calls: count execution tokens normally\nfunc (cs *chatSession) estimateTokens() int {\n\tvar total int\n\tstartIdx := 0\n\tif cs.file.CompressedSummary != nil {\n\t\ttotal += countTokensApprox(cs.file.CompressedSummary.Context)\n\t\t// find index of CompressedUpToMsgID\n\t\tstartIdx = findMsgIdx(cs.file.Messages, cs.file.CompressedSummary.CompressedUpToMsgID)\n\t}\n\tfor i := startIdx; i < len(cs.file.Messages); i++ {\n\t\ttotal += estimateMessageTokens(cs.file.Messages[i])\n\t}\n\treturn total\n}
Snippet: func estimateMessageTokens(msg config.Message) int {\n\tif msg.Summary != nil {\n\t\treturn countTokensApprox(msg.Summary.Text)\n\t}\n\tvar total int\n\tif msg.Role == config.RoleUser {\n\t\ttotal += countTokensApprox(msg.Text)\n\t}\n\tfor _, tc := range msg.ToolCalls {\n\t\tif tc.SkipExecutionSummary != "" {\n\t\t\ttotal += countTokensApprox(tc.SkipExecutionSummary)\n\t\t} else {\n\t\t\ttotal += countTokensApprox(tc.Execution.Result)\n\t\t}\n\t}\n\treturn total\n}
Acceptance: Respects CompressedSummary cutoff — skips msgs before CompressedUpToMsgID
Acceptance: Counts CompressedSummary.Context tokens as replacement for skipped msgs
Acceptance: For msgs with Summary, counts Summary.Text tokens instead of original
Acceptance: For tool entries with SkipExecutionSummary, counts placeholder tokens
Acceptance: For normal tool entries, counts execution result tokens
Acceptance: Used by compressor Run() for threshold percentage calculation
Acceptance: Used by footer buildFooterData() for display token count
Verification: go build ./...

### TASK: 3.3 - Integrate compressor into BuildAPIMessages and session save
Type: refactor
What: Wire Compressor.Run() into BuildAPIMessages. Apply actions to messages. Handle existing CompressedSummary on session load. Persist results on auto-save.
Why: BuildAPIMessages is the single entry point. Compression must happen here. Results must persist across session reloads.
Files: ~ internal/chat/engine.go
Files: ~ internal/app/stream.go
Files: ~ internal/app/chat_session.go
Snippet: func BuildAPIMessages(paths config.Paths, endpoints config.EndpointsConfig, settings config.Settings, session *config.SessionFile, messages []config.Message) []ChatMessage {\n\tvar result *compress.CompressionResult\n\tif settings.Compression.Enabled && settings.ContextWindow > 0 {\n\t\tresult = compress.NewCompressor(paths, endpoints, settings).Run(messages)\n\t\tapplyActions(messages, result.Actions)\n\t}\n\tif session != nil && session.CompressedSummary != nil {\n\t\treturn buildFromSummary(session.CompressedSummary, messages)\n\t}\n\treturn buildMessages(messages)\n}
Snippet: // applyActions: set msg.Summary for each action. Dedup -> Summary with reason. Summarized -> Summary text.\n// buildFromSummary: prepend synthetic msg with CompressedSummary.Context, skip msgs before CompressedUpToMsgID.\n// In stream.go: after compression runs, set m.session.file.CompressedSummary = result.SessionSummary, trigger auto-save.
Acceptance: Disabled compression = unchanged BuildAPIMessages behavior
Acceptance: Enabled dedup removes repeated read_file/skill_load from API output
Acceptance: Summarized messages send Summary.Text to API instead of original
Acceptance: Saved session with CompressedSummary starts from cutoff point
Acceptance: Summaries applied to messages for UI rendering
Acceptance: Compression results persist on auto-save
Verification: go build ./...

## MILESTONE: 4 - UI Rendering
Pattern: Conditional Render, Expand/Collapse
Objective: Render compressed, summarized, and deduped messages with expand/collapse. Update footer token count.
Success: Deduped messages show Not sent indicator + original dimmed on expand. Summarized show summary default + original dimmed on expand. CompressedSummary renders as synthetic message. Footer shows post-compression tokens.
Diagram: graph TD
        A[RenderMessage msg] --> B{msg.Summary != nil?}
        B -->|no| C[render normally]
        B -->|yes| D{Summary.Action.Reason}
        D -->|"dedup-file/dedup-skill"| E[Not sent indicator collapsed]
        D -->|"summarized"| F[Summary.Text collapsed]
        E --> G{expanded?}
        F --> G
        G -->|yes| H[original text dimmed]
        G -->|no| I[show collapsed only]
        J[updateViewportContent] --> K{session.CompressedSummary != nil?}
        K -->|yes| L[skip msgs before CompressedUpToMsgID]
        K -->|no| M[render all messages]
        L --> N[render synthetic compressed summary msg]
        O[Footer] --> P[post-compression token count]
        O --> Q[compression badge if active]

### TASK: 4.1 - Render compressed and summarized messages in UI
Type: feature
What: Update message rendering in ui/message_format.go and ui/message_draw.go to handle Summary fields. Update render.go to skip messages before CompressedSummary cutoff.
Why: User needs to see what was compressed and be able to expand to original. Messages before cutoff must not appear.
Files: ~ internal/ui/message_format.go
Files: ~ internal/ui/message_draw.go
Files: ~ internal/app/render.go
Snippet: // In RenderMessage or renderAssistantMessage:\nif msg.Summary != nil {\n\tswitch msg.Summary.Action.Reason {\n\tcase "dedup-file", "dedup-skill":\n\t\tcollapsed = "Not sent: file superseded / skill already loaded"\n\t\texpanded = original text dimmed\n\tcase "summarized":\n\t\tcollapsed = msg.Summary.Text with Summary label\n\t\texpanded = Summary.Text + original dimmed\n\t}\n}
Snippet: // In render.go updateViewportContent:\n// If session.CompressedSummary != nil, skip messages before CompressedUpToMsgID\n// Render synthetic Compressed: N messages summarized message
Snippet: // Dimmed style: lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(original)
Acceptance: Message with Summary renders summary text in collapsed view
Acceptance: Expanded view shows original text dimmed
Acceptance: Deduped messages show not sent indicator collapsed
Acceptance: Deduped messages show original dimmed on expand
Acceptance: CompressedSummary renders as styled synthetic message
Acceptance: Messages before CompressedUpToMsgID are not rendered
Verification: go build ./...

### TASK: 4.2 - Update footer to reflect post-compression token count
Type: feature
What: Update footer token counting to use post-compression estimate. Show compression indicator when active.
Why: Footer needs to show real token count going to API. Per-message headers stay frozen at generation time.
Files: ~ internal/ui/footer.go
Files: ~ internal/app/render.go
Snippet: // buildFooterData: use session.estimateTokens() for live post-compression token count\n// Per-message header tokens remain frozen at generation time (unchanged)
Snippet: // Optional: show compression indicator in footer when active\n// e.g. 'compress: 2 skipped, 1 summarized' badge
Acceptance: Footer total tokens comes from session.estimateTokens() — same function used by compressor
Acceptance: Per-message header tokens unchanged (frozen at generation time)
Acceptance: Compression indicator visible in footer when active
Verification: go build ./...

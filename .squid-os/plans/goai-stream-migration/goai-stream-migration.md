# EPIC: GoAI Streaming Migration
Why: The current custom adapter and SSE assembly stack is too fragile, especially for Codex multi-tool turns. We need a cleaner streaming architecture that keeps our app and tool execution logic while replacing provider transport and parsing with GoAI.
Outcomes: Engine streams through GoAI, adapter layer is removed, provider factory dispatches to per-provider BuildGoAIModel and ListModels, BuildGoAIMessages replaces API-shaped message building, tool events arrive cleanly for direct entry construction, and provider_setup.go uses only the generic Provider interface. Our rich config.Message history and metadata (token counts, TTFT, execution details) are preserved.

## MILESTONE: 1 - GoAI Engine Spine
Pattern: Factory
Pattern: Port and Adapter
Objective: Replace the adapter-driven engine transport with GoAI streaming while preserving our StreamEvent contract, our message model, and our custom tool execution loop.
Success: Engine no longer depends on internal/chat/adapter, streams through goai.StreamText with MaxSteps(1), and emits text, thinking, tool, error, and done events into the existing app layer. Engine gets its LanguageModel from provider.Lookup(settings).BuildGoAIModel().
Diagram: flowchart LR
A[ProviderSettings] --> B[provider.Lookup]
B --> C[Provider.BuildGoAIModel]
C --> D[provider.LanguageModel]
D --> E[Engine.Stream]
E --> F[goai.StreamText]
F --> G[provider.StreamChunk]
G --> H[StreamEvent]
H --> I[App ToolExecutor]

### TASK: 1.1 - Add GoAI dependency and BuildGoAIModel to Provider interface
Type: refactor
What: Add goai to go.mod and add BuildGoAIModel and ListModels methods to the existing Provider interface in provider.go.
Why: GoAI's core pattern is constructing a provider.LanguageModel per provider. Our existing registry already dispatches to the right provider struct — we just add two methods to the interface. No new registry or factory file needed.
Files: ~ internal/chat/provider/provider.go
Snippet: type Provider interface {\n    // Existing metadata methods\n    Name() string\n    SupportedAuth() []config.AuthMethod\n    StaticModels() []string\n    DefaultBaseURL() string\n    RequiresBaseURL() bool\n\n    // Auth flow — generic, called by provider_setup.go\n    StartDeviceAuth() (string, string, error)\n    PollDeviceAuth() error\n    StartOAuth(redirectURI string) (string, error)\n    FinishOAuth(code, redirectURI string) error\n    GetCredentials() *config.ProviderCreds\n\n    // GoAI integration — new\n    BuildGoAIModel() (provider.LanguageModel, bool, error)\n    ListModels(ctx context.Context) ([]string, error)\n}
Snippet: // NewEngine uses the existing registry:\nfunc NewEngine(settings *config.ProviderSettings, model string, thinking bool) *Engine {\n    p := provider.Lookup(settings.Name, settings)\n    langModel, parseThinking, err := p.BuildGoAIModel()\n    return &Engine{\n        settings:              settings,\n        Model:                 model,\n        Thinking:              thinking,\n        model:                 langModel,\n        parseThinkingFromText: parseThinking,\n    }\n}\n\n// Docs:\n// https://goai.sh/getting-started/quick-start\n// https://goai.sh/api/types.html
Acceptance: goai dependency is added to go.mod.
Acceptance: BuildGoAIModel() (provider.LanguageModel, bool, error) is on the Provider interface.
Acceptance: ListModels(ctx) ([]string, error) is on the Provider interface.
Acceptance: Auth flow methods (StartDeviceAuth, PollDeviceAuth, StartOAuth, FinishOAuth, GetCredentials) are on the Provider interface — provider_setup.go uses only the interface.
Acceptance: No separate registry or factory file — uses existing provider.Lookup(name, settings).
Acceptance: The bool return from BuildGoAIModel indicates whether the provider needs text-level think tag parsing.
Acceptance: Old transport methods (PrepareRequest, GetChatURL, IsExpired, Refresh) are removed from the interface.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.2 - Replace Engine adapter transport with GoAI stream bridge
Type: refactor
What: Rewrite engine streaming to call goai.StreamText with MaxSteps(1) and translate provider.StreamChunk into our StreamEvent model.
Why: Removes custom SSE parsing, request-body shaping, and tool-call delta reconstruction from engine.go. Our app stays in control of the tool execution loop.
Files: ~ internal/chat/engine.go
Snippet: import (\n    "github.com/zendev-sh/goai"\n    "github.com/zendev-sh/goai/provider"\n)\n\ntype Engine struct {\n    settings   *config.ProviderSettings\n    Model      string\n    Thinking   bool\n    model      provider.LanguageModel\n    parseThinkingFromText bool\n}\n\nfunc (e *Engine) Stream(ctx context.Context, messages []provider.Message, toolDefs []tools.Tool) <-chan StreamEvent {\n    // Convert our tools to goai.Tool (no Execute — we handle execution)\n    // Call goai.StreamText(ctx, e.model, goai.WithMessages(messages...), goai.WithTools(goaiTools...), goai.WithMaxSteps(1))\n    // Range stream.Stream() and map chunks to StreamEvent:\n    //   ChunkText -> StreamEvent.Text\n    //   ChunkReasoning -> StreamEvent.Thinking\n    //   ChunkToolCall / ChunkToolCallDelta -> accumulate into StreamEvent.ToolCalls\n    //   ChunkStepFinish -> flush accumulated tool calls, emit done\n    //   ChunkError -> StreamEvent.Error\n    // Track tool call start time for duration measurement\n}\n\n// Docs:\n// https://goai.sh/api/core-functions.html\n// https://goai.sh/concepts/streaming.html
Snippet: type StreamEvent struct {\n    Text          string     // visible delta text\n    Thinking      string     // thinking delta text\n    Done          bool       // stream finished\n    StopReason    string\n    Error         error\n    ToolCalls     []provider.ToolCall\n    ToolCallDelta string     // incremental arg fragment for UI timing\n    ToolDuration  time.Duration // duration from first tool chunk to completion\n}
Acceptance: Engine no longer imports or uses internal/chat/adapter.
Acceptance: Engine emits StreamEvent values from GoAI stream output.
Acceptance: Engine uses MaxSteps(1) so our app remains the sole tool executor.
Acceptance: Tool call deltas (ChunkToolCallStreamStart, ChunkToolCallDelta, ChunkToolCall) are accumulated correctly.
Acceptance: StreamEvent includes tool duration (time from first tool chunk to step finish).
Acceptance: Engine still returns stream errors and done states through StreamEvent.
Acceptance: Context cancellation is properly handled.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.3 - Preserve thinking behavior with provider-aware parsing fallback
Type: bug
What: Keep our current thinking parser for providers whose reasoning arrives inline in text content (like Qwen), while trusting GoAI ChunkReasoning for providers that expose it natively.
Why: Qwen and similar providers emit thinking tags within text content. GoAI's compat provider may return this as ChunkText. Our existing ThinkParser handles the extraction.
Files: ~ internal/chat/engine.go
Snippet: // Each provider's BuildGoAIModel returns (LanguageModel, parseThinkingFromText, error).\n// OpenAI/codex reasoning models return true for parseThinkingFromText.\n// Ollama/vllm/litellm return false.\n\n// In engine stream loop, when receiving ChunkText:\nif e.parseThinkingFromText && evt.Text != "" {\n    result := parser.Process(evt.Text)\n    // Emit result.Text and result.Thinking via StreamEvent\n} else {\n    // Trust GoAI chunk types directly (ChunkReasoning for thinking)\n}\n\n// Docs:\n// https://goai.sh/concepts/streaming.html\n// https://goai.sh/api/types.html
Acceptance: Thinking continues to work for providers that expose reasoning as separate chunks (OpenAI o-series, Claude).
Acceptance: Providers flagged for text parsing still use the local ThinkParser for inline think tags.
Acceptance: The engine keeps a single StreamEvent contract for the app.
Acceptance: No thinking information is lost for any provider.
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 2 - Provider and Message Refactor
Pattern: Anti Corruption Layer
Objective: Each provider implements BuildGoAIModel and ListModels, message building targets GoAI types, adapter package is removed, and provider_setup.go uses only the generic Provider interface.
Success: All 5 active providers implement BuildGoAIModel and ListModels, BuildGoAIMessages replaces BuildAPIMessages, adapter package is gone, and provider_setup.go calls only interface methods.
Diagram: flowchart TD
A[config.Message] --> B[BuildGoAIMessages]
B --> C[provider.Message]
C --> D[goai.WithMessages]
E[TextResult.ResponseMessages] --> F[Merge into config.Message]
G[provider.Lookup settings] --> H[Provider.BuildGoAIModel]
H --> I[provider.LanguageModel]
J[provider.Lookup settings] --> K[Provider.ListModels]
K --> L[http GET with auth from settings]
M[provider_setup.go] --> N[p.StartDeviceAuth etc]
N --> O[Provider interface only no switch]

### TASK: 2.1 - Replace API message builder with GoAI message builder
Type: refactor
What: Rebuild the message converter so it produces provider.Message values for GoAI, and merge ResponseMessages back into our config.Message history on completion.
Why: Our config.Message carries rich metadata (token counts, TTFT, execution files). We keep it. We just need to convert to/from GoAI's provider.Message for the API call.
Files: ~ internal/chat/engine.go
Snippet: import "github.com/zendev-sh/goai/provider"\n\nfunc BuildGoAIMessages(paths config.Paths, settings config.Settings, messages []config.Message) []provider.Message {\n    // Convert system messages -> goai.SystemMessage\n    // Convert user messages -> goai.UserMessage (with image parts if needed)\n    // Convert assistant messages with tool calls -> provider.Message with ToolCall parts\n    // Convert tool result messages -> goai.ToolMessage\n    // Convert synthetic messages -> goai.AssistantMessage\n}\n\n// Docs:\n// https://goai.sh/api/types.html\n// https://goai.sh/api/core-functions.html
Snippet: func mergeResponseMessages(result *goai.TextResult, messages []config.Message) []config.Message {\n    // After stream completes, take result.ResponseMessages and merge them\n    // into our config.Message history, preserving our metadata fields\n    // (token counts, TTFT, execution details already tracked separately)\n}
Acceptance: BuildGoAIMessages produces provider.Message values from config.Message.
Acceptance: System, user, assistant, tool, and synthetic roles all map correctly.
Acceptance: Tool call parts (provider.Part with PartToolCall) and tool result parts are constructed properly.
Acceptance: ResponseMessages from TextResult are merged back into config.Message on completion.
Acceptance: Our config.Message metadata (token counts, TTFT, execution details) is preserved — not lost to GoAI conversion.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.2 - Implement BuildGoAIModel and ListModels on each provider, remove transport methods
Type: refactor
What: Each provider file implements BuildGoAIModel (returns GoAI LanguageModel with correct auth/options) and ListModels (HTTP GET to /v1/models with auth from settings). Remove PrepareRequest, GetChatURL, IsExpired, Refresh. Keep auth flow methods and metadata on the interface.
Why: BuildGoAIModel replaces the adapter's transport role. ListModels consolidates GetModelsURL + PrepareRequest + FetchModels into one method. Auth flow methods stay for provider_setup.go to use generically.
Files: ~ internal/chat/provider/provider.go
Files: ~ internal/chat/provider/openai.go
Files: ~ internal/chat/provider/openai-codex.go
Files: ~ internal/chat/provider/vllm.go
Files: ~ internal/chat/provider/ollama.go
Files: ~ internal/chat/provider/litellm.go
Snippet: // openai.go\nfunc (o *OpenAIProvider) BuildGoAIModel() (provider.LanguageModel, bool, error) {\n    model := o.settings.Model\n    var opts []openai.Option\n    switch o.settings.Credentials.ActiveAuthMethod {\n    case config.AuthAPIKey:\n        opts = append(opts, openai.WithAPIKey(o.settings.Credentials.APIKey))\n    case config.AuthOAuth:\n        // Wrap OAuth refresh in provider.CachedTokenSource\n        ts := provider.CachedTokenSource(func(ctx context.Context) (*provider.Token, error) {\n            // Read token from o.settings.Credentials.OAuth\n            // Refresh via existing logic if expired\n        })\n        opts = append(opts, openai.WithTokenSource(ts))\n    }\n    if o.settings.BaseURL != "" {\n        opts = append(opts, openai.WithBaseURL(o.settings.BaseURL))\n    }\n    return openai.Chat(model, opts...), false, nil\n}\n\nfunc (o *OpenAIProvider) ListModels(ctx context.Context) ([]string, error) {\n    url := o.settings.BaseURL\n    if url == "" { url = "https://api.openai.com" }\n    req, err := http.NewRequestWithContext(ctx, "GET", url+"/v1/models", nil)\n    // Add auth from credentials (API key or OAuth token)\n    // http.Do, parse JSON, return []string\n}\n\n// Docs:\n// https://goai.sh/providers/openai.html\n// https://goai.sh/providers/ollama.html\n// https://goai.sh/providers/vllm.html\n// https://goai.sh/providers/compat.html
Snippet: // openai-codex.go — BuildGoAIModel adds WithHeaders for Originator/User-Agent/Account-Id\n// ollama.go — BuildGoAIModel uses ollama.Chat(model, ollama.WithBaseURL(base+"/v1"))\n// vllm.go — BuildGoAIModel uses vllm.Chat with base URL and optional API key\n// litellm.go — BuildGoAIModel uses compat.Chat with base URL and x-litellm-api-key header\n// Each also implements ListModels with its own URL and auth logic
Acceptance: All 5 providers implement BuildGoAIModel() returning the correct GoAI LanguageModel.
Acceptance: All 5 providers implement ListModels(ctx) that does an authenticated HTTP GET to /v1/models.
Acceptance: PrepareRequest, GetChatURL, IsExpired, Refresh are removed from the Provider interface.
Acceptance: Auth flow methods (StartDeviceAuth, PollDeviceAuth, StartOAuth, FinishOAuth, GetCredentials) remain on the Provider interface.
Acceptance: Metadata methods (Name, SupportedAuth, StaticModels, DefaultBaseURL, RequiresBaseURL) remain.
Acceptance: openai uses openai.Chat with API key or CachedTokenSource for OAuth.
Acceptance: codex uses openai.Chat with extra headers (Originator, User-Agent, Account-Id).
Acceptance: ollama uses ollama.Chat with base URL, no auth.
Acceptance: vllm uses vllm.Chat with base URL and optional API key.
Acceptance: litellm uses compat.Chat with base URL and x-litellm-api-key header.
Acceptance: Reasoning models (codex o-series) return true for parseThinkingFromText.
Acceptance: No dead compile-path remains for removed transport methods.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.3 - Clean provider_setup.go to use only Provider interface
Type: refactor
What: Remove any provider-specific type assertions, switch statements, or direct references to concrete provider types in provider_setup.go. It should only call generic Provider interface methods.
Why: provider_setup.go should work generically for any provider — the registry dispatches to the right implementation. No more special-casing Codex vs OpenAI vs others.
Files: ~ internal/app/provider_setup.go
Files: ~ internal/chat/provider/provider.go
Snippet: // provider_setup.go — now generic\nfunc handleAuthFlow() {\n    p := provider.Lookup(settings.Name, settings)\n    url, code, err := p.StartDeviceAuth()\n    // ... show URL to user ...\n    err = p.PollDeviceAuth()\n    creds := p.GetCredentials()\n    // ... save credentials ...\n}
Snippet: // No switch on provider name, no type assertions to *CodexProvider, *OpenAIProvider, etc.\n// All auth flow goes through the Provider interface.
Acceptance: provider_setup.go contains no switch/if on provider name for auth flow.
Acceptance: provider_setup.go contains no type assertions to concrete provider types.
Acceptance: All auth flow calls (StartDeviceAuth, PollDeviceAuth, StartOAuth, FinishOAuth, GetCredentials) go through the Provider interface.
Acceptance: Works for all 5 active providers without provider-specific branching.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.4 - Delete adapter package and obsolete request-shaping code
Type: refactor
What: Remove the adapter package and all request-body/SSE helper code that only served the old HTTP transport.
Why: We explicitly do not want legacy compatibility layers or dead code after the migration.
Files: rm internal/chat/adapter/adapter.go
Files: rm internal/chat/adapter/chatcompletions.go
Files: rm internal/chat/adapter/codex.go
Files: ~ internal/chat/engine.go
Snippet: // Remove adapter package entirely\n// Remove old request-body builders, SSE parsers, tool-call delta accumulators\n// Keep only GoAI bridge logic and provider-message conversion in engine.go
Acceptance: internal/chat/adapter is fully removed.
Acceptance: No code path still references adapter types or adapter imports.
Acceptance: Request shaping for removed adapter transport is deleted, not just unused.
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 3 - App Stream and Tool Entry Simplification
Pattern: State Simplification
Pattern: Single Source of Truth
Objective: Simplify app stream state so tools are built from clean GoAI chunk events instead of delta-assembled partial tool state, with duration tracking.
Success: Streamed tool events create ToolCallEntry values directly with duration metadata, tool execution still uses our custom logic, and both pending stream state and partial delta-only assembly paths are removed.
Diagram: sequenceDiagram
User->>Engine: prompt
Engine->>GoAI: stream request with MaxSteps 1
GoAI-->>Engine: ChunkText ChunkReasoning
Engine-->>App: StreamEvent with text thinking
GoAI-->>Engine: ChunkToolCallStreamStart ChunkToolCallDelta ChunkToolCall
Engine-->>App: StreamEvent with ToolCalls and duration
App->>App: build ToolCallEntry directly
App->>Tools: execute with local approval logic

### TASK: 3.1 - Build pending tool entries directly from StreamEvent with duration
Type: refactor
What: Change app stream handling so complete tool events create ToolCallEntry values directly from StreamEvent.ToolCalls, including a duration field measured from first tool chunk to step finish.
Why: GoAI gives us clean ChunkToolCall values. No more fragile delta assembly. We just accumulate deltas during the stream and flush on ChunkStepFinish, capturing the time delta.
Files: ~ internal/app/stream.go
Files: ~ internal/ui/message.go
Snippet: // StreamEvent now carries finalized ToolCalls and duration\nfunc (m *Model) handleStreamEvent(event chat.StreamEvent) {\n    // Append text and thinking to current message\n    if event.ToolCalls != nil {\n        // Build ToolCallEntry directly from event.ToolCalls\n        // Store event.ToolDuration in the entry metadata\n    }\n    // Do not build pending partial instruction state\n}
Acceptance: App no longer depends on streamed argument deltas to construct tool entries.
Acceptance: Streamed tool events create ToolCallEntry values directly from complete provider.ToolCall.
Acceptance: ToolCallEntry includes duration from first tool chunk to step finish.
Acceptance: Pending stream state and pending partial instruction assembly are removed from the tool-entry path.
Acceptance: Rendering continues to show pending, success, and error tool states.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.2 - Keep local tool execution loop and timing metrics on clean entries
Type: refactor
What: Retain our custom execution loop but make it consume finalized tool entries with duration metadata rather than transient streamed partial state.
Why: We must keep our approval, validation, and working-dir logic while removing execution dependence on stream-built state.
Files: ~ internal/app/stream.go
Snippet: func (m *Model) resumeToolExecution(entries []config.ToolCallEntry, startIndex int) (tea.Model, tea.Cmd) {\n    // Execute from finalized entries\n    // Keep approval preview validation and working-dir logic\n    // Entries already carry duration from the stream phase\n    // Track additional execution timing (start/end timestamps)\n}
Acceptance: Tool execution still uses local authorization, preview, and validation logic.
Acceptance: Execution reads finalized tool entry arguments.
Acceptance: Duration metrics capture both stream-phase duration (model to tool flush) and execution-phase timing.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.3 - Remove obsolete partial tool assembly state
Type: refactor
What: Delete partial tool assembly fields, delta-only helpers, and toolCallIDToIdx mapping that only existed to reconstruct streamed tool calls from low-level deltas.
Why: GoAI handles the delta accumulation internally and gives us clean ToolCall values. Those states are dead complexity.
Files: ~ internal/app/stream.go
Files: ~ internal/ui/message.go
Files: ~ internal/chat/engine.go
Snippet: // Remove from engine.go:\n// - toolBuffers map\n// - toolCallIDToIdx map\n// - nextSyntheticToolIdx\n// - flushToolCalls function\n\n// Remove from stream.go:\n// - partialTools slices\n// - delta-only assembly helpers\n\n// Remove from message.go:\n// - UI fallback paths for malformed tool assembly state
Acceptance: toolBuffers, toolCallIDToIdx, and delta assembly helpers are removed from engine.go.
Acceptance: partialTools and delta-only assembly helpers are removed from stream.go.
Acceptance: No UI path relies on malformed fallback tool assembly state.
Acceptance: Tool rendering uses the new direct entry path.
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 4 - Model Discovery and Provider Coverage
Pattern: Factory
Pattern: Progressive Delivery
Objective: Wire BuildGoAIModel and ListModels through engine and model scanning for the 5 active providers, then document remaining GoAI providers for future migration.
Success: All 5 active providers work end-to-end through GoAI (streaming + model listing), provider_setup.go uses only the interface, sentinels still work, and remaining 20+ providers are documented for a follow-up plan.
Diagram: flowchart TD
A[provider.Lookup settings] --> B[Provider.BuildGoAIModel]
B -->|openai| C[openai.Chat WithAPIKey or WithTokenSource]
B -->|codex| D[openai.Chat WithHeaders]
B -->|ollama| E[ollama.Chat WithBaseURL]
B -->|vllm| F[vllm.Chat WithBaseURL WithAPIKey]
B -->|litellm| G[compat.Chat WithBaseURL WithHeaders]
H[Provider.ListModels] --> I[http GET with auth from settings]
I --> J[ModelEntry list]
J --> K[Picker with sentinels]

### TASK: 4.1 - Wire model scanning to use Provider.ListModels
Type: feature
What: Update ScanModels to call p.ListModels(ctx) on the provider instance from Lookup, falling back to static models and sentinels on error.
Why: GoAI has no model listing API. ListModels is our own HTTP GET with per-provider auth — consolidated into one method per provider.
Files: ~ internal/chat/models.go
Snippet: func ScanModels(ctx context.Context, settings *config.ProviderSettings) []ModelEntry {\n    p := provider.Lookup(settings.Name, settings)\n    if p == nil {\n        return []ModelEntry{sentinelUnknownProvider}\n    }\n    models, err := p.ListModels(ctx)\n    if err != nil {\n        // Return sentinel: auth failed, unreachable, etc.\n        return []ModelEntry{sentinelForError(err)}\n    }\n    // Merge with p.StaticModels() fallback if empty\n    // Convert to ModelEntry with sentinel entries\n}\n\n// Docs:\n// https://goai.sh/concepts/providers-and-models.html
Acceptance: ScanModels calls provider.Lookup then p.ListModels(ctx).
Acceptance: Auth is applied correctly per provider (API key header, OAuth token, or no auth).
Acceptance: Sentinels for not-configured, auth-failed, and unreachable still work.
Acceptance: Static model fallback from p.StaticModels() is available when ListModels returns nothing.
Acceptance: No provider-specific branching in ScanModels — uses the interface.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 4.2 - Wire BuildGoAIModel and provider_setup.go end-to-end for all 5 active providers
Type: feature
What: Verify engine construction uses provider.Lookup(settings).BuildGoAIModel(), ScanModels uses p.ListModels(), and provider_setup.go uses only generic Provider interface methods — no provider-specific code.
Why: This is the integration verification: all call paths go through the Provider interface and registry, no concrete type references leak into app code.
Files: ~ internal/chat/engine.go
Files: ~ internal/chat/models.go
Files: ~ internal/app/provider_setup.go
Snippet: // engine.go\nfunc NewEngine(settings *config.ProviderSettings, model string, thinking bool) *Engine {\n    p := provider.Lookup(settings.Name, settings)\n    langModel, parseThinking, err := p.BuildGoAIModel()\n    // ...\n}\n\n// provider_setup.go\nfunc handleAuthFlow() {\n    p := provider.Lookup(settings.Name, settings)\n    url, code, err := p.StartDeviceAuth()\n    // ... all through interface\n}\n\n// Docs:\n// https://goai.sh/getting-started/quick-start.html\n// https://goai.sh/concepts/tools.html
Acceptance: Engine construction uses provider.Lookup(settings).BuildGoAIModel() — no adapter, no switch.
Acceptance: Model scanning uses p.ListModels(ctx) — no provider-specific URL building.
Acceptance: provider_setup.go calls only Provider interface methods — no type assertions, no switch on provider name.
Acceptance: All 5 active providers (openai, codex, vllm, ollama, litellm) work end-to-end.
Acceptance: OAuth providers use CachedTokenSource for dynamic token refresh during streaming.
Acceptance: Reasoning models (codex o-series) correctly flag parseThinkingFromText.
Acceptance: No old handwritten transport path remains in any call path.
Verification: cd ~/src/squid-os && go build ./...

### TASK: 4.3 - Document remaining GoAI providers for future migration
Type: doc
What: Document the remaining GoAI-supported providers (Anthropic, Google, Bedrock, Azure, Vertex, Cohere, Mistral, xAI, Groq, DeepSeek, MiniMax, Fireworks, Together, DeepInfra, OpenRouter, Requesty, Perplexity, Cerebras, NVIDIA NIM, RunPod, Cloudflare, FPT Cloud) and their BuildGoAIModel/ ListModels mapping for a follow-up plan.
Why: These 20+ providers follow the same pattern as the first 5. After the initial migration is stable and tested, they can be added via a separate plan using the same interface methods.
Files: + internal/chat/provider/anthropic.go
Files: + internal/chat/provider/google.go
Files: + internal/chat/provider/bedrock.go
Files: + internal/chat/provider/azure.go
Files: + internal/chat/provider/vertex.go
Files: ~ internal/chat/provider/provider.go
Snippet: // Each remaining provider follows the same pattern:\n//\n// func init() {\n//     Register(config.ProviderAnthropic, func(s *config.ProviderSettings) Provider {\n//         return &AnthropicProvider{settings: s}\n//     })\n// }\n//\n// func (a *AnthropicProvider) BuildGoAIModel() (provider.LanguageModel, bool, error) {\n//     model := a.settings.Model\n//     return anthropic.Chat(model, anthropic.WithAPIKey(a.settings.Credentials.APIKey)), false, nil\n// }\n//\n// func (a *AnthropicProvider) ListModels(ctx context.Context) ([]string, error) {\n//     // anthropic does not have /v1/models — return StaticModels() or empty\n// }\n\n// Docs:\n// https://goai.sh/providers/\n// https://goai.sh/providers/anthropic.html\n// https://goai.sh/providers/google.html\n// https://goai.sh/providers/bedrock.html\n// https://goai.sh/providers/azure.html
Snippet: // Deferred to a separate plan after initial migration is tested.\n// 20+ providers to implement following the same interface.
Acceptance: Document lists all 20+ remaining GoAI-supported providers.
Acceptance: Each provider maps to the correct GoAI package (anthropic.Chat, google.Chat, bedrock.Chat, etc.).
Acceptance: Auth mapping is documented per provider (API key, AWS creds, OAuth, etc.).
Acceptance: ListModels approach is noted (some providers like Anthropic have no model listing endpoint — fall back to StaticModels).
Acceptance: The work is deferred to a separate plan after this migration is stable.
Verification: cd ~/src/squid-os && go build ./...

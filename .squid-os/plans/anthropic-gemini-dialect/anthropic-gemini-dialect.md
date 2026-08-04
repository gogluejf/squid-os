# EPIC: Anthropic and Gemini Dialect Support
Why: Add native API support for Anthropic Claude and Google Gemini providers. Currently only OpenAI-compatible dialects are implemented. Both providers use fundamentally different request/response formats requiring dedicated adapters and providers.
Outcomes: Users can authenticate with Claude and Gemini API keys, select models from both providers, stream responses with tool calling support, and think/reasoning mode works where applicable.

## MILESTONE: 1 - Anthropic Provider
Pattern: Provider Factory, Port & Adapter
Objective: Implement the Anthropic provider registration, authentication, and URL resolution.
Success: Anthropic provider is registered, authenticates via API key, and resolves correct chat and models URLs.
Diagram: graph TD
    A[init() registers anthropic provider] --> B[Provider factory creates AnthropicProvider]
    B --> C[PrepareRequest sets x-api-key header]
    B --> D[Dialect returns DialectAnthropic]
    B --> E[GetChatURL returns messages endpoint]
    B --> F[StaticModels returns Claude model list]

### TASK: 1.1 - Add Anthropic provider constants and config
Type: feature
What: Add ProviderAnthropic constant and anthropic provider struct to config and provider packages.
Why: Establishes the provider name in the registry and enables users to configure Anthropic as a provider.
Files: + internal/config/endpoints.go
Files: + internal/chat/provider/anthropic.go
Snippet: const ProviderAnthropic = "anthropic"\n\ntype AnthropicProvider struct {\n    settings *config.ProviderSettings\n}\n\nfunc (a *AnthropicProvider) Dialect() config.Dialect {\n    return config.DialectAnthropic\n}\n\nfunc (a *AnthropicProvider) SupportedAuth() []config.AuthMethod {\n    return []config.AuthMethod{config.AuthAPIKey}\n}\n\nfunc (a *AnthropicProvider) DefaultBaseURL() string {\n    return "https://api.anthropic.com"\n}\n\nfunc (a *AnthropicProvider) GetChatURL() string {\n    base := a.settings.BaseURL\n    if base == "" { base = a.DefaultBaseURL() }\n    return base + "/v1/messages"\n}
Acceptance: ProviderAnthropic constant is defined in config package
Acceptance: AnthropicProvider struct implements all Provider interface methods
Acceptance: Default base URL is https://api.anthropic.com
Acceptance: Chat URL resolves to /v1/messages
Acceptance: Dialect returns DialectAnthropic
Acceptance: SupportedAuth returns API key method
Acceptance: RequiresBaseURL returns false for the known cloud provider
Verification: go build ./...
Verification: go test ./internal/chat/provider/...

### TASK: 1.2 - Implement Anthropic provider auth and models
Type: feature
What: Add API key authentication via x-api-key header and anthropic-version header, plus Claude model list and PrepareRequest logic.
Why: Anthropic requires its own header-based authentication and versioning for request signing.
Files: ~ internal/chat/provider/anthropic.go
Snippet: // PrepareRequest sets Anthropic-specific headers\nfunc (a *AnthropicProvider) PrepareRequest(req *http.Request) error {\n    if a.settings.Credentials != nil && a.settings.Credentials.APIKey != "" {\n        req.Header.Set("x-api-key", a.settings.Credentials.APIKey)\n    }\n    req.Header.Set("anthropic-version", "2023-06-01")\n    req.Header.Set("anthropic-dangerous-direct-browser-access", "true")\n    return nil\n}\n\nfunc (a *AnthropicProvider) StaticModels() []string {\n    return []string{\n        "claude-opus-4-20250514",\n        "claude-sonnet-4-20250514",\n        "claude-sonnet-4-5-20250929",\n        "claude-haiku-3-5-20241022",\n        "claude-opus-4-20250514",\n    }\n}
Acceptance: PrepareRequest sets x-api-key header from credentials
Acceptance: PrepareRequest sets anthropic-version header
Acceptance: StaticModels returns known Claude model IDs
Acceptance: IsExpired returns false since API key auth does not expire
Acceptance: Refresh returns nil for API key providers
Verification: go build ./...
Verification: go test ./internal/chat/provider/...

## MILESTONE: 2 - Anthropic Adapter
Pattern: Adapter Pattern, Vertical Slice
Objective: Implement the Anthropic API adapter that translates internal messages/tools into Claude request format and parses Claude SSE events.
Success: Claude requests serialize correctly with system prompt, messages, tools, and thinking. SSE events parse text deltas, thinking deltas, tool call deltas, and completion.
Diagram: graph TD
    A[BuildBody] --> B[Extract system prompt from messages]
    A --> C[Convert messages to Anthropic format]
    A --> D[Convert tools to Anthropic function tools]
    A --> E[Set thinking block if enabled]
    A --> F[Return JSON body]
    G[ParseSSE] --> H{Event type}
    H --> I[message_start]
    H --> J[content_block_start]
    H --> K[content_block_delta]
    H --> L[content_block_stop]
    H --> M[message_delta]
    H --> N[message_stop]

### TASK: 2.1 - Implement Anthropic adapter BuildBody
Type: feature
What: Create AnthropicAdapter with BuildBody that converts internal messages to Anthropic messages API format with system prompt, messages, tools, and thinking.
Why: Anthropic uses a distinct request format: system is a top-level string, messages use role/content with text blocks, and tools are separate from messages.
Files: + internal/chat/adapter/anthropic.go
Snippet: type AnthropicAdapter struct {}\n\n// Anthropic request structure\ntype anthropicRequest struct {\n    Model    string                   \n    System   interface{}              \n    Messages []anthropicMessage       \n    Tools    []anthropicTool          \n    MaxTokens int                     \n    Thinking *anthropicThinking       \n    Stream   bool                     \n}\n\ntype anthropicMessage struct {\n    Role    string           \n    Content interface{}      \n}\n\ntype anthropicTextBlock struct {\n    Type string \n    Text string \n}\n\ntype anthropicToolUseBlock struct {\n    Type     string \n    ID       string \n    Name     string \n    Input    map[string]interface{} \n}\n\ntype anthropicToolResultBlock struct {\n    Type       string \n    ToolUseID  string \n    Content    string \n}\n\n// Convert internal roles: system -> top-level, user/assistant stay, tool -> user with tool_result
Acceptance: BuildBody extracts system prompt from system role messages and places it in the top-level system field
Acceptance: BuildBody converts user messages to role=user with text blocks
Acceptance: BuildBody converts assistant messages with tool calls to tool_use blocks
Acceptance: BuildBody converts tool result messages to user role with tool_result blocks
Acceptance: BuildBody converts tools to Anthropic function tool format with name, description, input_schema
Acceptance: BuildBody sets thinking block with budget when thinking is enabled
Acceptance: BuildBody sets max_tokens (e.g. 8192 for claude)
Acceptance: BuildBody sets stream to true
Verification: go build ./...
Verification: go test ./internal/chat/adapter/...

### TASK: 2.2 - Implement Anthropic adapter ParseSSE
Type: feature
What: Create ParseSSE for Anthropic that handles message_start, content_block_start, content_block_delta, content_block_stop, message_delta, and message_stop SSE events.
Why: Anthropic streams events with distinct types for text, thinking, and tool_use blocks that need mapping to AdapterEvent.
Files: ~ internal/chat/adapter/anthropic.go
Snippet: func (a *AnthropicAdapter) ParseSSE(payload string) *AdapterEvent {\n    // Parse event type from top-level type field\n    // message_start: extract stop_reason for done signal\n    // content_block_start: detect text vs tool_use vs thinking blocks\n    // content_block_delta: extract text delta, thinking delta, or tool_use arg delta\n    // content_block_stop: end of current block\n    // message_delta: extract stop_reason\n    // message_stop: done signal\n}
Acceptance: ParseSSE handles message_stop event and returns Done=true
Acceptance: ParseSSE handles content_block_delta for text content and returns Text delta
Acceptance: ParseSSE handles content_block_delta for thinking blocks and returns Thinking delta
Acceptance: ParseSSE handles content_block_start for tool_use and initializes ToolCallName/ToolCallID
Acceptance: ParseSSE handles content_block_delta for tool_use input and returns ToolCallDelta
Acceptance: ParseSSE handles message_delta stop_reason and returns appropriate stop reason
Acceptance: ParseSSE returns nil for intermediate/skip events (ping, message_start without data, etc.)
Verification: go build ./...
Verification: go test ./internal/chat/adapter/...

### TASK: 2.3 - Wire Anthropic dialect into engine
Type: feature
What: Add DialectAnthropic case in engine.go NewEngine to instantiate AnthropicAdapter.
Why: The engine needs to route Anthropic dialect requests to the correct adapter.
Files: ~ internal/chat/engine.go
Snippet: var a adapter.APIAdapter\nswitch p.Dialect() {\ncase config.DialectOpenAICodex:\n    a = &adapter.CodexAdapter{}\ncase config.DialectAnthropic:\n    a = &adapter.AnthropicAdapter{}\ndefault:\n    a = &adapter.ChatCompletionsAdapter{}\n}
Acceptance: Engine selects AnthropicAdapter when provider Dialect returns DialectAnthropic
Acceptance: Existing OpenAI-compatible and Codex adapters remain unaffected
Acceptance: go build succeeds with the new case
Verification: go build ./...

## MILESTONE: 3 - Gemini Provider
Pattern: Provider Factory, Port & Adapter
Objective: Implement the Gemini provider registration, API key authentication, and URL resolution for Google's Generative Language API.
Success: Gemini provider is registered, authenticates via API key, and resolves correct generateContent and models URLs.
Diagram: graph TD
    A[init() registers gemini provider] --> B[Provider factory creates GeminiProvider]
    B --> C[PrepareRequest appends key query param]
    B --> D[Dialect returns DialectGemini]
    B --> E[GetChatURL returns generateContent endpoint]
    B --> F[StaticModels returns Gemini model list]

### TASK: 3.1 - Add Gemini provider constants and config
Type: feature
What: Add ProviderGemini constant and gemini provider struct with API key auth and URL resolution.
Why: Establishes the Gemini provider in the registry and enables users to configure Gemini as a provider with native API support.
Files: + internal/config/endpoints.go
Files: + internal/chat/provider/gemini.go
Snippet: const ProviderGemini = "gemini"\n\ntype GeminiProvider struct {\n    settings *config.ProviderSettings\n}\n\nfunc (g *GeminiProvider) Dialect() config.Dialect {\n    return config.DialectGemini\n}\n\nfunc (g *GeminiProvider) SupportedAuth() []config.AuthMethod {\n    return []config.AuthMethod{config.AuthAPIKey}\n}\n\nfunc (g *GeminiProvider) DefaultBaseURL() string {\n    return "https://generativelanguage.googleapis.com"\n}\n\nfunc (g *GeminiProvider) GetChatURL() string {\n    // Returns v1beta/models/MODEL:generateContent?key=API_KEY with streaming\n}
Acceptance: ProviderGemini constant is defined in config package
Acceptance: GeminiProvider struct implements all Provider interface methods
Acceptance: Default base URL is https://generativelanguage.googleapis.com
Acceptance: Chat URL resolves to the generateContent endpoint with api version and streaming=true
Acceptance: Dialect returns DialectGemini
Acceptance: SupportedAuth returns API key method
Acceptance: RequiresBaseURL returns false for the known cloud provider
Acceptance: StaticModels returns known Gemini model IDs
Verification: go build ./...
Verification: go test ./internal/chat/provider/...

### TASK: 3.2 - Implement Gemini provider auth and models
Type: feature
What: Add API key authentication via query parameter and Gemini model list with PrepareRequest logic.
Why: Gemini authenticates via ?key=API_KEY query parameter appended to the URL, not via headers.
Files: ~ internal/chat/provider/gemini.go
Snippet: func (g *GeminiProvider) PrepareRequest(req *http.Request) error {\n    // Gemini uses query param auth - append key to URL\n    if g.settings.Credentials != nil && g.settings.Credentials.APIKey != "" {\n        u, _ := url.Parse(req.URL.String())\n        q := u.Query()\n        q.Set("key", g.settings.Credentials.APIKey)\n        u.RawQuery = q.Encode()\n        req.URL = u\n    }\n    return nil\n}\n\nfunc (g *GeminiProvider) StaticModels() []string {\n    return []string{\n        "gemini-2.5-pro",\n        "gemini-2.5-flash",\n        "gemini-2.5-flash-lite",\n        "gemini-2.0-flash",\n    }\n}
Acceptance: PrepareRequest appends ?key=API_KEY query parameter to the request URL
Acceptance: StaticModels returns known Gemini model IDs
Acceptance: IsExpired returns false for API key auth
Acceptance: Refresh returns nil for API key providers
Acceptance: Models URL returns the list models endpoint
Verification: go build ./...
Verification: go test ./internal/chat/provider/...

## MILESTONE: 4 - Gemini Adapter
Pattern: Adapter Pattern, Vertical Slice
Objective: Implement the Gemini API adapter that translates internal messages/tools into Gemini generateContent format and parses streaming response events.
Success: Gemini requests serialize correctly with contents, tools, and system instructions. Streaming events parse text deltas, function calls, and completion.
Diagram: graph TD
    A[BuildBody] --> B[Extract system instructions from messages]
    A --> C[Convert messages to Gemini contents with parts]
    A --> D[Convert tools to Gemini function_declarations]
    A --> E[Set thinking config if enabled]
    A --> F[Return JSON body]
    G[ParseSSE] --> H{Event structure}
    H --> I[candidates with text parts]
    H --> J[candidates with functionCall parts]
    H --> K[usageMetadata for token counts]

### TASK: 4.1 - Implement Gemini adapter BuildBody
Type: feature
What: Create GeminiAdapter with BuildBody that converts internal messages to Gemini generateContent format with system instructions, contents, tools, and thinking config.
Why: Gemini uses a distinct request format: system_instruction is separate from contents, messages map to parts with role alternation, and tools use function_declarations.
Files: + internal/chat/adapter/gemini.go
Snippet: type GeminiAdapter struct {}\n\n// Gemini request structure for generateContent\ntype geminiRequest struct {\n    Contents         []geminiContent     \n    Tools            []geminiTool        \n    SystemInstruction *geminiContent     \n    GenerationConfig *geminiGenConfig    \n}\n\ntype geminiContent struct {\n    Role  string           \n    Parts []geminiPart     \n}\n\ntype geminiPart struct {\n    Text         string                 \n    FunctionCall *geminiFunctionCall    \n    FunctionResponse *geminiFuncResp   \n}\n\n// Convert internal roles: system -> system_instruction, user/assistant -> role user/model, tool -> functionResponse
Acceptance: BuildBody extracts system prompt into system_instruction content
Acceptance: BuildBody converts user messages to role=user with text parts
Acceptance: BuildBody converts assistant messages with tool calls to functionCall parts
Acceptance: BuildBody converts tool result messages to functionResponse parts with role=model
Acceptance: BuildBody converts tools to Gemini function_declarations with name, description, parameters
Acceptance: BuildBody sets thinking_config when thinking is enabled
Acceptance: BuildBody handles Gemini role convention (user vs model, not assistant)
Verification: go build ./...
Verification: go test ./internal/chat/adapter/...

### TASK: 4.2 - Implement Gemini adapter ParseSSE
Type: feature
What: Create ParseSSE for Gemini that handles the generateContent streaming response with candidates containing text parts and functionCall parts.
Why: Gemini streaming returns a single JSON object per SSE chunk with candidates containing incremental text or function calls.
Files: ~ internal/chat/adapter/gemini.go
Snippet: func (g *GeminiAdapter) ParseSSE(payload string) *AdapterEvent {\n    // Each SSE chunk is a complete JSON object\n    // with candidates array containing content with parts\n    // Parse text parts -> Text delta\n    // Parse functionCall parts -> ToolCallName, ToolCallDelta\n    // Parse usageMetadata with finishReason -> Done event\n}
Acceptance: ParseSSE handles text parts in candidates and returns Text delta
Acceptance: ParseSSE handles functionCall parts and returns ToolCallName, ToolCallDelta with arguments
Acceptance: ParseSSE detects finishReason and returns Done with stop reason
Acceptance: ParseSSE handles multiple parts in a single chunk
Acceptance: ParseSSE returns nil for chunks with no meaningful content (usage-only)
Verification: go build ./...
Verification: go test ./internal/chat/adapter/...

### TASK: 4.3 - Wire Gemini dialect into engine
Type: feature
What: Add DialectGemini case in engine.go NewEngine to instantiate GeminiAdapter.
Why: The engine needs to route Gemini dialect requests to the correct adapter.
Files: ~ internal/chat/engine.go
Snippet: var a adapter.APIAdapter\nswitch p.Dialect() {\ncase config.DialectOpenAICodex:\n    a = &adapter.CodexAdapter{}\ncase config.DialectAnthropic:\n    a = &adapter.AnthropicAdapter{}\ncase config.DialectGemini:\n    a = &adapter.GeminiAdapter{}\ndefault:\n    a = &adapter.ChatCompletionsAdapter{}\n}
Acceptance: Engine selects GeminiAdapter when provider Dialect returns DialectGemini
Acceptance: Existing OpenAI-compatible, Codex, and Anthropic adapters remain unaffected
Acceptance: go build succeeds with the new case
Verification: go build ./...

## MILESTONE: 5 - Integration and Testing
Pattern: End-to-End Verification
Objective: Verify both providers integrate correctly with the engine, UI model picker, and endpoint configuration.
Success: Both providers appear in the model picker, accept API key configuration, and can be selected for chat sessions.
Diagram: graph TD
    A[User configures API key] --> B[Provider settings saved to endpoints.json]
    B --> C[Provider lookup creates instance]
    C --> D[Dialect determines adapter]
    D --> E[Engine uses adapter for request/response]
    E --> F[SSE stream parsed correctly]
    F --> G[Text and tool calls delivered to UI]

### TASK: 5.1 - Add unit tests for Anthropic and Gemini adapters
Type: test
What: Add unit tests for both AnthropicAdapter and GeminiAdapter covering BuildBody and ParseSSE with representative payloads.
Why: Ensures correctness of request serialization and response parsing for both new dialects before live testing.
Files: + internal/chat/adapter/anthropic_test.go
Files: + internal/chat/adapter/gemini_test.go
Snippet: // TestAnthropicAdapterBuildBody tests system extraction, user/assistant/tool messages, tools, and thinking\n// TestAnthropicAdapterParseSSE tests text delta, thinking delta, tool_use start/delta, and message_stop\n// TestGeminiAdapterBuildBody tests system_instruction, user/model contents, functionCall/functionResponse parts\n// TestGeminiAdapterParseSSE tests text parts, functionCall parts, and finishReason detection
Acceptance: BuildBody test covers system prompt extraction for both adapters
Acceptance: BuildBody test covers user message conversion
Acceptance: BuildBody test covers assistant message with tool calls conversion
Acceptance: BuildBody test covers tool result message conversion
Acceptance: BuildBody test covers tool definition conversion
Acceptance: BuildBody test covers thinking mode toggle
Acceptance: ParseSSE test covers text streaming
Acceptance: ParseSSE test covers tool call streaming
Acceptance: ParseSSE test covers done/stop detection
Verification: go test ./internal/chat/adapter/... -v

### TASK: 5.2 - Add unit tests for Anthropic and Gemini providers
Type: test
What: Add unit tests for both provider structs covering auth, URL resolution, and metadata methods.
Why: Validates provider contract compliance and correct URL/header behavior.
Files: + internal/chat/provider/anthropic_test.go
Files: + internal/chat/provider/gemini_test.go
Snippet: // TestAnthropicProviderAuth verifies x-api-key header and anthropic-version header\n// TestAnthropicProviderURLs verifies GetChatURL and GetModelsURL resolution\n// TestGeminiProviderAuth verifies query parameter auth\n// TestGeminiProviderURLs verifies generateContent URL with streaming\n// TestIsConfigured verifies credential validation for both providers
Acceptance: Anthropic provider test verifies API key header is set correctly
Acceptance: Anthropic provider test verifies anthropic-version header
Acceptance: Anthropic provider test verifies chat URL resolution
Acceptance: Gemini provider test verifies API key query param is appended
Acceptance: Gemini provider test verifies generateContent URL resolution
Acceptance: Both providers return correct dialect, supported auth, and static models
Verification: go test ./internal/chat/provider/... -v

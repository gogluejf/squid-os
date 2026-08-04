# GoAI SDK Reference

## Install

```bash
go get github.com/zendev-sh/goai@latest
```

Requires Go 1.25+.

## Core Imports

```go
import (
    "github.com/zendev-sh/goai"
    "github.com/zendev-sh/goai/provider"
    "github.com/zendev-sh/goai/provider/openai"
    "github.com/zendev-sh/goai/provider/ollama"
    "github.com/zendev-sh/goai/provider/vllm"
    "github.com/zendev-sh/goai/provider/compat"
)
```

## Core Functions

### StreamText

```go
func StreamText(ctx context.Context, model provider.LanguageModel, opts ...Option) (*TextStream, error)
```

Returns a `*TextStream` with three consumption modes:

- `stream.Stream()` → `<-chan provider.StreamChunk` — all chunk types
- `stream.TextStream()` → `<-chan string` — text only
- `stream.Result()` → `*TextResult` — blocks until complete
- `stream.Err()` → `error` — check after consuming

**MaxSteps:** Default is 1 (single model step). With `MaxSteps(1)`, tool calls are surfaced in the stream/result for you to handle. With `MaxSteps(2+)`, GoAI auto-loops.

```go
stream, err := goai.StreamText(ctx, model,
    goai.WithMessages(messages...),
    goai.WithTools(tools...),
    goai.WithMaxSteps(1),
)
for chunk := range stream.Stream() {
    switch chunk.Type {
    case provider.ChunkText:
        fmt.Print(chunk.Text)
    case provider.ChunkReasoning:
        fmt.Print(chunk.Text)
    case provider.ChunkToolCall:
        fmt.Printf("Tool: %s(%s)\n", chunk.ToolName, chunk.ToolInput)
    case provider.ChunkStepFinish:
        fmt.Println("[step done]")
    }
}
if err := stream.Err(); err != nil { /* handle */ }
result := stream.Result()
```

## Chunk Types

```go
const (
    ChunkText                StreamChunkType = "text"
    ChunkReasoning           StreamChunkType = "reasoning"
    ChunkToolCall            StreamChunkType = "tool_call"
    ChunkToolCallDelta       StreamChunkType = "tool_call_delta"
    ChunkToolCallStreamStart StreamChunkType = "tool_call_streaming_start"
    ChunkToolResult          StreamChunkType = "tool_result"
    ChunkStepFinish          StreamChunkType = "step_finish"
    ChunkFinish              StreamChunkType = "finish"
    ChunkError               StreamChunkType = "error"
)
```

```go
type StreamChunk struct {
    Type         StreamChunkType
    Text         string           // ChunkText, ChunkReasoning
    ToolCallID   string           // ChunkToolCall, ChunkToolCallStreamStart
    ToolName     string           // ChunkToolCall, ChunkToolCallStreamStart
    ToolInput    string           // ChunkToolCall (complete), ChunkToolCallDelta (incremental)
    FinishReason FinishReason     // ChunkStepFinish, ChunkFinish
    Usage        Usage            // ChunkFinish, ChunkStepFinish
    Error        error            // ChunkError
    Response     ResponseMetadata // ChunkFinish
    Metadata     map[string]any
    StoppedBy    StopCause
}
```

## TextResult

```go
type TextResult struct {
    Text             string
    ToolCalls        []provider.ToolCall
    Steps            []StepResult
    TotalUsage       provider.Usage
    FinishReason     provider.FinishReason
    Response         provider.ResponseMetadata
    ProviderMetadata map[string]map[string]any
    Sources          []provider.Source
    StepsExhausted   bool
    ResponseMessages []provider.Message  // assistant + tool messages for multi-turn
}
```

## Tool

```go
type Tool struct {
    Name                   string
    Description            string
    InputSchema            json.RawMessage
    ProviderDefinedType    string
    ProviderDefinedOptions map[string]any
    Execute                func(ctx context.Context, input json.RawMessage) (string, error)
}
```

When `Execute` is nil and `MaxSteps(1)`, the model's tool calls appear in `result.ToolCalls` for you to handle.

## Message Types

```go
type Message struct {
    Role            Role
    Content         []Part
    ProviderOptions map[string]any
}

type Part struct {
    Type            PartType
    Text            string          // PartText, PartReasoning
    URL             string          // PartImage
    ToolCallID      string          // PartToolCall
    ToolName        string          // PartToolCall
    ToolInput       json.RawMessage // PartToolCall
    ToolOutput      string          // PartToolResult
    CacheControl    string
    Detail          string
    MediaType       string
    Filename        string
    ProviderOptions map[string]any
}

type PartType string
const (
    PartText       PartType = "text"
    PartReasoning  PartType = "reasoning"
    PartImage      PartType = "image"
    PartToolCall   PartType = "tool-call"
    PartToolResult PartType = "tool-result"
    PartFile       PartType = "file"
)

type Role string
const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)
```

### Message Builders

```go
goai.SystemMessage(text string) provider.Message
goai.UserMessage(text string) provider.Message
goai.AssistantMessage(text string) provider.Message
goai.ToolMessage(toolCallID, toolName, output string) provider.Message
```

## Options

```go
goai.WithSystem(s string)          // system prompt
goai.WithPrompt(s string)          // user message
goai.WithMessages(msgs ...provider.Message)  // conversation history
goai.WithTools(tools ...Tool)      // available tools
goai.WithMaxSteps(n int)           // max tool loop steps (default 1)
goai.WithMaxOutputTokens(n int)    // response length limit
goai.WithTemperature(t float64)    // randomness
goai.WithToolChoice(tc string)     // "auto", "none", "required", or tool name
goai.WithHeaders(h map[string]string)
goai.WithProviderOptions(opts map[string]any)
goai.WithMaxRetries(n int)         // default 2
goai.WithTimeout(d time.Duration)
```

## Authentication

### Static API Key

```go
model := openai.Chat("gpt-4o", openai.WithAPIKey("sk-..."))
```

### TokenSource (dynamic / OAuth)

```go
type TokenSource interface {
    Token(ctx context.Context) (string, error)
}

ts := provider.CachedTokenSource(func(ctx context.Context) (*provider.Token, error) {
    tok, err := fetchToken(ctx)
    return &provider.Token{
        Value:     tok.AccessToken,
        ExpiresAt: tok.Expiry,
    }, err
})
model := openai.Chat("gpt-4o", openai.WithTokenSource(ts))
```

`CachedTokenSource` caches until expiry, safe for concurrent use. Also implements `InvalidatingTokenSource` with `Invalidate()`.

## Providers

### OpenAI

```go
import "github.com/zendev-sh/goai/provider/openai"

model := openai.Chat("gpt-4o",
    openai.WithAPIKey(key),          // or reads OPENAI_API_KEY env
    openai.WithBaseURL(url),         // optional override
    openai.WithHeaders(headers),     // custom headers
    openai.WithHTTPClient(client),   // custom transport
    openai.WithTokenSource(ts),      // dynamic auth
)
```

Provider options (via `goai.WithProviderOptions`):
- `useResponsesAPI` (bool) — default true
- `reasoning_effort` (string) — "low", "medium", "high"
- `parallelToolCalls` (bool)

### Ollama

```go
import "github.com/zendev-sh/goai/provider/ollama"

model := ollama.Chat("llama3",
    ollama.WithBaseURL("http://localhost:11434/v1"),  // default
)
```

No auth required. Thin wrapper over `compat`.

### vLLM

```go
import "github.com/zendev-sh/goai/provider/vllm"

model := vllm.Chat("meta-llama/Llama-3-8b",
    vllm.WithBaseURL("http://localhost:8000/v1"),  // default
    vllm.WithAPIKey(key),                          // optional
)
```

Thin wrapper over `compat`.

### Generic Compatible (for LiteLLM, etc.)

```go
import "github.com/zendev-sh/goai/provider/compat"

model := compat.Chat("my-model",
    compat.WithBaseURL("https://my-api.com/v1"),    // required
    compat.WithAPIKey(key),                         // optional
    compat.WithHeaders(headers),                    // custom headers
)
```

### Anthropic

```go
import "github.com/zendev-sh/goai/provider/anthropic"

model := anthropic.Chat("claude-sonnet-4-20250514",
    anthropic.WithAPIKey(key),  // or reads ANTHROPIC_API_KEY env
)
```

### Google Gemini

```go
import "github.com/zendev-sh/goai/provider/google"

model := google.Chat("gemini-2.5-flash",
    google.WithAPIKey(key),  // or reads GOOGLE_GENERATIVE_AI_API_KEY / GEMINI_API_KEY env
)
```

### Other Providers (all follow same pattern)

```go
import "github.com/zendev-sh/goai/provider/azure"
import "github.com/zendev-sh/goai/provider/bedrock"
import "github.com/zendev-sh/goai/provider/vertex"
import "github.com/zendev-sh/goai/provider/cohere"
import "github.com/zendev-sh/goai/provider/mistral"
import "github.com/zendev-sh/goai/provider/groq"
import "github.com/zendev-sh/goai/provider/deepseek"
import "github.com/zendev-sh/goai/provider/xai"
import "github.com/zendev-sh/goai/provider/minimax"
import "github.com/zendev-sh/goai/provider/fireworks"
import "github.com/zendev-sh/goai/provider/together"
import "github.com/zendev-sh/goai/provider/deepinfra"
import "github.com/zendev-sh/goai/provider/openrouter"
import "github.com/zendev-sh/goai/provider/requesty"
import "github.com/zendev-sh/goai/provider/perplexity"
import "github.com/zendev-sh/goai/provider/cerebras"
import "github.com/zendev-sh/goai/provider/nvidia"
import "github.com/zendev-sh/goai/provider/runpod"
import "github.com/zendev-sh/goai/provider/cloudflare"
import "github.com/zendev-sh/goai/provider/fptcloud"
```

## LanguageModel Interface

```go
type LanguageModel interface {
    ModelID() string
    DoGenerate(ctx context.Context, params GenerateParams) (*GenerateResult, error)
    DoStream(ctx context.Context, params GenerateParams) (*StreamResult, error)
}
```

## ToolCall

```go
type ToolCall struct {
    ID       string
    Name     string
    Input    json.RawMessage
    Metadata map[string]any
}
```

## Usage

```go
type Usage struct {
    InputTokens      int
    OutputTokens     int
    TotalTokens      int
    ReasoningTokens  int
    CacheReadTokens  int
    CacheWriteTokens int
}
```

## FinishReason

```go
type FinishReason string
const (
    FinishStop          FinishReason = "stop"
    FinishToolCalls     FinishReason = "tool-calls"
    FinishLength        FinishReason = "length"
    FinishContentFilter FinishReason = "content-filter"
    FinishError         FinishReason = "error"
    FinishOther         FinishReason = "other"
)
```

## Multi-Turn with ResponseMessages

```go
var messages []provider.Message
messages = append(messages, goai.UserMessage("What's the weather?"))

stream, err := goai.StreamText(ctx, model,
    goai.WithMessages(messages...),
    goai.WithTools(weatherTool),
    goai.WithMaxSteps(5),
)
// consume stream...
result := stream.Result()
messages = append(messages, result.ResponseMessages...)  // append assistant + tool messages
```

## Model Listing

GoAI does **not** provide a unified model listing API. Each provider's `/v1/models` endpoint (for OpenAI-compatible providers) must be queried directly with proper auth.

## Source

- Website: https://goai.sh
- GitHub: https://github.com/zendev-sh/goai
- GoDoc: https://pkg.go.dev/github.com/zendev-sh/goai
- Docs: https://goai.sh/getting-started/installation

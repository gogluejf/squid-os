# Plan: OpenAI Codex Integration

## Goal
Add first-class support for OpenAI Codex via both API Key and OAuth (Device Flow), using the **Responses API** format.

## Architecture

### Two Providers

| Provider | Name | Auth Methods | Endpoint | Dialect |
|---|---|---|---|---|
| **OpenAI** | `openai` | `api_key` only | `https://api.openai.com/v1/chat/completions` | `DialectOpenAICompatible` |
| **OpenAI Codex** | `openai-codex` | `oauth` (device) + `api_key` | OAuth: `https://chatgpt.com/backend-api/codex/responses`<br>API Key: `https://api.openai.com/v1/responses` | `DialectOpenAICodex` |

### New Dialect: `DialectOpenAICodex`
- Added to `config/endpoints.go`.
- Uses the **Responses API** body format and SSE event types.

### API Adapter Layer

Create `internal/chat/adapter.go` with interface:

```go
type APIAdapter interface {
    // BuildBody creates the JSON request body from our internal messages/tools.
    BuildBody(model string, messages []ChatMessage, tools []tools.Tool, thinking bool) ([]byte, error)
    // ParseSSE parses one SSE line into a StreamEvent. Returns nil to skip.
    ParseSSE(line string) *StreamEvent
    // GetChatURL returns the inference endpoint for these settings.
    GetChatURL(s *config.ProviderSettings) string
    // GetModels returns available model IDs (fetched or hardcoded).
    GetModels() []string
}
```

#### Adapters

| Adapter | Used By | Body Format | SSE Events |
|---|---|---|---|
| `ChatCompletionsAdapter` | `openai`, `anthropic`, `gemini`, `vllm`, `ollama`, `litellm` | `{model, messages, stream}` | `choices[0].delta.content` |
| `CodexAdapter` | `openai-codex` | `{model, input, instructions, store:false}` | `response.output_text.delta`, `response.completed` |

### Engine Changes (`engine.go`)

- `Engine` struct gains `adapter APIAdapter` field.
- `NewEngine()` picks adapter based on `meta.Dialect`.
- `Stream()`:
  - Body: `adapter.BuildBody(...)`
  - URL: `adapter.GetChatURL(settings)`
  - SSE loop: `adapter.ParseSSE(line)` instead of hard-coded unmarshaling.
  - Rest of flow (tool buffers, thinking parser, cancellation) stays the same.

### Provider Implementation (`provider/openai-codex.go`)

- New file registering `openai-codex` provider.
- `Dialect`: `DialectOpenAICodex`
- `SupportedAuth`: `[AuthOAuth, AuthAPIKey]`
- `New()`: returns `*CodexProvider`
- `CodexProvider`:
  - `PrepareRequest()`: sets `Authorization`, `Originator: opencode`, `ChatGPT-Account-Id` (from JWT), `User-Agent`.
  - `StartDeviceAuth()`, `PollDeviceAuth()` — same as current `OpenAIProvider`.
  - `Refresh()` — token refresh via `https://auth.openai.com/oauth/token`.

### Model Discovery

- **OAuth:** Hardcoded list (`gpt-5.4`, `gpt-5.4-mini`, `gpt-5.1-codex`, etc.) — Codex backend doesn't expose a models endpoint.
- **API Key:** Fetch from `https://api.openai.com/v1/models` (standard endpoint works with API keys).

### Wizard Changes (`provider_setup.go`)

- `openai-codex` with OAuth → device auth flow (start → poll → done).
- Store `ChatGPT-Account-Id` in `OAuthCreds` for `PrepareRequest`.
- `openai-codex` with API key → standard prompt for key entry.

### Config Changes (`config/endpoints.go`)

```go
const (
    DialectOpenAICompatible Dialect = "openai"
    DialectOpenAICodex      Dialect = "openai-codex"
    DialectAnthropic        Dialect = "anthropic"
    DialectGemini           Dialect = "gemini"
)

const ProviderOpenAICodex = "openai-codex"

type OAuthCreds struct {
    AccessToken    string    `json:"access_token"`
    RefreshToken   string    `json:"refresh_token"`
    AccountID      string    `json:"account_id,omitempty"` // ChatGPT-Account-Id
    ExpiresAt      time.Time `json:"expires_at"`
}
```

## Key Endpoints

| Purpose | URL |
|---|---|
| Auth (OAuth authorize) | `https://auth.openai.com/oauth/authorize` |
| Token exchange | `https://auth.openai.com/oauth/token` |
| Device auth start | `POST https://auth.openai.com/api/accounts/deviceauth/usercode` |
| Device auth poll | `POST https://auth.openai.com/api/accounts/deviceauth/token` |
| Codex inference (OAuth) | `POST https://chatgpt.com/backend-api/codex/responses` |
| Codex inference (API Key) | `POST https://api.openai.com/v1/responses` |
| Models (API Key) | `GET https://api.openai.com/v1/models` |

## Required Headers for Codex

- `Authorization: Bearer <token>`
- `Originator: opencode`
- `ChatGPT-Account-Id: <from JWT https://api.openai.com/auth.chatgpt_account_id>`
- `User-Agent: squid-os`

## Responses API Body Format

```json
{
  "model": "gpt-5.4",
  "instructions": "system prompt here",
  "input": [
    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hello"}]},
    {"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "hi"}]}
  ],
  "tools": [{"type": "function", "name": "bash", "parameters": {...}}],
  "stream": true,
  "store": false
}
```

## Responses API SSE Events

| Event Type | Maps To |
|---|---|
| `response.output_text.delta` | `StreamEvent.Text = delta` |
| `response.reasoning_summary_text.delta` | `StreamEvent.Thinking = delta` |
| `response.function_call.arguments.delta` | `StreamEvent.ToolCallDelta` |
| `response.function_call.name.delta` | `StreamEvent.ToolCallName` |
| `response.completed` | `StreamEvent.Done = true` |

## Implementation Order

1. Add `DialectOpenAICodex` + `ProviderOpenAICodex` to config.
2. Add `AccountID` to `OAuthCreds`.
3. Create `APIAdapter` interface + `ChatCompletionsAdapter` (extract existing engine logic).
4. Create `CodexAdapter` (build body, parse SSE).
5. Refactor `Engine` to use adapter.
6. Create `openai-codex` provider registration + `CodexProvider` impl.
7. Update wizard for `openai-codex` (device auth + API key).
8. Update model picker for `openai-codex` (hardcoded list for OAuth).
9. Update `PrepareRequest` to include Codex headers.

## Anti-Patterns to Eliminate During Refactor

**1. URL resolution belongs on `ProviderImpl`, not in shared `ResolveChatURL()` / `ResolveModelsURL()`**

Current smell: shared functions with `if/else` on provider name and auth method.

Fix: Add to `ProviderImpl` interface:
```go
type ProviderImpl interface {
    PrepareRequest(req *http.Request) error
    IsExpired() bool
    Refresh() error
    GetChatURL(settings *ProviderSettings) string   // who knows the URL better than the provider?
    GetModelsURL(settings *ProviderSettings) string
}
```

Each provider implementation returns its own URLs based on its auth method. `openai-codex` returns the Codex backend for OAuth, the Platform API for API key. `openai` returns `api.openai.com/v1/chat/completions`. No shared if/else needed.

**2. Model discovery: always try hardcoded list first, then URL — never skip one for the other**

Current smell: `ScanModels` checks URL first, gives up if empty. Codex workaround was a special-case if.

Fix: Each `ProviderMeta` declares `StaticModels []string`. `ScanModels` logic becomes:
```go
// 1. Add static models from meta (if any)
for _, id := range meta.StaticModels {
    models = append(models, ModelEntry{ID: id, Provider: m.Name})
}

// 2. If there's a models URL, fetch and append (deduplicate)
if modelsURL := impl.GetModelsURL(&s); modelsURL != "" {
    entries, err := fetchModelsDetail(ctx, modelsURL, impl, m.Name)
    if err == nil {
        // merge, dedup by ID
        models = append(models, entries...)
    }
}
```

This means `openai-codex` with OAuth gets its static models. `openai` with API key fetches from URL. Both can coexist. No special cases.

## Reference: OpenCode Source

The primary reference implementation is OpenCode's Codex plugin. When in doubt about
exact formats, headers, or flow details, curl these files:

- **Codex plugin (auth + inference adapter):**
  `curl -s https://raw.githubusercontent.com/anomalyco/opencode/2.0/packages/opencode/src/plugin/codex.ts`

- **Key things to look up in it:**
  - Device auth flow (`StartDeviceAuth`, `PollDeviceAuth` equivalents)
  - PKCE verifier generation (43 chars from RFC 7636 charset)
  - `buildAuthorizeUrl()` — exact params for `/oauth/authorize`
  - `exchangeCodeForTokens()` — exact params for `/oauth/token`
  - `buildAuthorizeUrl()` includes `originator`, `codex_cli_simplified_flow`, `id_token_add_organizations`
  - Token exchange uses `redirect_uri: https://auth.openai.com/deviceauth/callback`
  - Model list (`allowedModels` set) — update this list periodically
  - `chat.headers` hook — `originator`, `User-Agent`, `session_id`
  - URL rewrite to `https://chatgpt.com/backend-api/codex/responses`

- **OpenAI Engineering Blog (Responses API details):**
  `https://openai.com/index/unrolling-the-codex-agent-loop/`
  — Explains Responses API body format (`instructions`, `input`, `tools`)
  and SSE event types (`response.output_text.delta`, etc.)

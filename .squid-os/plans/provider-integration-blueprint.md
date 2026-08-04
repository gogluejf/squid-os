# Provider Integration Blueprint

This blueprint describes how to integrate a new GoAI-backed chat provider into Squid OS using the existing provider registry contract. It is based on the provider work for Anthropic, Gemini, OpenRouter, and Fireworks.

## Goal

Add a provider without provider-specific branching in app or engine code.

A provider integration must plug into:

- `internal/config/endpoints.go` for provider constants and dialects if needed
- `internal/chat/provider/provider.go` via `Provider` interface implementation
- `internal/chat/provider/models.go` via `ScanModels`
- GoAI provider package via `BuildGoAIModel`

Do not add new provider-specific conditionals to:

- `internal/chat/engine.go`
- `internal/chat/models.go`
- `internal/app/provider_setup.go`

## Provider Contract

Every provider must implement `internal/chat/provider.Provider`:

```go
type Provider interface {
    Name() string
    Dialect() config.Dialect

    StartDeviceAuth() (string, string, error)
    PollDeviceAuth() error
    StartOAuth(redirectURI string) (string, error)
    FinishOAuth(code, redirectURI string) error
    GetCredentials() *config.ProviderCreds
    GetDeviceAuthID() string
    SetDeviceState(id, code string)

    BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error)

    ListModels(ctx context.Context) ([]ModelEntry, error)
    ModelDetails(ctx context.Context, modelID string) *ModelEntry

    RequestProviderOptions(model string, thinking bool) map[string]any

    SupportedAuth() []config.AuthMethod
    StaticModels() []ModelEntry
    DefaultBaseURL() string
    RequiresBaseURL() bool
}
```

## Implementation Steps

### 1. Research first; do not assume

For every provider, find evidence for:

- GoAI package name and constructor
- auth method supported by GoAI package
- default base URL used by GoAI
- whether the API is OpenAI-compatible, Anthropic-native, Gemini-native, or custom
- whether model listing exists
- model listing endpoint response shape
- where context length appears, if available
- model detail endpoint, if available
- thinking/reasoning request option shape, if applicable

Preferred evidence order:

1. Local GoAI source in module cache or vendor
2. Provider official docs / SDK source
3. Targeted curl checks against known endpoints
4. Static curated list only when dynamic listing is unavailable or insufficient

Useful commands:

```bash
ls $(go env GOMODCACHE)/github.com/zendev-sh/goai@*/provider/
cat $(go env GOMODCACHE)/github.com/zendev-sh/goai@vX.Y.Z/provider/<provider>/<provider>.go
curl -s "<models-endpoint>" -H "Authorization: Bearer dummy" | head -c 1000
```

Do not broad-grep the whole repository unnecessarily. Prefer targeted paths and files.

### 2. Add config provider constant

Edit `internal/config/endpoints.go`:

```go
const (
    ProviderExample = "example"
)
```

Only add a new dialect if the provider is not compatible with an existing dialect:

- `DialectOpenAICompatible`
- `DialectOpenAICodex`
- `DialectAnthropic`
- `DialectGemini`

### 3. Create provider file

Create:

```text
internal/chat/provider/<provider>.go
```

Use this structure:

```go
package provider

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "squid-os/internal/config"

    goai_provider "github.com/zendev-sh/goai/provider"
    goai_example "github.com/zendev-sh/goai/provider/example"
)

func init() {
    Register(config.ProviderExample, func(settings *config.ProviderSettings) Provider {
        return newExampleProvider(settings)
    })
}

type ExampleProvider struct {
    settings *config.ProviderSettings
}

func newExampleProvider(settings *config.ProviderSettings) *ExampleProvider {
    if settings == nil {
        settings = &config.ProviderSettings{}
    }
    return &ExampleProvider{settings: settings}
}
```

### 4. Metadata methods

Implement provider identity and config:

```go
func (p *ExampleProvider) Name() string { return config.ProviderExample }
func (p *ExampleProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *ExampleProvider) SupportedAuth() []config.AuthMethod {
    return []config.AuthMethod{config.AuthAPIKey}
}
func (p *ExampleProvider) DefaultBaseURL() string { return "https://api.example.com/v1" }
func (p *ExampleProvider) RequiresBaseURL() bool { return false }
```

Rules:

- API-key-only cloud providers return `[]config.AuthMethod{config.AuthAPIKey}`.
- Local unauthenticated providers return `[]config.AuthMethod{config.AuthNone}`.
- Only return OAuth if an actual OAuth flow is implemented and tested.
- `RequiresBaseURL()` is `true` for user-hosted/local/proxy providers like vLLM, Ollama, LiteLLM; false for known cloud providers.

### 5. Static models

Implement `StaticModels()` even when dynamic listing exists.

Purpose:

- common known models are immediately visible
- context lengths are available even if list endpoint omits them
- dynamic endpoint failure still leaves useful defaults

Example:

```go
func (p *ExampleProvider) StaticModels() []ModelEntry {
    return []ModelEntry{
        {ID: "example-large", ContextLength: 128_000},
        {ID: "example-small", ContextLength: 32_000},
    }
}
```

Rules:

- Include context length when evidence exists.
- Use `0` when unknown.
- Do not invent context lengths.
- Keep static list small and curated.
- Dynamic `ListModels` entries are merged after static entries by `ScanModels` and deduped by model ID.

### 6. Credentials helper

Each provider should protect against nil settings/credentials:

```go
func (p *ExampleProvider) creds() *config.ProviderCreds {
    if p.settings == nil || p.settings.Credentials == nil {
        p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
    }
    return p.settings.Credentials
}
```

### 7. Auth stubs

If OAuth/device auth is not supported, implement explicit stubs:

```go
func (p *ExampleProvider) StartDeviceAuth() (string, string, error) {
    return "", "", fmt.Errorf("example: device auth not supported")
}
func (p *ExampleProvider) PollDeviceAuth() error {
    return fmt.Errorf("example: device auth not supported")
}
func (p *ExampleProvider) StartOAuth(redirectURI string) (string, error) {
    return "", fmt.Errorf("example: OAuth not supported")
}
func (p *ExampleProvider) FinishOAuth(code, redirectURI string) error {
    return fmt.Errorf("example: OAuth not supported")
}
func (p *ExampleProvider) GetCredentials() *config.ProviderCreds { return p.creds() }
func (p *ExampleProvider) GetDeviceAuthID() string { return "" }
func (p *ExampleProvider) SetDeviceState(id, code string) {}
```

### 8. BuildGoAIModel

Use the matching GoAI provider package.

API-key provider example:

```go
func (p *ExampleProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
    if model == "" {
        model = "example-large"
    }

    opts := []goai_example.Option{goai_example.WithAPIKey(p.creds().APIKey)}
    if p.settings.BaseURL != "" {
        opts = append(opts, goai_example.WithBaseURL(p.settings.BaseURL))
    }

    return goai_example.Chat(model, opts...), false, nil
}
```

For OpenAI-compatible providers, normalize the base URL if the GoAI provider expects the `/v1` API root:

```go
opts = append(opts, goai_example.WithBaseURL(normalizeOpenAICompatBaseURL(p.settings.BaseURL, p.DefaultBaseURL())))
```

The boolean return value means: does this provider need text-level think-tag parsing?

- Usually `false` for native GoAI providers that expose reasoning as native chunks.
- Use `true` only with evidence that the provider emits reasoning inline in normal text and needs manual parsing.

### 9. RequestProviderOptions

Implement provider-specific thinking/reasoning options here, not in engine code.

Examples:

OpenAI-compatible reasoning effort:

```go
func (p *ExampleProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
    if !thinking {
        return nil
    }
    return map[string]any{"reasoning_effort": "medium"}
}
```

Anthropic-style thinking:

```go
return map[string]any{"thinking": map[string]any{"type": "enabled"}}
```

OpenRouter-style reasoning forwarding:

```go
return map[string]any{"reasoning": map[string]any{"enabled": true}}
```

If unsure, return nil and document why. Do not guess option names.

### 10. ListModels

Implement dynamic listing when endpoint evidence exists.

OpenAI-compatible shape:

```go
func (p *ExampleProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
    baseURL := p.settings.BaseURL
    if baseURL == "" {
        baseURL = p.DefaultBaseURL()
    }
    baseURL = normalizeOpenAICompatBaseURL(baseURL, p.DefaultBaseURL())

    req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+p.creds().APIKey)

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("example models endpoint returned %d: %s", resp.StatusCode, string(body))
    }

    var result struct {
        Data []struct {
            ID            string `json:"id"`
            ContextLength int    `json:"context_length,omitempty"`
            MaxTokens     int    `json:"max_tokens,omitempty"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    models := make([]ModelEntry, 0, len(result.Data))
    for _, m := range result.Data {
        entry := ModelEntry{ID: m.ID}
        switch {
        case m.ContextLength > 0:
            entry.ContextLength = m.ContextLength
        case m.MaxTokens > 0:
            entry.ContextLength = m.MaxTokens
        }
        models = append(models, entry)
    }
    return models, nil
}
```

Rules:

- Return real errors on auth/unreachable so `ScanModels` can show correct sentinel entries.
- Use a 10–15 second timeout.
- Include provider-required headers.
- Filter out non-chat models only when the endpoint returns mixed resources and there is clear evidence of non-chat IDs.
- Do not silently fall back to static models inside `ListModels` unless the provider has no dynamic endpoint by design. Let `ScanModels` handle errors.

### 11. ModelDetails

Implement details if a stable detail endpoint exists.

```go
func (p *ExampleProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
    // On any failure, return non-nil fallback.
    return &ModelEntry{ID: modelID, Provider: config.ProviderExample}
}
```

Rules:

- Must always return non-nil.
- Must include at least `ID` and `Provider`.
- Best effort only; never fail model selection because details lookup failed.
- Use details endpoint to resolve context length when list endpoint omitted it.

### 12. Vendor update

This repo uses `vendor/`. If a new GoAI provider package is imported, run:

```bash
go mod tidy
go mod vendor
```

Then verify vendored paths exist:

```bash
ls vendor/github.com/zendev-sh/goai/provider/<provider>
```

### 13. Validation

Always run:

```bash
go build ./...
```

Optional targeted checks:

```bash
go vet ./internal/chat/provider/...
git diff -- internal/chat/provider/<provider>.go internal/config/endpoints.go vendor/modules.txt
```

### 14. Commit guidance

Use modular commits:

1. Provider integration commit:
   - config constants
   - provider implementation file(s)
   - vendor additions
   - provider docs/plans update

2. UI/bugfix commits separately.

Example commit message:

```text
add <provider> provider
```

or for multiple providers:

```text
add anthropic gemini openrouter fireworks providers
```

## Evidence Patterns From Implemented Providers

### Anthropic

GoAI package:

```go
github.com/zendev-sh/goai/provider/anthropic
anthropic.Chat(model, anthropic.WithAPIKey(key))
```

Default base URL:

```text
https://api.anthropic.com
```

List endpoint:

```text
GET /v1/models
x-api-key: <key>
anthropic-version: 2023-06-01
```

Response context field:

```json
{
  "data": [
    {
      "id": "...",
      "max_input_tokens": 200000
    }
  ]
}
```

Details endpoint:

```text
GET /v1/models/{model_id}
```

Dialect:

```go
config.DialectAnthropic
```

### Gemini / Google Generative Language

GoAI package:

```go
github.com/zendev-sh/goai/provider/google
google.Chat(model, google.WithAPIKey(key))
```

Default base URL:

```text
https://generativelanguage.googleapis.com
```

Generate endpoint used by GoAI:

```text
POST /v1beta/models/{model}:generateContent
```

List endpoint:

```text
GET /v1beta/models?key=<api_key>
```

Response context field:

```json
{
  "models": [
    {
      "name": "models/gemini-2.5-flash",
      "baseModelId": "gemini-2.5-flash",
      "inputTokenLimit": 1048576,
      "outputTokenLimit": 65536
    }
  ]
}
```

Details endpoint:

```text
GET /v1beta/models/{model_id}?key=<api_key>
```

Dialect:

```go
config.DialectGemini
```

### OpenRouter

GoAI package:

```go
github.com/zendev-sh/goai/provider/openrouter
openrouter.Chat(model, openrouter.WithAPIKey(key))
```

Default base URL:

```text
https://openrouter.ai/api/v1
```

List endpoint:

```text
GET /api/v1/models
Authorization: Bearer <key>
HTTP-Referer: <app-url>
X-Title: <app-title>
```

Response context field:

```json
{
  "data": [
    {
      "id": "anthropic/claude-sonnet-5",
      "context_length": 1000000
    }
  ]
}
```

Dialect:

```go
config.DialectOpenAICompatible
```

### Fireworks

GoAI package:

```go
github.com/zendev-sh/goai/provider/fireworks
fireworks.Chat(model, fireworks.WithAPIKey(key))
```

Default base URL:

```text
https://api.fireworks.ai/inference/v1
```

List endpoint:

```text
GET /v1/models
Authorization: Bearer <key>
```

OpenAI-compatible shape:

```json
{
  "data": [
    {
      "id": "accounts/fireworks/models/...",
      "object": "model"
    }
  ]
}
```

Dialect:

```go
config.DialectOpenAICompatible
```

## Remaining Providers To Integrate

These are GoAI-supported providers not currently integrated in Squid OS.

### Cloud / hosted API-key providers

- cohere
- mistral
- xai
- groq
- deepseek
- minimax
- together
- deepinfra
- requesty
- perplexity
- cerebras
- nvidia
- runpod
- fptcloud

### Cloud providers requiring cloud-specific auth or endpoint shaping

- bedrock
- azure
- vertex
- cloudflare

### Local / self-hosted providers

- llamacpp

## Provider Research Checklist

For each remaining provider, fill this before coding:

```md
## <Provider>

- GoAI package:
- GoAI constructor:
- Default base URL:
- Dialect:
- Auth method:
- Env vars supported by GoAI:
- List models endpoint:
- Model detail endpoint:
- Context length field:
- Static fallback models:
- Thinking/reasoning option:
- Requires base URL:
- Notes / caveats:
```

## Common Pitfalls

- Forgetting to update `vendor/` after importing a new GoAI provider.
- Adding provider-specific logic to engine/app instead of provider implementation.
- Returning `nil` from `ModelDetails`; it must always return an entry.
- Guessing context lengths instead of using documented endpoint fields or leaving `0`.
- Using `/v1/models` when the GoAI provider default base URL already includes `/v1` and accidentally producing `/v1/v1/models`.
- Passing a `/chat/completions` base URL into GoAI when it expects an API root.
- Treating model listing errors as empty lists, which hides auth and network failures from the UI.
- Including non-chat models in picker when a provider returns embeddings/rerank/reward/image models in the same endpoint.

## Minimal Provider Skeleton

```go
package provider

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "squid-os/internal/config"

    goai_provider "github.com/zendev-sh/goai/provider"
    goai_example "github.com/zendev-sh/goai/provider/example"
)

func init() {
    Register(config.ProviderExample, func(settings *config.ProviderSettings) Provider {
        return newExampleProvider(settings)
    })
}

type ExampleProvider struct { settings *config.ProviderSettings }

func newExampleProvider(settings *config.ProviderSettings) *ExampleProvider {
    if settings == nil { settings = &config.ProviderSettings{} }
    return &ExampleProvider{settings: settings}
}

func (p *ExampleProvider) Name() string { return config.ProviderExample }
func (p *ExampleProvider) Dialect() config.Dialect { return config.DialectOpenAICompatible }
func (p *ExampleProvider) SupportedAuth() []config.AuthMethod { return []config.AuthMethod{config.AuthAPIKey} }
func (p *ExampleProvider) StaticModels() []ModelEntry { return nil }
func (p *ExampleProvider) DefaultBaseURL() string { return "https://api.example.com/v1" }
func (p *ExampleProvider) RequiresBaseURL() bool { return false }
func (p *ExampleProvider) RequestProviderOptions(model string, thinking bool) map[string]any { return nil }

func (p *ExampleProvider) BuildGoAIModel(model string) (goai_provider.LanguageModel, bool, error) {
    if model == "" { model = "example-default" }
    opts := []goai_example.Option{goai_example.WithAPIKey(p.creds().APIKey)}
    if p.settings.BaseURL != "" { opts = append(opts, goai_example.WithBaseURL(p.settings.BaseURL)) }
    return goai_example.Chat(model, opts...), false, nil
}

func (p *ExampleProvider) ListModels(ctx context.Context) ([]ModelEntry, error) {
    baseURL := p.settings.BaseURL
    if baseURL == "" { baseURL = p.DefaultBaseURL() }

    req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
    if err != nil { return nil, err }
    req.Header.Set("Authorization", "Bearer "+p.creds().APIKey)

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("example models endpoint returned %d: %s", resp.StatusCode, string(body))
    }

    var result struct { Data []struct { ID string `json:"id"` } `json:"data"` }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { return nil, err }

    models := make([]ModelEntry, 0, len(result.Data))
    for _, m := range result.Data { models = append(models, ModelEntry{ID: m.ID}) }
    return models, nil
}

func (p *ExampleProvider) ModelDetails(ctx context.Context, modelID string) *ModelEntry {
    return &ModelEntry{ID: modelID, Provider: config.ProviderExample}
}

func (p *ExampleProvider) StartDeviceAuth() (string, string, error) { return "", "", fmt.Errorf("example: device auth not supported") }
func (p *ExampleProvider) PollDeviceAuth() error { return fmt.Errorf("example: device auth not supported") }
func (p *ExampleProvider) StartOAuth(redirectURI string) (string, error) { return "", fmt.Errorf("example: OAuth not supported") }
func (p *ExampleProvider) FinishOAuth(code, redirectURI string) error { return fmt.Errorf("example: OAuth not supported") }
func (p *ExampleProvider) GetCredentials() *config.ProviderCreds { return p.creds() }
func (p *ExampleProvider) GetDeviceAuthID() string { return "" }
func (p *ExampleProvider) SetDeviceState(id, code string) {}

func (p *ExampleProvider) creds() *config.ProviderCreds {
    if p.settings == nil || p.settings.Credentials == nil {
        p.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
    }
    return p.settings.Credentials
}
```

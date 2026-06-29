# GoAI Future Provider Migration Notes

This document tracks the remaining GoAI-supported providers that are not part of the first migration wave.

Initial migration scope implemented now:
- openai
- openai-codex
- ollama
- vllm
- litellm (via compat)

Deferred providers should follow the same interface pattern already established by the active providers:
- `BuildGoAIModel(model string) (provider.LanguageModel, bool, error)`
- `ListModels(ctx context.Context) ([]string, error)`
- standard metadata methods
- auth flow methods only when the provider actually supports them

## Migration pattern

Each future provider should:
1. register with `Register(config.ProviderX, ...)`
2. implement `BuildGoAIModel(model string)` using the matching GoAI provider package
3. implement `ListModels(ctx)` with authenticated HTTP GET when the upstream API supports listing
4. return `parseThinkingFromText=true` only when the provider exposes reasoning inline in text rather than native reasoning chunks
5. keep provider-specific auth inside the provider implementation, not in app code

## Provider mapping

| Provider | GoAI package | BuildGoAIModel pattern | Auth mapping | ListModels guidance |
|---|---|---|---|---|
| Anthropic | `provider/anthropic` | `anthropic.Chat(model, ...)` | API key | Anthropic has limited/no standard `/v1/models` compatibility path in many setups; fall back to `StaticModels()` if needed |
| Google | `provider/google` | `google.Chat(model, ...)` | API key / Google auth depending provider support | implement only if stable listing endpoint is available; otherwise static fallback |
| Bedrock | `provider/bedrock` | `bedrock.Chat(model, ...)` | AWS credentials | usually no simple generic `/v1/models`; prefer static fallback |
| Azure OpenAI | `provider/azure` | `azure.Chat(model, ...)` | API key / Azure auth | provider-specific endpoint layout; list only if endpoint is stable in our config model |
| Vertex | `provider/vertex` | `vertex.Chat(model, ...)` | Google Cloud credentials | usually static fallback unless listing path is worth wiring |
| Cohere | `provider/cohere` | `cohere.Chat(model, ...)` | API key | implement list if supported, else static fallback |
| Mistral | `provider/mistral` | `mistral.Chat(model, ...)` | API key | implement authenticated list if stable |
| xAI | `provider/xai` | `xai.Chat(model, ...)` | API key | implement authenticated list if stable |
| Groq | `provider/groq` | `groq.Chat(model, ...)` | API key | implement authenticated list if stable |
| DeepSeek | `provider/deepseek` | `deepseek.Chat(model, ...)` | API key | implement authenticated list if stable |
| MiniMax | `provider/minimax` | `minimax.Chat(model, ...)` | API key | implement authenticated list if stable |
| Fireworks | `provider/fireworks` | `fireworks.Chat(model, ...)` | API key | implement authenticated list if stable |
| Together | `provider/together` | `together.Chat(model, ...)` | API key | implement authenticated list if stable |
| DeepInfra | `provider/deepinfra` | `deepinfra.Chat(model, ...)` | API key | implement authenticated list if stable |
| OpenRouter | `provider/openrouter` | `openrouter.Chat(model, ...)` | API key | implement authenticated list if stable |
| Requesty | `provider/requesty` | `requesty.Chat(model, ...)` | API key | implement authenticated list if stable |
| Perplexity | `provider/perplexity` | `perplexity.Chat(model, ...)` | API key | implement authenticated list if stable |
| Cerebras | `provider/cerebras` | `cerebras.Chat(model, ...)` | API key | implement authenticated list if stable |
| NVIDIA NIM | `provider/nvidia` | `nvidia.Chat(model, ...)` | API key | implement authenticated list if stable |
| RunPod | `provider/runpod` | `runpod.Chat(model, ...)` | API key | implement authenticated list if stable |
| Cloudflare Workers AI | `provider/cloudflare` | `cloudflare.Chat(model, ...)` | API key / account auth | likely provider-specific listing; static fallback acceptable |
| FPT Smart Cloud | `provider/fptcloud` | `fptcloud.Chat(model, ...)` | API key | implement authenticated list if stable |
| Llama.cpp | `provider/llamacpp` | `llamacpp.Chat(model, ...)` | local / optional auth | similar to Ollama/vLLM; list endpoint may be local-provider-specific |

## Deferred-by-design notes

These providers are intentionally deferred until the current 5-provider migration is stable in real use.

Reasons for deferral:
- the first wave already exercises all core migration patterns:
  - native OpenAI provider
  - OpenAI-compatible compat provider
  - local server provider
  - OAuth-backed provider
  - API-key provider
- several remaining providers do not have a clean universal model-listing endpoint
- many require cloud-specific auth or endpoint shaping not needed to validate the architecture

## Implementation checklist for the follow-up plan

For each provider added later:
- add config provider constant if missing
- register the provider in `internal/chat/provider`
- implement metadata methods
- implement `BuildGoAIModel(model string)`
- implement `ListModels(ctx)` or explicitly document static fallback
- decide `parseThinkingFromText` based on actual streaming behavior
- verify:
  - `go build ./...`
  - engine streaming
  - model scan sentinels
  - provider setup/auth path if applicable

## Important rule

Do not reintroduce provider-specific branching into:
- `internal/chat/engine.go`
- `internal/chat/models.go`
- `internal/app/provider_setup.go`

All future providers must plug in through the existing `Provider` interface and registry only.

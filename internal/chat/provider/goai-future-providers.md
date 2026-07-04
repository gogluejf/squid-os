# GoAI Future Provider Migration Notes

This document tracks the remaining GoAI-supported providers that are not part of the current implementation.

## Implemented providers

| Provider | File | Auth | ListModels | Dialect |
|---|---|---|---|---|
| openai | openai.go | API Key / OAuth | `GET /v1/models` | openai |
| openai-codex | openai-codex.go | API Key / OAuth | `GET /v1/models` (API key) / static (OAuth) | openai-codex |
| ollama | ollama.go | None | `GET /v1/models` | openai |
| vllm | vllm.go | None | `GET /v1/models` | openai |
| litellm | litellm.go | API Key | (via compat) | openai |
| anthropic | anthropic.go | API Key | `GET /v1/models` | anthropic |
| gemini | gemini.go | API Key | `GET /v1beta/models?key=` | gemini |
| openrouter | openrouter.go | API Key | `GET /api/v1/models` | openai |
| fireworks | fireworks.go | API Key | `GET /v1/models` | openai |

## Remaining providers

- bedrock
- azure
- vertex
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
- cloudflare
- fptcloud
- llamacpp

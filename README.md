# squid-os

Runtime and AI harness to operate your entire OS — workstation, server, production.

You do more than code. Squid OS is built for all of it: files, documents, applications, workflows, browsing, research, trading, automation. Coding? Absolutely, and very well. But that's just one thing you do in a day.

## Quick start

```bash
git clone https://github.com/gogluejf/squid-os
cd squid-os
go build -o squid-os .
./squid-os
```

On first launch it seeds `~/.config/squid-os/` with default skills, agents, and settings.

Then:

1. `/models` — pick a model (local or API)
2. Start talking

Press **Ctrl+H** anytime for help.

## Config

Everything lives in `~/.config/squid-os/`:

| Path | What |
|------|------|
| `settings.json` | App settings |
| `endpoints.json` | Model endpoints |
| `skills/` | Installed skills |
| `agents/` | Installed agents |
| `sessions/` | Saved sessions |
| `sys-prompts/` | System prompts |

## Skills

Everything is a skill. A skill is a set of instructions + scripts that teaches the agent how to do something specific.

**Philosophy:** skills are deterministic and predictable. No magic, no hidden state — just clear steps, explicit tools, and repeatable output. If it works once, it works every time.

Built-in skills ship with every install:

- **skill-builder** — create your own skills (start here) → [view source](defaultconfig/skills/skill-builder/SKILL.md)
- **browser-use** — browse websites, automate Chrome, extract data
- **chat-analytics** — interactive dashboard for session analysis
- **plan-generator** / **plan-runner** / **task-executor** — structured dev planning & execution
- **mouse-paint** — paint text/symbols via mouse control
- **os-vision** — inspect media files

Build your own with the **skill-builder** skill. Just ask: *"build me a skill that does X"*.

## Agents

Agents are specialized sub-personalities with their own system prompt, tools, limits, and memory. They can be called from chat or by other agents.

Ships with:

- **trader** — market research, setup evaluation, risk-aware trade proposals → [view example](defaultconfig/agents/trader/agent.yaml)

An agent is defined by a single `agent.yaml`:

```yaml
name: trader
description: Researches markets, evaluates setups, and proposes risk-aware trades.
model: vllm/unsloth/Qwen3.8-27B-NVFP4
system: |
  You are a careful trading research agent.
  ...
tools:
  - read_file
  - bash
skills:
  - browser-use
limits:
  steps: 60
  time: 30m
```

Create your own in `~/.config/squid-os/agents/<name>/agent.yaml`.

## Headless mode

Run without TUI for scripting and pipelines:

```bash
./squid-os --headless "your prompt here"
```

## Session config resolution

Session properties resolve by priority (leftmost wins): **CLI flag > Agent definition > Global settings > Computed default**. Saved sessions lock certain fields (working dir, system prompt, memory) — those are immutable once a session exists.

Full precedence table: [`.squid-os/plans/session-config-resolution.md`](.squid-os/plans/session-config-resolution.md)

## Multimodal attachments

Squid OS gives your operating system eyes. Attach images, screenshots, PDFs, or any media and the model sees them directly — no separate vision pipeline, no "describe this image" ceremony. You can point it at a terminal window and ask *"what do you see?"* and get back a real analysis of what's on screen. Your OS environment becomes part of the conversation.

## Why this exists

Most AI tools are optimized for convenience. Squid OS is optimized for **feedback speed** and **operational breadth**:

- keyboard-native, minimal overhead
- local-AI-first (works great with vLLM, Ollama, etc.)
- streaming telemetry visible (tok/s, TTFT, token counts)
- fail-fast cancel and retry
- operates your actual filesystem, browser, and tools — not just chat

Built in Go. Deliberately so.

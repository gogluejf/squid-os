# README Production Plan for squid-os

## Objective
Produce a high-quality `README.md` that combines:
- Strong product narrative (fast local AI chat workflow)
- Credible technical detail (grounded in current code)
- Clear project identity (extension of rig-stack, now evolving as a standalone TUI)
- A complete open-source finish (screenshot slot + MIT license section)

---

## Writing Principles
1. **Balanced tone with punch**: persuasive product language + concrete technical proof.
2. **No overclaiming**: every capability should map to implemented behavior in code.
3. **Local-first positioning**: speed, control, privacy, experimentation.
4. **Observability-first narrative**: position the tool as the fastest way to observe local model behavior while iterating.
4. **Founder-motivation clarity**: include personal Go motivation and AI learning angle.

---

## Planned README Structure

### 1) Title + One-Liner
- Position `squid-os` as a high-speed local AI TUI for iterative experimentation.

### 2) Intro: Why this exists
- Explain web UI friction and the need for a fast keyboard-native loop.
- Keep this section short and punchy.
- Mention ability to quickly load/manipulate/fail-fast/retry/cancel/switch context locally.
- Explicitly mention core runtime metrics: tok/s, token count, TTFT, durations.
- Anchor the intro message around **local AI observability + speed**.

### 3) Motivation
- Product motivation:
  - remove interaction friction,
  - optimize local inference testing speed,
  - keep users close to model behavior.
- Personal Go motivation:
  - opportunity to learn and build a complete project in Go,
  - preference for Go simplicity/paradigm,
  - test how language clarity impacts agentic coding compared with JS/TS workflows.

### 4) Feature Highlights (Sell each feature)
Each bullet should include both value statement and concise technical credibility:
- **Streaming with live performance telemetry** (tok/s, tokens, elapsed, TTFT) as the core observability loop
- **Thinking mode with expand/collapse visibility**
- **Fast cancel + fail-fast interaction loop**
- **Session save/load + preview + autosave behavior**
- **Prompt history and rapid iteration ergonomics**
- **Model discovery + switching across OpenAI-compatible endpoints**
- **Rich markdown rendering in terminal output**
- **Image attachment for multimodal prompts**
- **Incognito mode for local/private experimentation**
- **Headless mode for scripted/CLI use**

### 5) Local AI Experimentation Vision
- Describe `squid-os` as an evolving local AI experimentation tool:
  - fast iterative testing,
  - observability as the main primitive before orchestration,
  - groundwork toward local AI OS-like concepts,
  - examples: bios, identity, soul, persistent memory, behavior shaping loops,
  - positioning: not just chat UX, but a rapid control and feedback surface for local inference systems.

### 6) Origin Story
- Explain it started as an extension of rig-stack (GPU AI local cloud server context).
- Emphasize evolution into a powerful, easy, fun standalone TUI chat.

### 7) Screenshot Section
- Add a dedicated section with placeholder:
  - `docs/images/squid-os-tui.png` (or similar)
  - alt text + short caption.

### 8) Quick Start
- Build/install paths from repo (`install.sh`, Go build path, binary usage).
- Include TUI and headless usage examples.

### 9) Commands & Keybindings
- Summarize slash commands and ergonomic shortcuts for speed.

### 10) Configuration & Data Paths
- Explain config directory defaults and important files:
  - endpoints, settings, history, sessions, system prompts.

### 11) License
- Add explicit MIT section with link to `LICENSE`.
- If missing in repo, include task to create `LICENSE` file with standard MIT text.

---

## Feature-to-Code Evidence Map
Use this map while drafting to keep claims accurate:

- CLI flags (`--headless`, `--prompt`, `--image`, `--thinking`, `--incognito`) in `main.go`.
- Live streaming metrics and TTFT in `internal/app/metrics.go` and stream persistence fields in `internal/config/session.go`.
- Footer/headers showing model and token/tok-s info in `internal/ui/footer.go` and `internal/ui/message.go`.
- Thinking parsing and rendering in `internal/chat/thinking.go` and `internal/ui/message.go`.
- Cancel behavior and stream cleanup in `internal/app/input.go` and `internal/app/stream.go`.
- Session save/load/preview/autosave/incognito behavior in `internal/app/session.go`, `internal/app/pickers.go`, `internal/config/session.go`.
- Model scan/switch in `internal/chat/models.go`, `internal/chat/engine.go`, `internal/app/pickers.go`.
- Rich markdown rendering in `internal/ui/markdown.go` + assistant message rendering in `internal/ui/message.go`.
- Prompt history navigation in `internal/app/util.go` and persisted history in `internal/config/history.go`.
- Headless mode execution in `internal/headless/headless.go`.

---

## Implementation Checklist for Code Mode
- [ ] Create `README.md` from this outline.
- [ ] Add screenshot placeholder section and expected image path.
- [ ] Add MIT License section linking `LICENSE`.
- [ ] If `LICENSE` does not exist, create standard MIT `LICENSE` file.
- [ ] Final pass: tighten language, fix typos, ensure feature claims match code.

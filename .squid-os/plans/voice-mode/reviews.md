## 1.1 Add voice settings
- A (qwen3.8-27b): PASS — minor note: json.Unmarshal error discarded in LoadSettings (pre-existing, preserved)
- B: skipped by user request (no Codex reviewer this run)
- Resolution: no findings accepted; task complete

## Milestone 1 Review — Resolution (2026-11-18)

### User rulings on design questions:
- **Item 7** (`/voice` args): NO arguments. `/voice` = toggle, exactly like `/thinking`. Remove on/off arg handling; bare command flips state.
- **Item 8** (speech across tool calls): Each step (stream + tools) is a SEPARATE group. When a tool batch finishes and the next stream starts, that's a NEW group — the old one is done/dead. No crazy combination or continuation across tool phases. Simple: one group per step.
- **Item 9** (language detection latency): REJECTED — non-issue. The /detect call is localhost (127.0.0.1:8811), its only purpose is picking the voice NAME (-v flag) for the TTS request; the server auto-detects language for synthesis regardless. It is not under a lock that blocks input. Milestone 3 task 3.1 handles lastUserLang for alert lead-ins separately. No change needed.

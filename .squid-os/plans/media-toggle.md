# Media Toggle

## Problem

There is no user-facing control for whether media attachments are delivered to the model. The current reactive retry (`stripMediaParts` + `isMediaRejection`) is unreliable: it can't distinguish a media rejection from a network blip, and it doesn't persist anything, so a genuinely-unsupported model wastes a failed request every turn.

## Goal

A simple **media on/off** toggle (like thinking) that controls whether media parts are included in the API request. Persisted on the session. Configurable via settings, CLI flag, and agent definition YAML. Delete the reactive retry/strip code.

## Behavior

- **Media: on** (default) — `BuildContext` includes image/file parts for attachments. If the model rejects them, the user sees the error. Their problem.
- **Media: off** — `BuildContext` skips all media parts. Text only. Attachments still exist in the session (refs, files, tally) but are not sent to the provider.
- **Caps hard-floor** — if the model explicitly declares `Attachment: true` + `Image: false` (or `PDF: false`), that modality is omitted regardless of the toggle. The toggle only controls the "unknown" case.

## Changes

### 1. Config field

**`internal/config/session.go`** — `SessionConfig`:
```go
MediaEnabled bool `json:"media_enabled"` // default true
```

**`internal/config/settings.go`** — `Settings`:
```go
MediaEnabled bool `json:"media_enabled"` // default true
```

### 2. Resolution matrix

**`internal/runtime/runtime.go`** — `Resolve`:

| Source | Field | Precedence |
|--------|-------|-----------|
| CLI | `--media / --no-media` | highest |
| Agent YAML | `media: true/false` | middle |
| Settings | `media_enabled` | lowest |

Resolution: `CLI > agent > settings > default(true)`

- `in.CLI.MediaEnabled *bool` (nil = not set)
- `definition.Media *bool` in agent YAML (nil = not set)
- `in.Settings.MediaEnabled bool`

Apply in `Resolve()` after agent block, same pattern as `Thinking`:
```go
if definition.Media != nil {
    cfg.MediaEnabled = *definition.Media
}
// CLI overrides last
if c.MediaEnabled != nil {
    cfg.MediaEnabled = *c.MediaEnabled
}
```

### 3. Agent definition YAML

**`internal/agent/definition.go`**:
```go
Media *bool `yaml:"media"`
```

Example agent YAML:
```yaml
name: text-only-agent
model: vllm/unsloth/Qwen3.8-27B-NVFP4
media: false
```

### 4. CLI flag

**`internal/cli/`** — add `--media` / `--no-media` flag:
```go
MediaEnabled *bool `flag:"media,no-media"`
```

Or as a pair of bool flags depending on the existing CLI pattern.

### 5. BuildContext gate

**`internal/chat/context.go`** — `buildUserMessageParts` and `buildSyntheticInspectParts`:

Add a `mediaEnabled bool` param (or pass it through `BuildContext`):

```go
func BuildContext(messages []config.Message, enabled bool, baseDir string, attachments []media.Attachment, caps *goai_provider.ModelCapabilities, mediaEnabled bool) Context {
```

In `buildUserMessageParts`:
```go
if !mediaEnabled {
    return []goai_provider.Part{{Type: goai_provider.PartText, Text: msg.Text}}
}
```

Same guard in `buildSyntheticInspectParts`.

The caps-based per-kind omission (`MediaDecisionFor`) still applies as a hard floor even when `mediaEnabled` is true.

### 6. Session method

**`internal/chat/session.go`** — `s.BuildContext()`:
```go
func (s *Session) BuildContext() Context {
    caps := s.ModelCaps()
    ctx := BuildContext(s.Doc.Messages, s.Doc.Config.ContextCompaction, s.mediaBaseDir(), s.Doc.Attachments, &caps, s.Doc.Config.MediaEnabled)
    ...
}
```

**`internal/chat/token_tally.go`** — `calculateContextTally`: same change.

### 7. Delete reactive retry

**`internal/chat/engine.go`**:
- Delete `stripMediaParts` function
- Delete the `retrying` param from `runStream`
- Delete the `isMediaRejection` check block in the stream loop
- `runStream` becomes a simple single-pass stream (no recursion)

**`internal/chat/media_policy.go`**:
- Delete `isMediaRejection`
- Delete `isNonMediaError`
- Delete `asAPIError`
- Keep `MediaDecision` / `MediaDecisionFor` (still used for caps-based build-time omission)

### 8. Footer / UI

**`internal/app/render.go`** — `buildFooterData`:
- Show media state in footer (e.g. `media:off` when disabled, like thinking indicator)

**`internal/ui/help.go`** — document the toggle keybind if we add one.

### 9. Keybind (optional, like thinking)

**`internal/app/keymap.go`** — e.g. `ctrl+m` to toggle media on/off mid-session.
- Flips `s.Doc.Config.MediaEnabled`
- Shows notification: "Media delivery: off"
- Takes effect on next turn (next `BuildContext`)

## What this does NOT do

- Does not auto-omit individual attachments
- Does not track per-model exclusions
- Does not retry failed requests
- Does not resize/compress images
- Does not block the user from attaching files when media is off (they can still attach, it just won't be sent)

## Verify

```bash
go build ./...
go test ./internal/chat/ ./internal/config/ ./internal/runtime/ ./internal/agent/ ./internal/cli/ -count=1
```

Manual:
- Session with media:off → footer shows indicator, no image parts in API request
- Session with media:on + vllm → image sent, if model fails user sees error
- Agent YAML with `media: false` → session starts with media off
- CLI `--no-media` → overrides agent/settings

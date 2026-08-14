# Squid — File & Multimodal Attachments

## Goal

Allow files to become part of a Squid message regardless of where they originate, while keeping sessions portable, persistent, and provider-independent.

## File Sources

A file can enter a message through:

- `@` file autocomplete
- Clipboard paste
- Media/file URL
- Future sources such as drag/drop, camera, microphone, etc.

All sources follow the same ingestion flow.

## Ingestion

When a file enters the composer:

1. Resolve/read/download the file.
2. Copy it into the session directory.
3. Detect its MIME type.
4. Give it a canonical session reference:

`@file/<session-file>`

The original source no longer matters after ingestion.

## @ Autocomplete

`@` is Squid's general reference autocomplete and can expose:

- 📎 Files
- ⚡ Skills
- 🔧 Tools
- 🤖 Agents

File search primarily covers:

- Current workspace recursively
- Existing session files

Selecting an external file copies it into the session before creating its `@file/...` reference.

## Composer & Rendering

Canonical message text:

`Analyze @file/abc123-screenshot.png`

TUI rendering:

`Analyze [📎 screenshot.png]`

The chip is only a visual representation. The session stores the canonical `@file/...` reference.

The same rendering is used when displaying historical messages.

## Attachment Semantics

The message text is the source of truth.

On message submission, `BuildAPIMessage` resolves every `@file/...` reference and attaches the corresponding session file.

If the user removes the file chip/reference before submitting, the file is not attached.

The physical file can remain in the session for future reuse.

## Multimodal Processing

For every referenced file:

`@file/... → session file → MIME detection → media classification → provider adapter`

Basic classifications:

- `image/*` → image
- `audio/*` → audio
- `video/*` → video
- `application/pdf` → document
- `text/*` → document/file
- Other → generic file

MIME detection should use the actual file content where possible, with the extension as a hint.

## Model Capabilities

Capabilities belong primarily to the model:

`text, image, pdf, audio, video`

When capabilities are known, Squid can validate them.

When capabilities are unknown, Squid should be permissive and attempt to send the attachment rather than blocking it.

## Provider Adapter

Squid's core does not need to understand provider-specific multimodal formats.

The provider adapter translates Squid attachments into the provider's API representation:

`Squid Attachment → Provider-specific content`

For example, an image may become an OpenAI-compatible image content part, while a PDF may use a file content part.

## Core Principle

**Many file sources → one session file abstraction → one `@file/...` reference → provider-specific multimodal encoding.**

The session remains self-contained: message history references files owned by the session rather than external paths or URLs.

## Note

Incognito mode or when not auto-save

## Temporary & Incognito Sessions

Attachments do not depend on a persisted session. Squid-OS always provides a session file workspace behind `@file/...`. Saved sessions use persistent session storage, while unsaved/manual-save sessions use temporary storage that is moved into the session directory when saved. Incognito sessions use ephemeral storage that is deleted when the session ends. The attachment and provider layers always resolve files through the same session workspace abstraction and do not need to know how or where the session is persisted.
# Squid-OS Documentation

## Attachment & Multimodal Specification

The authoritative specification for file and multimodal attachments is at
`.squid-os/plans/attachment.md`. It covers:

- **Supported tiers**: deliverable (image, PDF, text), storable (audio, video), and provider-dependent unsupported combinations.
- **Shared attachment contract**: `media.Attachment` record, `@file:<ID>` canonical references, ingestion pipeline, URL security, and GoAI part resolution.
- **Search and paste behavior**: configurable file search roots (working directory default), Ctrl+V / Shift+Insert paste flow, and 32 KiB large-text threshold.
- **Capability and retry policy**: `MediaAllow` / `MediaOmit` / `MediaAttempt` decisions, one classified retry on media rejection, and non-media error exclusion.
- **Storage lifecycle**: temporary workspace for unsaved sessions, atomic migration on first save, fork copy semantics, and incognito cleanup with bounded startup stale removal.
- **inspect_media contract**: tool schema, source resolution, execution flow, and bounded text-only results.

## Images

See `docs/images/` for reference screenshots.

# Local input gateway (socket + CLI)

Status: implemented (milestone 2). Design contract: `.squid-os/plans/queued-input-design.md` §4–5.

## What it is

A private Unix-domain socket per TUI process bridges external submissions
(voice, scripts, other tools) into the shared queued-input coordinator. No key
simulation, no direct session mutation — everything funnels through
`SubmissionCoordinator.AdmitSubmission`, the same path the keyboard uses.

- Server: `internal/localinput` (protocol.go, server.go)
- CLI: `squid-os input <list|send|status|clear|resume>` (`internal/cli/input.go`)
- TUI wiring: `--input-instance ID` flag on `squid-os tui`; socket published at
  `$XDG_RUNTIME_DIR/squid-os/<instance>.sock` (0600), removed on exit.

## Commands

```
echo "text" | squid-os input send --when step [--id UUID] [--session ID] [--instance ID]
squid-os input status [--id UUID] [--session ID]
squid-os input list
squid-os input clear
squid-os input resume
```

Text is read from **stdin** (Unicode/newlines safe), never argv. When `--id` is
omitted a UUID is minted client-side; **retries after a timeout MUST reuse the
same `--id`** — the queue deduplicates by request ID, so a retry can never
duplicate history.

Instance selection: exactly one live instance → used automatically; multiple →
`ambiguous_target` error, pass `--instance`. Stale socket files are detected by
dial probe and skipped.

## Exit codes

| code | meaning |
|---|---|
| 0 | accepted / pending / injected / paused (informational) |
| 1 | uncertain or unavailable (no reply, no TUI, store error) — retry with SAME id |
| 2 | permanent rejection (invalid, oversize, wrong version/session, full, id_conflict) — do NOT retry |

## Accepted vs injected vs done

- `accepted` = durably admitted to the queue journal. It is NOT an assistant
  answer and does not mean the model ran.
- `injected` = the entry was appended to session history at a safe boundary
  (tool-batch-done for step entries, logical-turn end for turn entries).
- There is **no exactly-once guarantee for model requests or tool side effects
  across crashes** — only exactly-once *history insertion* within retained
  receipt state. A discarded request stays discarded on retry.

## Durability and privacy

- Normal sessions: receipts persist under the configured state root
  (`input-queue/` journals, `input-queue-paused/` markers), dirs 0700, files
  0600. Queue checkpointing happens even when chat autosave is off — this is
  intentional and documented here.
- Incognito: memory-only; replies carry `"durability": "memory"`. Nothing is
  written to disk.
- Frame limit: 256 KiB/request; queue limits per design (4096 entries / 16 MiB
  pending per session) reject rather than evict.

## Recovery semantics

- Crash before durable admit → nothing recorded; client keeps its outbox.
- Crash after admit, before injection → entry recovers as pending, scheduler
  PAUSED. `/queue-resume` (or `input resume`) is required; nothing auto-runs.
- Crash after append+checkpoint, before receipt update → reconciliation marks
  the entry injected (its message ID is in the persisted history). No double
  insert.
- Restart with pending entries → they reload paused. `input status` shows
  `"paused"` until resumed.

## Known limitations (v1)

- Peer-UID validation (SO_PEERCRED) not yet enforced; isolation comes from the
  per-UID runtime dir + 0600 socket.
- `input status --id` reports queue-level state, not per-request receipt lookup
  (receipts are queryable via the coordinator but not yet exposed on the wire).
- Socket handshake carries no protocol version negotiation beyond the frame's
  `version` field.

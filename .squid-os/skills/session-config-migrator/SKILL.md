---
name: session-config-migrator
description: Safely analyzes and migrates persisted Squid-OS session JSON when session configuration schemas change, using Git comparisons, approval gates, metadata-preserving backups, deterministic callbacks, and integrity validation.
version: 1.0.0
allowed-tools: bash read_file write_file
---

## Overview
Analyzes relevant old and new session JSON structures from a user-specified Git comparison, proposes an explicit field migration for approval, then runs a deterministic project-local migration workflow that preserves source files, creates timestamped backup and migrated trees, and validates every result.

## Variables
- `<skill-folder>` — directory containing this skill and its deterministic migration runner
- `<working-dir>` — active Squid-OS project root whose code and Git history define the session schema

## Instructions
1. Interpret the user input as instructions for locating the before and after schema versions, such as HEAD versus unstaged changes, two commits, staged versus working tree, or a named revision versus the working tree. The user is not expected to provide struct definitions.
2. Inspect the requested Git diff plus enough surrounding Go code, JSON tags, constructors, defaults, and consumers to understand the semantic migration.
3. Present only the relevant old and new struct fields and representative JSON fragments. Do not dump entire unrelated structs.
4. Enumerate every relevant addition, removal, rename, type change, nesting change, default, and constraint.
5. Propose an explicit field-by-field migration: values copied unchanged, values transformed, defaults inserted, fields removed, ambiguous cases, possible data loss, and validation assertions.
6. In interactive mode, stop and obtain explicit user confirmation of the old/new analysis and mapping before writing migration-specific code or running a migration. In autonomous mode, proceed only when the mapping is unambiguous and conservative; otherwise stop with an error.
7. After approval, write one migration-specific Python callback module into the configured temporary directory. It must implement exactly this interface:

    MIGRATION_API_VERSION = 1
    MIGRATION_ID = "migration-name"
    ALLOWED_CHANGED_PATHS: set[str] = set()

    def migrate(document: dict) -> dict:
        ...

    def validate(before: dict, after: dict) -> list[str]:
        ...

8. Keep the callback pure and deterministic. It transforms one parsed JSON document, does not mutate its input, performs no filesystem/network/subprocess operations, and returns schema-specific validation errors from validate().
9. Run exactly `python3 <skill-folder>/scripts/migrate_sessions.py --source <session-directory> --migration <temporary-callback.py>`. Do not add naming or timestamp arguments. The permanent runner obtains the current UTC time internally and owns discovery, timestamped naming, backup/new tree copying, JSON I/O, callback enforcement, metadata restoration, generic integrity checks, and reporting.
10. Review the runner report. Report the exact source, `.bck`, and `.new` paths; file totals; migration/validation failures; changed paths; metadata results; and whether the `.new` tree is safe to adopt. Never replace the live source directory automatically.

## Rules
- Never modify or replace the source session directory.
- Create `<source>.<UTC timestamp>.bck` and `<source>.<UTC timestamp>.new`; refuse to overwrite existing destinations. The production CLI has no timestamp override: it always obtains current UTC internally. Tests may inject a clock only through the Python function API, never through a CLI argument available to agents or operators.
- Preserve directory and file metadata in both copies to the maximum supported by the OS. After all migrated JSON writes, restore the entire destination tree bottom-up so parent directory atime/mtime values are not left changed by child writes. Verify source-relative directory names are unchanged and verify mode, atime, and mtime for every non-symlink directory and file. Preserve ownership when permitted. Preserve embedded `meta.created_at` and `meta.updated_at` unless explicitly approved as changed paths. Do not claim filesystem birth-time preservation when unsupported or unverifiable.
- The backup must remain content-identical to the source. Apply transformations only inside `.new`.
- Reject callback modules with the wrong interface version, missing metadata, invalid changed paths, or missing functions.
- Run migrate twice and require identical results. Reject input mutation and non-JSON-object output.
- Reject any actual JSON change outside ALLOWED_CHANGED_PATHS. A declared parent path authorizes descendants.
- Run callback validation before and after serialization. Any error makes the output tree unsafe to adopt.
- Account for every discovered file and verify the source remains unchanged.
- Never silently discard or invent data. Stop on unsupported legacy shapes unless the approved mapping defines them.
- Keep migration-specific callbacks in the configured temporary directory, not in the skill or project.

## Output Format
```
Migration analysis
- Comparison: <before source> → <after source>
- Relevant before fields: <Go and JSON fragment>
- Relevant after fields: <Go and JSON fragment>
- Changes: <enumerated schema changes>
- Mapping: <enumerated old-to-new data mapping>
- Defaults/data loss/ambiguities: <details>
- Validation: <assertions>
- Approval: required before callback generation in interactive mode

Migration result
- Source: <path>
- Backup: <path>.bck
- Migrated: <path>.new
- Files found/migrated/unchanged/failed: <counts>
- Metadata preservation: <results and platform limitations>
- Validation: <passed or failures>
- Safe to adopt: <yes/no>
```

## Examples
Input: Compare HEAD with unstaged changes and migrate the configured sessions directory.
Output: Show only relevant old/new fields and JSON, enumerate mappings and risks, request approval, generate a temporary callback implementing migrate(document) and validate(before, after), then invoke the runner to create verified timestamped `.bck` and `.new` trees.

## Resources

### Scripts
- [migrate_sessions.py](scripts/migrate_sessions.py) — Executable script

#!/usr/bin/env python3
"""Safely apply a pure one-document migration callback to session JSON files."""

from __future__ import annotations

import argparse
import copy
import hashlib
import importlib.util
import inspect
import json
import os
import re
import shutil
import stat
import sys
from datetime import datetime, timezone
from pathlib import Path
from types import ModuleType
from typing import Any


class MigrationFailure(Exception):
    pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--migration", required=True, type=Path)
    parser.add_argument("--pattern", default="*/chat.json")
    parser.add_argument("--timestamp", help="UTC naming token for reproducible tests")
    return parser.parse_args()


def load_module(path: Path) -> ModuleType:
    if not path.is_file():
        raise MigrationFailure(f"migration module not found: {path}")
    spec = importlib.util.spec_from_file_location("session_migration_callback", path)
    if spec is None or spec.loader is None:
        raise MigrationFailure(f"cannot load migration module: {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    if getattr(module, "MIGRATION_API_VERSION", None) != 1:
        raise MigrationFailure("MIGRATION_API_VERSION must equal 1")
    migration_id = getattr(module, "MIGRATION_ID", None)
    if not isinstance(migration_id, str) or not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", migration_id):
        raise MigrationFailure("MIGRATION_ID must be a non-empty kebab-case string")
    allowed = getattr(module, "ALLOWED_CHANGED_PATHS", None)
    if not isinstance(allowed, set) or not all(isinstance(item, str) and item for item in allowed):
        raise MigrationFailure("ALLOWED_CHANGED_PATHS must be a set of non-empty strings")
    migrate = getattr(module, "migrate", None)
    validate = getattr(module, "validate", None)
    if not callable(migrate) or len(inspect.signature(migrate).parameters) != 1:
        raise MigrationFailure("migrate must have signature migrate(document: dict) -> dict")
    if not callable(validate) or len(inspect.signature(validate).parameters) != 2:
        raise MigrationFailure("validate must have signature validate(before: dict, after: dict) -> list[str]")
    return module


def read_bytes(path: Path) -> bytes:
    flags = os.O_RDONLY
    if hasattr(os, "O_NOATIME"):
        flags |= os.O_NOATIME
    try:
        fd = os.open(path, flags)
    except PermissionError:
        fd = os.open(path, os.O_RDONLY)
    try:
        chunks: list[bytes] = []
        while True:
            chunk = os.read(fd, 1024 * 1024)
            if not chunk:
                return b"".join(chunks)
            chunks.append(chunk)
    finally:
        os.close(fd)


def sha256(path: Path) -> str:
    return hashlib.sha256(read_bytes(path)).hexdigest()


def manifest(root: Path) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for path in sorted(root.rglob("*")):
        rel = path.relative_to(root).as_posix()
        info = path.lstat()
        entry: dict[str, Any] = {
            "kind": "symlink" if path.is_symlink() else "dir" if path.is_dir() else "file",
            "mode": stat.S_IMODE(info.st_mode),
            "mtime_ns": info.st_mtime_ns,
        }
        if path.is_symlink():
            entry["target"] = os.readlink(path)
        elif path.is_file():
            entry["size"] = info.st_size
            entry["sha256"] = sha256(path)
        result[rel] = entry
    return result


def archive_copy(source: Path, destination: Path) -> None:
    shutil.copytree(source, destination, symlinks=True, copy_function=shutil.copy2)


def compare_backup(source_manifest: dict[str, Any], backup_manifest: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if source_manifest.keys() != backup_manifest.keys():
        errors.append("backup inventory differs from source")
        return errors
    for rel, expected in source_manifest.items():
        actual = backup_manifest[rel]
        for key in ("kind", "mode", "mtime_ns", "size", "sha256", "target"):
            if key in expected and actual.get(key) != expected[key]:
                errors.append(f"backup metadata/content mismatch: {rel} ({key})")
    return errors


def json_diff_paths(before: Any, after: Any, path: str = "") -> set[str]:
    if type(before) is not type(after):
        return {path or "$"}
    if isinstance(before, dict):
        changed: set[str] = set()
        for key in before.keys() | after.keys():
            child = f"{path}.{key}" if path else key
            if key not in before or key not in after:
                changed.add(child)
            else:
                changed |= json_diff_paths(before[key], after[key], child)
        return changed
    if isinstance(before, list):
        changed = set()
        for index in range(max(len(before), len(after))):
            child = f"{path}[{index}]"
            if index >= len(before) or index >= len(after):
                changed.add(child)
            else:
                changed |= json_diff_paths(before[index], after[index], child)
        return changed
    return set() if before == after else {path or "$"}


def path_allowed(path: str, patterns: set[str]) -> bool:
    for pattern in patterns:
        regex = re.escape(pattern).replace(r"\[\*\]", r"\[\d+\]")
        if re.fullmatch(regex + r"(?:\..+|\[\d+\].*)?", path):
            return True
    return False


def restore_metadata(source: Path, destination: Path) -> list[str]:
    warnings: list[str] = []
    info = source.stat(follow_symlinks=False)
    try:
        os.chmod(destination, stat.S_IMODE(info.st_mode), follow_symlinks=False)
    except OSError as exc:
        warnings.append(f"mode not restored for {destination.name}: {exc}")
    if hasattr(os, "chown"):
        try:
            os.chown(destination, info.st_uid, info.st_gid, follow_symlinks=False)
        except (PermissionError, OSError) as exc:
            warnings.append(f"ownership not restored for {destination.name}: {exc}")
    try:
        os.utime(destination, ns=(info.st_atime_ns, info.st_mtime_ns), follow_symlinks=False)
    except OSError as exc:
        warnings.append(f"times not restored for {destination.name}: {exc}")
    return warnings


def atomic_write_json(path: Path, document: dict[str, Any], source: Path) -> list[str]:
    temporary = path.with_name(f".{path.name}.migrating-{os.getpid()}")
    try:
        with temporary.open("w", encoding="utf-8") as handle:
            json.dump(document, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        return restore_metadata(source, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def parse_json_object(data: bytes, label: str) -> dict[str, Any]:
    try:
        value = json.loads(data)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise MigrationFailure(f"{label}: invalid JSON: {exc}") from exc
    if not isinstance(value, dict):
        raise MigrationFailure(f"{label}: top-level JSON value must be an object")
    return value


def callback_errors(module: ModuleType, before: dict[str, Any], after: dict[str, Any]) -> list[str]:
    result = module.validate(copy.deepcopy(before), copy.deepcopy(after))
    if not isinstance(result, list) or not all(isinstance(item, str) for item in result):
        raise MigrationFailure("validate must return list[str]")
    return result


def run() -> int:
    args = parse_args()
    source = args.source.resolve()
    migration_path = args.migration.resolve()
    if not source.is_dir():
        raise MigrationFailure(f"source is not a directory: {source}")
    module = load_module(migration_path)
    token = args.timestamp or datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup = source.with_name(f"{source.name}.{token}.bck")
    destination = source.with_name(f"{source.name}.{token}.new")
    if backup.exists() or destination.exists():
        raise MigrationFailure("backup or migrated destination already exists")

    original_manifest = manifest(source)
    targets = sorted(path for path in source.rglob(args.pattern) if path.is_file())
    report: dict[str, Any] = {
        "migration_id": module.MIGRATION_ID,
        "source": str(source),
        "backup": str(backup),
        "new": str(destination),
        "files_found": len(targets),
        "migrated": 0,
        "unchanged": 0,
        "failed": 0,
        "failures": [],
        "warnings": [],
        "changed_paths": {},
        "safe_to_adopt": False,
    }

    archive_copy(source, backup)
    backup_errors = compare_backup(original_manifest, manifest(backup))
    if backup_errors:
        report["failures"].extend(backup_errors)
        report["failed"] += len(backup_errors)
        print(json.dumps(report, indent=2))
        return 1
    archive_copy(source, destination)

    for source_file in targets:
        rel = source_file.relative_to(source)
        destination_file = destination / rel
        try:
            before = parse_json_object(read_bytes(source_file), rel.as_posix())
            argument = copy.deepcopy(before)
            snapshot = copy.deepcopy(argument)
            after_a = module.migrate(argument)
            if argument != snapshot:
                raise MigrationFailure("migrate modified its input")
            after_b = module.migrate(copy.deepcopy(before))
            if after_a != after_b:
                raise MigrationFailure("migrate is not deterministic")
            if not isinstance(after_a, dict):
                raise MigrationFailure("migrate must return a dict")
            json.dumps(after_a, ensure_ascii=False)
            changed = json_diff_paths(before, after_a)
            unauthorized = sorted(path for path in changed if not path_allowed(path, module.ALLOWED_CHANGED_PATHS))
            if unauthorized:
                raise MigrationFailure(f"unauthorized changed paths: {', '.join(unauthorized)}")
            errors = callback_errors(module, before, after_a)
            if errors:
                raise MigrationFailure("callback validation failed: " + "; ".join(errors))
            report["changed_paths"][rel.as_posix()] = sorted(changed)
            if after_a == before:
                report["unchanged"] += 1
                continue
            report["warnings"].extend(restore_metadata(source_file, destination_file))
            report["warnings"].extend(atomic_write_json(destination_file, after_a, source_file))
            written = parse_json_object(read_bytes(destination_file), rel.as_posix())
            if written != after_a:
                raise MigrationFailure("serialized document differs from callback output")
            errors = callback_errors(module, before, written)
            if errors:
                raise MigrationFailure("post-write validation failed: " + "; ".join(errors))
            report["migrated"] += 1
        except Exception as exc:
            report["failed"] += 1
            report["failures"].append(f"{rel.as_posix()}: {exc}")

    if manifest(source) != original_manifest:
        report["failed"] += 1
        report["failures"].append("source tree changed during migration")
    if compare_backup(original_manifest, manifest(backup)):
        report["failed"] += 1
        report["failures"].append("backup changed after verification")
    report["safe_to_adopt"] = report["failed"] == 0
    print(json.dumps(report, indent=2))
    return 0 if report["safe_to_adopt"] else 1


def main() -> None:
    try:
        raise SystemExit(run())
    except MigrationFailure as exc:
        print(f"migration failed: {exc}", file=sys.stderr)
        raise SystemExit(1)


if __name__ == "__main__":
    main()

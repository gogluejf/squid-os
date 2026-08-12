#!/usr/bin/env python3
from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "migrate_sessions.py"
SPEC = importlib.util.spec_from_file_location("migrate_sessions", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MIGRATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MIGRATOR)


CALLBACK = '''\
MIGRATION_API_VERSION = 1
MIGRATION_ID = "test-migration"
ALLOWED_CHANGED_PATHS = {"migrated"}

def migrate(document):
    result = dict(document)
    result["migrated"] = True
    return result

def validate(before, after):
    return [] if after.get("migrated") is True else ["not migrated"]
'''


class MigrationRunnerTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = Path(self.tempdir.name)
        self.source = self.root / "sessions"
        self.session = self.source / "original-session-date"
        self.session.mkdir(parents=True)
        self.chat = self.session / "chat.json"
        self.chat.write_text(json.dumps({"meta": {"id": "one"}}), encoding="utf-8")
        self.callback = self.root / "callback.py"
        self.callback.write_text(CALLBACK, encoding="utf-8")

        self.file_time_ns = 1_650_000_001_123_456_789
        self.session_time_ns = 1_640_000_002_234_567_890
        self.root_time_ns = 1_630_000_003_345_678_901
        os.utime(self.chat, ns=(self.file_time_ns, self.file_time_ns))
        os.utime(self.session, ns=(self.session_time_ns, self.session_time_ns))
        os.utime(self.source, ns=(self.root_time_ns, self.root_time_ns))

    def tearDown(self) -> None:
        self.tempdir.cleanup()

    def test_cli_has_no_timestamp_override(self) -> None:
        with self.assertRaises(SystemExit) as raised, contextlib.redirect_stderr(io.StringIO()):
            MIGRATOR.parse_args([
                "--source", str(self.source),
                "--migration", str(self.callback),
                "--timestamp", "20000101T000000Z",
            ])
        self.assertEqual(raised.exception.code, 2)

    def test_injected_test_clock_and_directory_mtimes_are_preserved(self) -> None:
        fixed = datetime(2030, 1, 2, 3, 4, 5, tzinfo=timezone.utc)
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            result = MIGRATOR.run(
                ["--source", str(self.source), "--migration", str(self.callback)],
                now=lambda: fixed,
            )

        self.assertEqual(result, 0, output.getvalue())
        report = json.loads(output.getvalue())
        destination = Path(report["new"])
        backup = Path(report["backup"])
        self.assertEqual(destination.name, "sessions.20300102T030405Z.new")
        self.assertEqual(backup.name, "sessions.20300102T030405Z.bck")

        self.assertEqual((destination / "original-session-date").stat().st_mtime_ns, self.session_time_ns)
        self.assertEqual(destination.stat().st_mtime_ns, self.root_time_ns)
        self.assertEqual((destination / "original-session-date" / "chat.json").stat().st_mtime_ns, self.file_time_ns)
        self.assertEqual((backup / "original-session-date").stat().st_mtime_ns, self.session_time_ns)
        self.assertEqual(backup.stat().st_mtime_ns, self.root_time_ns)


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""
Set task summary fields in progress file.
ONLY writes summary fields — never status.

Usage:
    set-summary.py --progress <progress.json> --task-id <number> \
        [--done "text"] [--tradeoffs "text"] [--confidence "high|medium|low"] \
        [--failures "text"] [--decisions "text"] [--lessons-learned "text"]
"""
import sys
import json
import argparse

ALLOWED_FIELDS = {"done", "tradeoffs", "confidence", "failures", "decisions", "lessons_learned"}

def find_task_key(tasks, task_id):
    for key, entry in tasks.items():
        if entry.get("number") == task_id:
            return key
    return None

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--progress", required=True)
    parser.add_argument("--task-id", required=True)
    parser.add_argument("--done", default=None)
    parser.add_argument("--tradeoffs", default=None)
    parser.add_argument("--confidence", default=None)
    parser.add_argument("--failures", default=None)
    parser.add_argument("--decisions", default=None)
    parser.add_argument("--lessons-learned", dest="lessons_learned", default=None)
    args = parser.parse_args()

    with open(args.progress, 'r') as f:
        progress = json.load(f)

    key = find_task_key(progress.get("tasks", {}), args.task_id)
    if not key:
        print(f"Error: task {args.task_id} not found", file=sys.stderr)
        sys.exit(1)

    if "summary" not in progress["tasks"][key]:
        progress["tasks"][key]["summary"] = {}

    updated = False
    for field in ALLOWED_FIELDS:
        value = getattr(args, field)
        if value is not None:
            progress["tasks"][key]["summary"][field] = value
            updated = True

    if not updated:
        print("Warning: no summary fields provided", file=sys.stderr)

    with open(args.progress, 'w') as f:
        json.dump(progress, f, indent=2)
    print(f"Updated summary for {key}")

if __name__ == "__main__":
    main()

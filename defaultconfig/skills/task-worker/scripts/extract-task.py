#!/usr/bin/env python3
"""
Extract a single task with its epic and milestone context.
Outputs compact JSON to stdout.

Usage:
    extract-task.py --plan <plan.json> --task-id <number>
"""
import sys
import json
import argparse

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--plan", required=True)
    parser.add_argument("--task-id", required=True, help="Task number (e.g. '1.1', '2.3')")
    args = parser.parse_args()

    with open(args.plan, 'r') as f:
        plan = json.load(f)

    milestone = None
    task = None
    for m in plan.get("milestones", []):
        for t in m.get("tasks", []):
            if t.get("number") == args.task_id:
                milestone = m
                task = t
                break
        if task:
            break

    if not task:
        print(f"Error: task {args.task_id} not found", file=sys.stderr)
        sys.exit(1)

    output = {
        "plan": {
            "title": plan.get("title", ""),
            "why": plan.get("why", ""),
            "outcomes": plan.get("outcomes", "")
        },
        "milestone": {
            "number": milestone.get("number", ""),
            "name": milestone.get("name", ""),
            "objective": milestone.get("objective", ""),
            "success": milestone.get("success", "")
        },
        "task": {
            "number": task.get("number", ""),
            "name": task.get("name", ""),
            "type": task.get("type", ""),
            "what": task.get("what", ""),
            "why": task.get("why", ""),
            "files": task.get("files", []),
            "snippet": task.get("snippet", []),
            "acceptance": task.get("acceptance", []),
            "verify": task.get("verify", [])
        }
    }

    print(json.dumps(output, indent=2))

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""
Plan progress tracker.
Manages .plan-progress.json alongside .plan.json.

Usage:
    progress.py init --plan <plan.json>
    progress.py next-milestone --progress <progress.json>
    progress.py milestone-info --progress <progress.json> --milestone <number>
    progress.py milestone-complete --progress <progress.json> --milestone <number>
    progress.py next-task --progress <progress.json>
    progress.py mark-in-progress --progress <progress.json> --task-id <number>
    progress.py mark-done --progress <progress.json> --task-id <number>
    progress.py mark-failed --progress <progress.json> --task-id <number>
    progress.py status --progress <progress.json>
"""
import sys
import json
import argparse


def load_json(path):
    with open(path, 'r') as f:
        return json.load(f)


def save_json(path, data):
    with open(path, 'w') as f:
        json.dump(data, f, indent=2)


def find_task(tasks_dict, task_id):
    for key, entry in tasks_dict.items():
        if entry.get("number") == task_id:
            return key
    return None


def get_milestone_num(task_id):
    return task_id.split(".")[0]


def load_plan_for_progress(progress_path):
    plan_path = progress_path.replace(".plan-progress.json", ".plan.json")
    return load_json(plan_path)


def cmd_init(args):
    plan = load_json(args.plan)
    progress = {"tasks": {}}
    for milestone in plan.get("milestones", []):
        for task in milestone.get("tasks", []):
            num = task.get("number", "")
            if not num:
                continue
            progress["tasks"][num] = {
                "number": num,
                "status": "not_started",
                "name": task.get("name", ""),
                "milestone": milestone.get("name", ""),
                "milestone_number": milestone.get("number", ""),
            }
    progress_file = args.plan.replace(".plan.json", ".plan-progress.json")
    save_json(progress_file, progress)
    print(f"Initialized progress for {len(progress['tasks'])} tasks")


def strip_none(val):
    if val is None or val == "" or val.lower() == "none":
        return None
    return val


def cmd_get_summary(args):
    progress = load_json(args.progress)
    key = find_task(progress["tasks"], args.task_id)
    if not key:
        print(f"Error: task {args.task_id} not found", file=sys.stderr)
        sys.exit(1)
    s = progress["tasks"][key].get("summary", {})
    name = progress["tasks"][key].get("name", "")
    print(f"## {args.task_id}: {name}")
    for field, label in [("done", "Done"), ("tradeoffs", "Tradeoffs"), ("confidence", "Confidence"), ("failures", "Failures"), ("decisions", "Decisions"), ("lessons_learned", "Lessons Learned")]:
        val = strip_none(s.get(field))
        if val:
            print(f"- **{label}:** {val}")


def cmd_get_milestone_summary(args):
    progress = load_json(args.progress)
    plan = load_plan_for_progress(args.progress)
    tasks = progress.get("tasks", {})

    for m in plan.get("milestones", []):
        if m["number"] == args.milestone:
            for t in m.get("tasks", []):
                t_num = t["number"]
                t_data = tasks.get(t_num, {})
                s = t_data.get("summary", {})
                tradeoff = strip_none(s.get("tradeoffs"))
                lesson = strip_none(s.get("lessons_learned"))
                if tradeoff or lesson:
                    print(f"- **{t_num} {t['name']}**")
                    if tradeoff:
                        print(f"  - **Tradeoff:** {tradeoff}")
                    if lesson:
                        print(f"  - **Lesson:** {lesson}")
            return
    print(f"Error: milestone {args.milestone} not found", file=sys.stderr)
    sys.exit(1)


def cmd_next_milestone(args):
    progress = load_json(args.progress)
    plan = load_plan_for_progress(args.progress)
    tasks = progress.get("tasks", {})

    for m in plan.get("milestones", []):
        m_num = m["number"]
        m_tasks = m.get("tasks", [])
        for t in m_tasks:
            t_num = t["number"]
            if t_num in tasks and tasks[t_num]["status"] != "done":
                print(f"{m_num} {m['name']}")
                return
    print("all_done")


def cmd_milestone_info(args):
    progress = load_json(args.progress)
    plan = load_plan_for_progress(args.progress)
    tasks = progress.get("tasks", {})

    for m in plan.get("milestones", []):
        if m["number"] == args.milestone:
            m_tasks = m.get("tasks", [])
            total = len(m_tasks)
            print(f"{m['number']} {m['name']} ({total} tasks)")
            for t in m_tasks:
                t_num = t["number"]
                t_data = tasks.get(t_num, {})
                status = t_data.get("status", "not_started")
                mark = "✓" if status == "done" else "◯"
                if status == "in_progress":
                    mark = "◷"
                if status == "failed":
                    mark = "✗"
                print(f"  {mark} {t_num}  {t['name']}")
            return
    print(f"Error: milestone {args.milestone} not found", file=sys.stderr)
    sys.exit(1)


def cmd_milestone_complete(args):
    progress = load_json(args.progress)
    plan = load_plan_for_progress(args.progress)
    tasks = progress.get("tasks", {})

    for m in plan.get("milestones", []):
        if m["number"] == args.milestone:
            for t in m.get("tasks", []):
                t_data = tasks.get(t["number"], {})
                if t_data.get("status") != "done":
                    print("no")
                    return
            print("yes")
            return
    print(f"Error: milestone {args.milestone} not found", file=sys.stderr)
    sys.exit(1)


def cmd_next_task(args):
    progress = load_json(args.progress)
    tasks = progress.get("tasks", {})
    for key in sorted(tasks.keys()):
        if tasks[key]["status"] != "done":
            t = tasks[key]
            print(f"{t['number']} {t['name']}")
            return
    print("all_done")


def cmd_status(args):
    progress = load_json(args.progress)
    tasks = progress.get("tasks", {})
    total = len(tasks)
    done = sum(1 for t in tasks.values() if t["status"] == "done")
    failed = sum(1 for t in tasks.values() if t["status"] == "failed")
    in_progress = sum(1 for t in tasks.values() if t["status"] == "in_progress")
    not_started = total - done - failed - in_progress
    print(f"Total: {total} | Done: {done} | In Progress: {in_progress} | Not Started: {not_started} | Failed: {failed}")


def main():
    parser = argparse.ArgumentParser(description="Plan progress tracker")
    subparsers = parser.add_subparsers(dest="command")

    p = subparsers.add_parser("init")
    p.add_argument("--plan", required=True)

    p = subparsers.add_parser("next-milestone")
    p.add_argument("--progress", required=True)

    p = subparsers.add_parser("milestone-info")
    p.add_argument("--progress", required=True)
    p.add_argument("--milestone", required=True)

    p = subparsers.add_parser("milestone-complete")
    p.add_argument("--progress", required=True)
    p.add_argument("--milestone", required=True)

    p = subparsers.add_parser("next-task")
    p.add_argument("--progress", required=True)

    p = subparsers.add_parser("mark-in-progress")
    p.add_argument("--progress", required=True)
    p.add_argument("--task-id", required=True)

    p = subparsers.add_parser("mark-done")
    p.add_argument("--progress", required=True)
    p.add_argument("--task-id", required=True)

    p = subparsers.add_parser("mark-failed")
    p.add_argument("--progress", required=True)
    p.add_argument("--task-id", required=True)

    p = subparsers.add_parser("status")
    p.add_argument("--progress", required=True)

    p = subparsers.add_parser("get-summary")
    p.add_argument("--progress", required=True)
    p.add_argument("--task-id", required=True)

    p = subparsers.add_parser("get-milestone-summary")
    p.add_argument("--progress", required=True)
    p.add_argument("--milestone", required=True)

    args = parser.parse_args()

    if args.command == "init":
        cmd_init(args)
    elif args.command == "next-milestone":
        cmd_next_milestone(args)
    elif args.command == "milestone-info":
        cmd_milestone_info(args)
    elif args.command == "milestone-complete":
        cmd_milestone_complete(args)
    elif args.command == "next-task":
        cmd_next_task(args)
    elif args.command == "mark-done":
        progress = load_json(args.progress)
        key = find_task(progress["tasks"], args.task_id)
        if key:
            progress["tasks"][key]["status"] = "done"
            save_json(args.progress, progress)
            print(f"Marked {key} done")
        else:
            print(f"Error: task {args.task_id} not found", file=sys.stderr)
            sys.exit(1)
    elif args.command == "mark-failed":
        progress = load_json(args.progress)
        key = find_task(progress["tasks"], args.task_id)
        if key:
            progress["tasks"][key]["status"] = "failed"
            save_json(args.progress, progress)
            print(f"Marked {key} failed")
        else:
            print(f"Error: task {args.task_id} not found", file=sys.stderr)
            sys.exit(1)
    elif args.command == "mark-in-progress":
        progress = load_json(args.progress)
        key = find_task(progress["tasks"], args.task_id)
        if key:
            progress["tasks"][key]["status"] = "in_progress"
            save_json(args.progress, progress)
            print(f"Marked {key} in progress")
        else:
            print(f"Error: task {args.task_id} not found", file=sys.stderr)
            sys.exit(1)
    elif args.command == "status":
        cmd_status(args)
    elif args.command == "get-summary":
        cmd_get_summary(args)
    elif args.command == "get-milestone-summary":
        cmd_get_milestone_summary(args)
    else:
        print("Error: no command specified")
        sys.exit(1)


if __name__ == "__main__":
    main()

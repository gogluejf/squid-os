#!/usr/bin/env python3
"""
Deterministic Plan Builder.
Builds a plan step-by-step (Epic -> Milestone -> Task) into a JSON state, then exports to Markdown.

Usage:
    build_plan.py init --epic "Name" --why "Why" --outcomes "Outcomes" --dir ./plans
    build_plan.py add-milestone --name "Name" --pattern "Pattern" --objective "Text" --success "Text" --diagram "Text" --dir ./plans
    build_plan.py add-task --name "Name" --what "..." --why "..." --files "..." --snippet "..." --acceptance "..." --verify "..." --dir ./plans
    build_plan.py export --dir ./plans
"""
import sys
import os
import json
import argparse
import re

def get_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dir", default=".")
    subparsers = parser.add_subparsers(dest="command")

    # Init
    p_init = subparsers.add_parser("init")
    p_init.add_argument("--plan-name", required=True, help="The base filename slug (e.g., user-auth)")
    p_init.add_argument("--title", required=True, help="The display title (e.g., User Auth)")
    p_init.add_argument("--why", default="")
    p_init.add_argument("--outcomes", default="")

    # Add Milestone
    p_ms = subparsers.add_parser("add-milestone")
    p_ms.add_argument("--name", required=True)
    p_ms.add_argument("--pattern", action='append', default=None)
    p_ms.add_argument("--objective", default="")
    p_ms.add_argument("--success", default="")
    p_ms.add_argument("--diagram", default="")

    # Add Task
    p_task = subparsers.add_parser("add-task")
    p_task.add_argument("--name", required=True)
    p_task.add_argument("--type", required=True, choices=["feature", "refactor", "bug", "test", "doc", "chore"], help="Kind of change: feature (new behavior), refactor (no behavior change), bug, test, doc, chore.")
    p_task.add_argument("--what", required=True)
    p_task.add_argument("--why", required=True)
    p_task.add_argument("--files", action='append', default=None)
    p_task.add_argument("--snippet", action='append', default=None)
    p_task.add_argument("--acceptance", action='append', default=None)
    p_task.add_argument("--verify", action='append', required=True)

    # Export
    p_export = subparsers.add_parser("export")

    return parser.parse_args()

def load_state(path):
    fp = os.path.join(path, ".plan.json")
    if os.path.exists(fp):
        with open(fp, "r") as f:
            return json.load(f)
    return None

def save_state(path, state):
    fp = os.path.join(path, ".plan.json")
    with open(fp, "w") as f:
        json.dump(state, f, indent=2)

def validate_diagram(diagram):
    """
    Validate Mermaid diagram syntax for 9.4.3 compatibility.
    Checks for known-breaking patterns and returns a list of errors.
    """
    if not diagram:
        return []
    errors = []
    
    # Check that it starts with a known Mermaid directive
    directives = ["graph ", "flowchart ", "sequenceDiagram", "stateDiagram-v2", "classDiagram", "gantt", "pie", "gitGraph"]
    first_line = diagram.strip().split("\n")[0].strip().lower()
    valid_start = any(first_line.startswith(d.lower()) for d in directives)
    if not valid_start:
        errors.append(f"Diagram must start with a known Mermaid directive (graph, flowchart, sequenceDiagram, etc.), got: '{first_line}'")
    
    # Ban << >> stereotype syntax (classDiagram <<interface>> not supported in 9.4.3)
    if re.search(r"<<.*>>", diagram):
        errors.append("Diagram contains << >> stereotype syntax — not supported in Mermaid 9.4.3. Remove <<interface>> and similar.")
    
    # Ban <|-- and --|> arrows (use ..> for dependency, -- for association)
    if "<|--" in diagram or "--|>" in diagram:
        errors.append("Diagram contains <|-- or --|> arrow syntax — not reliably supported. Use ..> for dependency or -- for association.")
    
    # Ban bare | characters in node labels (skill rule)
    lines = diagram.split("\n")
    for i, line in enumerate(lines):
        stripped = line.strip()
        # Skip lines that are pure arrow definitions (relationships between nodes)
        if re.match(r'^[\w\s\.]+\s*[-+>]+\s*[\w\s\.]+$', stripped):
            continue
        # Check for | in what looks like a node label (inside [] or plain text with spaces)
        if "|" in stripped and not stripped.startswith(("    ", "\t")):
            # Allow it in relationship lines but not in node definitions
            if re.search(r'\[.*\|.*\]', stripped) or (re.search(r'[A-Z].*\|.*[A-Z]', stripped) and '[' not in stripped):
                errors.append(f"Line {i+1} contains | in node label: '{stripped}' — pipe characters break rendering.")
    
    return errors


def main():
    args = get_args()
    os.makedirs(args.dir, exist_ok=True)
    state = load_state(args.dir)

    if args.command == "init":
        state = {
            "plan_name": args.plan_name,
            "title": args.title,
            "why": args.why,
            "outcomes": args.outcomes,
            "milestones": []
        }
        save_state(args.dir, state)
        print("Initialized Plan: {} ({})".format(args.title, args.plan_name))

    elif args.command == "add-milestone":
        if not state:
            print("Error: Run 'init' first.")
            sys.exit(1)
        
        # Validate diagram before accepting
        if args.diagram:
            errors = validate_diagram(args.diagram)
            if errors:
                print("Error: Diagram validation failed:")
                for e in errors:
                    print(f"  - {e}")
                sys.exit(1)
        
        num = len(state["milestones"]) + 1
        state["milestones"].append({
            "number": str(num),
            "name": args.name,
            "pattern": args.pattern or [],
            "objective": args.objective,
            "success": args.success,
            "diagram": args.diagram,
            "tasks": []
        })
        save_state(args.dir, state)
        print(f"Added Milestone #{num}: {args.name}")

    elif args.command == "add-task":
        if not state or not state["milestones"]:
            print("Error: Add a milestone first.")
            sys.exit(1)
        current_milestone = state["milestones"][-1]
        t_num = len(current_milestone["tasks"]) + 1
        full_num = f"{current_milestone['number']}.{t_num}"
        current_milestone["tasks"].append({
            "number": full_num,
            "name": args.name,
            "type": args.type,
            "what": args.what,
            "why": args.why,
            "files": args.files or [],
            "snippet": args.snippet or [],
            "acceptance": args.acceptance or [],
            "verify": args.verify
        })
        save_state(args.dir, state)
        print(f"Added Task {full_num}: {args.name} to {current_milestone['name']}")

    elif args.command == "export":
        if not state:
            print("Error: No state found.")
            sys.exit(1)
        
        md = []
        md.append(f"# EPIC: {state['title']}")
        md.append(f"Why: {state['why']}")
        md.append(f"Outcomes: {state['outcomes']}")
        md.append("")
        
        for m in state["milestones"]:
            md.append(f"## MILESTONE: {m['number']} - {m['name']}")
            for p in m['pattern']:
                md.append(f"Pattern: {p}")
            md.append(f"Objective: {m['objective']}")
            md.append(f"Success: {m['success']}")
            if m['diagram']:
                md.append(f"Diagram: {m['diagram']}")
            md.append("")
            
            for t in m['tasks']:
                md.append(f"### TASK: {t['number']} - {t['name']}")
                md.append(f"Type: {t['type']}")
                md.append(f"What: {t['what']}")
                md.append(f"Why: {t['why']}")
                for f in t['files']:
                    md.append(f"Files: {f}")
                for s in t['snippet']:
                    # Escape newlines and tabs to keep Snippet on a single line in Markdown
                    escaped_s = s.replace('\n', '\\n').replace('\t', '\\t')
                    md.append(f"Snippet: {escaped_s}")
                for a in t['acceptance']:
                    md.append(f"Acceptance: {a}")
                for v in t['verify']:
                    md.append(f"Verification: {v}")
                md.append("")

        slug = state['plan_name']
        out_file = os.path.join(args.dir, f"{slug}.md")
        with open(out_file, "w") as f:
            f.write("\n".join(md))
        print(f"Exported to {out_file}")
        # Keep .plan.json as the source of truth (already saved by save_state)

if __name__ == "__main__":
    main()

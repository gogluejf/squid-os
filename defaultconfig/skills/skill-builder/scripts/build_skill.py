#!/usr/bin/env python3
"""
Deterministic Skill Builder.
Builds a squid-os skill step-by-step into a JSON state, then exports to SKILL.md.

Usage:
    build_skill.py init --name my-skill --dir ./skills --description "..." --overview "..." --allowed-tools "bash read_file"
    build_skill.py add-variables --dir ./skills/my-skill --text "<skill-folder> is the directory containing this SKILL.md"
    build_skill.py add-instructions --dir ./skills/my-skill --text "Step 1. Do X..."
    build_skill.py add-rules --dir ./skills/my-skill --text "No assumptions."
    build_skill.py add-output-format --dir ./skills/my-skill --text "..."
    build_skill.py add-examples --dir ./skills/my-skill --text "..."
    build_skill.py add-metadata --dir ./skills/my-skill --key "author" --value "me"
    build_skill.py add-script --dir ./skills/my-skill --name validate.sh [--content "..." or --from-file /tmp/validate.sh]
    build_skill.py add-asset --dir ./skills/my-skill --name template.md [--content "..." or --from-file /tmp/t.md]
    build_skill.py add-reference --dir ./skills/my-skill --name api-docs.md [--content "..." or --from-file /tmp/api.md]
    build_skill.py set-version --dir ./skills/my-skill --version "1.0.0"
    build_skill.py set-license --dir ./skills/my-skill --license "MIT"
    build_skill.py finalize --dir ./skills/my-skill
"""
import sys
import os
import json
import argparse
import re


STATE_FILE = ".skill.json"

# ── Helpers ────────────────────────────────────────────────────────────────


def load_state(dirpath):
    fp = os.path.join(dirpath, STATE_FILE)
    if os.path.exists(fp):
        with open(fp, "r") as f:
            return json.load(f), fp
    # Check subdirectories for .skill.json (in case --dir is the parent)
    try:
        for entry in os.listdir(dirpath):
            sub = os.path.join(dirpath, entry)
            if os.path.isdir(sub):
                sub_fp = os.path.join(sub, STATE_FILE)
                if os.path.exists(sub_fp):
                    with open(sub_fp, "r") as f:
                        return json.load(f), sub_fp
    except OSError:
        pass
    return None, None


def save_state(dirpath, state, state_path=None):
    if state_path:
        fp = state_path
    else:
        fp = os.path.join(dirpath, STATE_FILE)
    with open(fp, "w") as f:
        json.dump(state, f, indent=2)
    return fp


def ensure_dir(dirpath):
    os.makedirs(dirpath, exist_ok=True)


def read_from_file(path):
    with open(path, "r") as f:
        return f.read()


def yaml_value(s):
    """Return a YAML-safe string: quoted if it contains problematic chars."""
    if s is None or s == "":
        return ""
    s = str(s)
    needs_quote = any(c in s for c in [':', '#', '{', '}', '"'])
    if needs_quote:
        escaped = s.replace('"', '\\"')
        return '"{}"'.format(escaped)
    return s


# ── Subcommand parsers ─────────────────────────────────────────────────────


def get_args():
    parser = argparse.ArgumentParser(
        description="Deterministic squid-os Skill Builder"
    )
    parser.add_argument(
        "--dir",
        default=".",
        help="Parent directory (init creates <dir>/<name>/, other commands use <dir>/<name>/ or <dir> if .skill.json is there)",
    )
    subparsers = parser.add_subparsers(dest="command")

    # ── init ───────────────────────────────────────────────────────────
    p = subparsers.add_parser("init", help="Create a new skill skeleton")
    p.add_argument("--name", required=True, help="Skill name (kebab-case)")
    p.add_argument("--description", required=True, help="Short description (max 1024 chars)")
    p.add_argument("--overview", default="", help="One-paragraph summary")
    p.add_argument("--allowed-tools", default="", help="Space-separated tool names")

    # ── set-version ────────────────────────────────────────────────────
    p = subparsers.add_parser("set-version", help="Set version")
    p.add_argument("--version", required=True)

    # ── set-license ────────────────────────────────────────────────────
    p = subparsers.add_parser("set-license", help="Set license")
    p.add_argument("--license", required=True)

    # ── add-metadata ───────────────────────────────────────────────────
    p = subparsers.add_parser("add-metadata", help="Add a metadata key-value pair")
    p.add_argument("--key", required=True)
    p.add_argument("--value", required=True)

    # ── add-variables ──────────────────────────────────────────────
    p = subparsers.add_parser("add-variables", help="Set contextual tags for path construction (NOT shell env vars)")
    p.add_argument("--text", default="", help="Variables text to set (tag definitions)")
    p.add_argument("--from-file", default=None, help="Read variables text from file")

    # ── add-instructions ───────────────────────────────────────────────
    p = subparsers.add_parser("add-instructions", help="Append text to instructions")
    p.add_argument("--text", default="", help="Instruction text to append")
    p.add_argument("--from-file", default=None, help="Read instruction text from file")

    # ── add-rules ──────────────────────────────────────────────────────
    p = subparsers.add_parser("add-rules", help="Append text to rules")
    p.add_argument("--text", default="", help="Rule text to append")
    p.add_argument("--from-file", default=None, help="Read rule text from file")

    # ── add-output-format ──────────────────────────────────────────────
    p = subparsers.add_parser("add-output-format", help="Set output format section")
    p.add_argument("--text", default="", help="Output format text")
    p.add_argument("--from-file", default=None, help="Read output format from file")

    # ── add-examples ───────────────────────────────────────────────────
    p = subparsers.add_parser("add-examples", help="Set examples section")
    p.add_argument("--text", default="", help="Examples text")
    p.add_argument("--from-file", default=None, help="Read examples from file")

    # ── add-script ─────────────────────────────────────────────────────
    p = subparsers.add_parser("add-script", help="Add an executable script")
    p.add_argument("--name", required=True, help="Script filename (e.g. validate.sh)")
    p.add_argument("--content", default=None, help="Script content (short)")
    p.add_argument("--from-file", default=None, help="Read script from file (for long content)")

    # ── add-asset ──────────────────────────────────────────────────────
    p = subparsers.add_parser("add-asset", help="Add a template or resource file")
    p.add_argument("--name", required=True, help="Asset filename (e.g. template.md)")
    p.add_argument("--content", default=None, help="Asset content (short)")
    p.add_argument("--from-file", default=None, help="Read asset from file (for long content)")

    # ── add-reference ──────────────────────────────────────────────────
    p = subparsers.add_parser("add-reference", help="Add a reference document")
    p.add_argument("--name", required=True, help="Reference filename (e.g. api-docs.md)")
    p.add_argument("--content", default=None, help="Reference content (short)")
    p.add_argument("--from-file", default=None, help="Read reference from file (for long content)")

    # ── finalize ───────────────────────────────────────────────────────
    p = subparsers.add_parser("finalize", help="Export SKILL.md, write files, remove state")

    # ── status ─────────────────────────────────────────────────────────
    p = subparsers.add_parser("status", help="Show current state summary")

    return parser.parse_args()


# ── Commands ───────────────────────────────────────────────────────────────


def cmd_init(args):
    if len(args.description) > 1024:
        print("Error: Description exceeds 1024 characters (got {})".format(len(args.description)))
        sys.exit(1)

    name_re = re.compile(r"^[a-z](?:[a-z0-9-]{0,62}[a-z0-9])?$")
    if not name_re.match(args.name):
        print("Error: Name must be lowercase, digits, hyphens only; no leading/trailing/consecutive hyphens")
        sys.exit(1)
    if len(args.name) > 64:
        print("Error: Name too long")
        sys.exit(1)

    # Create the skill directory. If --dir already points at the skill
    # directory (basename matches the name, or a .skill.json exists there),
    # use it as-is instead of nesting <dir>/<name>.
    base = os.path.abspath(args.dir)
    if os.path.basename(base) == args.name or os.path.exists(os.path.join(base, STATE_FILE)):
        skill_dir = base
    else:
        skill_dir = os.path.join(base, args.name)
    ensure_dir(skill_dir)

    state = {
        "name": args.name,
        "description": args.description,
        "version": "",
        "license": "",
        "allowed_tools": args.allowed_tools,
        "metadata": {},
        "overview": args.overview,
        "variables": "",
        "instructions": "",
        "rules": "",
        "output_format": "",
        "examples": "",
        "scripts": {},
        "assets": {},
        "references": {},
    }
    save_state(skill_dir, state)
    print("Initialized skill '{}' at {}".format(args.name, skill_dir))


def cmd_set_version(args):
    state, sp = _load(args)
    state["version"] = args.version
    _save(args, state, sp)
    print("Set version: {}".format(args.version))


def cmd_set_license(args):
    state, sp = _load(args)
    state["license"] = args.license
    _save(args, state, sp)
    print("Set license: {}".format(args.license))


def cmd_add_metadata(args):
    state, sp = _load(args)
    state["metadata"][args.key] = args.value
    _save(args, state, sp)
    print("Added metadata: {} = {}".format(args.key, args.value))


def _load(args):
    """Load state, returning (state, state_path) or exiting."""
    state, state_path = load_state(args.dir)
    if state is None:
        print("Error: No state found. Run 'init' first.")
        sys.exit(1)
    return state, state_path


def _save(args, state, state_path):
    save_state(args.dir, state, state_path)


def _read_text(args):
    """Helper: get text from --text or --from-file or stdin."""
    if args.from_file:
        return read_from_file(args.from_file)
    if args.text:
        return args.text
    # Check stdin
    if not sys.stdin.isatty():
        return sys.stdin.read()
    return ""


def cmd_add_variables(args):
    state, sp = _load(args)
    text = _read_text(args)
    if text:
        state["variables"] = text
    _save(args, state, sp)
    print("Set variables ({} chars)".format(len(state["variables"])))


def cmd_add_instructions(args):
    state, sp = _load(args)
    text = _read_text(args)
    if text:
        if state["instructions"]:
            state["instructions"] += "\n\n" + text
        else:
            state["instructions"] = text
    _save(args, state, sp)
    print("Added instructions ({} chars)".format(len(state["instructions"])))


def cmd_add_rules(args):
    state, sp = _load(args)
    text = _read_text(args)
    if text:
        if state["rules"]:
            state["rules"] += "\n\n" + text
        else:
            state["rules"] = text
    _save(args, state, sp)
    print("Added rules ({} chars)".format(len(state["rules"])))


def cmd_add_output_format(args):
    state, sp = _load(args)
    text = _read_text(args)
    state["output_format"] = text
    _save(args, state, sp)
    print("Set output format ({} chars)".format(len(text)))


def cmd_add_examples(args):
    state, sp = _load(args)
    text = _read_text(args)
    state["examples"] = text
    _save(args, state, sp)
    print("Set examples ({} chars)".format(len(text)))


def _add_named_resource(state, category, name, content):
    """Add a named file to scripts/assets/references."""
    if name in state[category]:
        print("Warning: {} '{}' already exists, overwriting.".format(category.rstrip("s"), name))
    state[category][name] = content


def _get_resource_content(args):
    """Get content from --content, --from-file, or stdin."""
    if args.from_file:
        return read_from_file(args.from_file)
    if args.content is not None:
        return args.content
    if not sys.stdin.isatty():
        return sys.stdin.read()
    return ""


def cmd_add_script(args):
    state, sp = _load(args)
    content = _get_resource_content(args)
    _add_named_resource(state, "scripts", args.name, content)
    _save(args, state, sp)
    print("Added script: {}".format(args.name))


def cmd_add_asset(args):
    state, sp = _load(args)
    content = _get_resource_content(args)
    _add_named_resource(state, "assets", args.name, content)
    _save(args, state, sp)
    print("Added asset: {}".format(args.name))


def cmd_add_reference(args):
    state, sp = _load(args)
    content = _get_resource_content(args)
    _add_named_resource(state, "references", args.name, content)
    _save(args, state, sp)
    print("Added reference: {}".format(args.name))


def cmd_status(args):
    state, sp = _load(args)
    print("Skill: {}".format(state["name"]))
    print("Description: {}".format(state["description"][:80]))
    if state["version"]:
        print("Version: {}".format(state["version"]))
    if state["license"]:
        print("License: {}".format(state["license"]))
    if state["allowed_tools"]:
        print("Allowed tools: {}".format(state["allowed_tools"]))
    print("Metadata: {}".format(state["metadata"]))
    print("Overview: {} chars".format(len(state["overview"])))
    print("Variables: {} chars".format(len(state.get("variables", ""))))
    print("Instructions: {} chars".format(len(state["instructions"])))
    print("Rules: {} chars".format(len(state["rules"])))
    print("Output format: {} chars".format(len(state["output_format"])))
    print("Examples: {} chars".format(len(state["examples"])))
    print("Scripts: {}".format(list(state["scripts"].keys())))
    print("Assets: {}".format(list(state["assets"].keys())))
    print("References: {}".format(list(state["references"].keys())))


# ── Finalize ───────────────────────────────────────────────────────────────


def write_skill_md(dirpath, state):
    """Write SKILL.md from state."""
    lines = []

    # ── Frontmatter ──────────────────────────────────────────────────
    lines.append("---")

    lines.append("name: {}".format(yaml_value(state["name"])))
    lines.append("description: {}".format(yaml_value(state["description"])))
    if state.get("version"):
        lines.append("version: {}".format(yaml_value(state["version"])))
    if state.get("license"):
        lines.append("license: {}".format(yaml_value(state["license"])))
    if state.get("allowed_tools"):
        lines.append("allowed-tools: {}".format(yaml_value(state["allowed_tools"])))
    if state.get("metadata"):
        lines.append("metadata:")
        for k, v in state["metadata"].items():
            lines.append("  {}: {}".format(k, yaml_value(v)))

    lines.append("---")

    # ── Body ─────────────────────────────────────────────────────────
    if state.get("overview"):
        lines.append("")
        lines.append("## Overview")
        lines.append(state["overview"])

    if state.get("variables"):
        lines.append("")
        lines.append("## Variables")
        lines.append(state["variables"])

    if state.get("instructions"):
        lines.append("")
        lines.append("## Instructions")
        lines.append(state["instructions"])

    if state.get("rules"):
        lines.append("")
        lines.append("## Rules")
        lines.append(state["rules"])

    if state.get("output_format"):
        lines.append("")
        lines.append("## Output Format")
        lines.append("```")
        lines.append(state["output_format"])
        lines.append("```")

    if state.get("examples"):
        lines.append("")
        lines.append("## Examples")
        lines.append(state["examples"])

    # ── Resources ────────────────────────────────────────────────────
    has_resources = bool(state.get("scripts") or state.get("references") or state.get("assets"))
    if has_resources:
        lines.append("")
        lines.append("## Resources")

        if state.get("scripts"):
            lines.append("")
            lines.append("### Scripts")
            for fname in sorted(state["scripts"].keys()):
                lines.append("- [{}]({}) — Executable script".format(fname, "scripts/{}".format(fname)))

        if state.get("references"):
            lines.append("")
            lines.append("### References")
            for fname in sorted(state["references"].keys()):
                lines.append("- [{}]({}) — Additional documentation".format(fname, "references/{}".format(fname)))

        if state.get("assets"):
            lines.append("")
            lines.append("### Assets")
            for fname in sorted(state["assets"].keys()):
                lines.append("- [{}]({}) — Template or resource file".format(fname, "assets/{}".format(fname)))

    lines.append("")

    return "\n".join(lines)


def cmd_finalize(args):
    state, state_path = _load(args)
    # The skill dir is where .skill.json lives
    skill_dir = os.path.dirname(state_path)

    if not state.get("instructions"):
        print("Warning: No instructions set. The skill will be incomplete.")

    # Write SKILL.md
    skill_md = write_skill_md(skill_dir, state)
    skill_path = os.path.join(skill_dir, "SKILL.md")
    with open(skill_path, "w") as f:
        f.write(skill_md)
    print("Wrote: SKILL.md")

    # Write scripts
    if state.get("scripts"):
        scripts_dir = os.path.join(skill_dir, "scripts")
        os.makedirs(scripts_dir, exist_ok=True)
        for fname, content in state["scripts"].items():
            path = os.path.join(scripts_dir, fname)
            with open(path, "w") as f:
                f.write(content)
            os.chmod(path, 0o755)
            print("Wrote: scripts/{}".format(fname))

    # Write assets
    if state.get("assets"):
        assets_dir = os.path.join(skill_dir, "assets")
        os.makedirs(assets_dir, exist_ok=True)
        for fname, content in state["assets"].items():
            path = os.path.join(assets_dir, fname)
            with open(path, "w") as f:
                f.write(content)
            print("Wrote: assets/{}".format(fname))

    # Write references
    if state.get("references"):
        refs_dir = os.path.join(skill_dir, "references")
        os.makedirs(refs_dir, exist_ok=True)
        for fname, content in state["references"].items():
            path = os.path.join(refs_dir, fname)
            with open(path, "w") as f:
                f.write(content)
            print("Wrote: references/{}".format(fname))

    # Remove state file
    if os.path.exists(state_path):
        os.remove(state_path)
        print("Removed: {}".format(STATE_FILE))

    print("Skill '{}' finalized at {}".format(state["name"], skill_dir))


# ── Main ───────────────────────────────────────────────────────────────────


def main():
    args = get_args()

    if args.command is None:
        print("Error: No subcommand specified. Use: init, add-instructions, add-script, finalize, etc.")
        sys.exit(1)

    ensure_dir(args.dir)

    commands = {
        "init": cmd_init,
        "set-version": cmd_set_version,
        "set-license": cmd_set_license,
        "add-metadata": cmd_add_metadata,
        "add-variables": cmd_add_variables,
        "add-instructions": cmd_add_instructions,
        "add-rules": cmd_add_rules,
        "add-output-format": cmd_add_output_format,
        "add-examples": cmd_add_examples,
        "add-script": cmd_add_script,
        "add-asset": cmd_add_asset,
        "add-reference": cmd_add_reference,
        "finalize": cmd_finalize,
        "status": cmd_status,
    }

    handler = commands.get(args.command)
    if handler is None:
        print("Error: Unknown command '{}'".format(args.command))
        sys.exit(1)

    handler(args)


if __name__ == "__main__":
    main()

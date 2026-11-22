#!/usr/bin/env python3
"""Cheap fact-fetcher for session-resume. Read-only.

Prints JSON with mechanical facts only:
  - recent session folders (filtered to --working-dir when possible)
  - per session: created/updated, model, message count, crash line,
    FIRST user msg + LAST user msg (short pointers, NOT a summary),
    commits made DURING the session window
  - git state of --working-dir

The AGENT must read chat.json itself for the real story. This script
only saves tokens on listing and git archaeology.
"""
import argparse, json, os, re, subprocess, sys
from datetime import datetime

DEFAULT_SESSIONS = os.path.expanduser("~/.config/squid-os/sessions")


def clip(text, n=200):
    text = (text or "").strip()
    return text[:n] + ("…" if len(text) > n else "")


def parse_ts(s):
    try:
        return datetime.fromisoformat(s.replace("Z", "+00:00"))
    except Exception:
        return None


def load_session(folder, sessions_dir, working_dir):
    path = os.path.join(sessions_dir, folder, "chat.json")
    out = {
        "folder": folder, "created": None, "updated": None,
        "working_dir": None, "model": None, "n_messages": 0,
        "crash": None, "first_user": None, "last_user": None,
        "readable": False,
    }
    try:
        with open(path, encoding="utf-8") as f:
            d = json.load(f)
    except Exception as e:
        out["error"] = str(e)
        return out
    out["readable"] = True
    meta = d.get("meta") or {}
    out["created"] = meta.get("created_at")
    out["updated"] = meta.get("updated_at")
    initial = d.get("initial") or {}
    out["working_dir"] = initial.get("working_dir")
    infer = initial.get("inference") or {}
    out["model"] = infer.get("model")
    msgs = d.get("messages") or []
    out["n_messages"] = len(msgs)

    users, synthetics = [], []
    for m in msgs:
        role = m.get("role")
        text = m.get("text") or ""
        if role == "user" and text.strip():
            users.append(text)
        elif role in ("synthetic", "internal") and text.strip():
            synthetics.append(text)

    # Pointers only — the agent reads the full file for the story.
    if users:
        out["first_user"] = clip(users[0])
        out["last_user"] = clip(users[-1])

    # Skill-invocation signals (chain continuity evidence):
    #  - user typed @skill:name (real user message)
    #  - assistant issued a skill_load tool call
    #  - synthetic/internal message about a skill load/unload ("Skill \"x\" has been ...")
    at_skills, loaded, synth_skill_msgs = set(), set(), []
    for m in msgs:
        role = m.get("role")
        text = m.get("text") or ""
        if role == "user":
            for s in re.findall(r"@skill:([\w-]+)", text):
                at_skills.add(s)
        for tc in (m.get("tool_calls") or []):
            instr = tc.get("instruction") or {}
            if instr.get("name") == "skill_load":
                try:
                    name = json.loads(instr.get("arguments") or "{}").get("name")
                except Exception:
                    name = None
                if name:
                    loaded.add(name)
        if role in ("synthetic", "internal"):
            for sm in re.finditer(r'Skill\s+"([^"]+)"\s+has been\s+(\w+)', text):
                synth_skill_msgs.append({"skill": sm.group(1), "event": sm.group(2)})
    skills_invoked = sorted(at_skills | loaded)
    out["skills_invoked"] = skills_invoked
    out["resumed_via_skill"] = "session-resume" in skills_invoked
    if synth_skill_msgs:
        out["synthetic_skill_events"] = synth_skill_msgs[:5]
    # Crash = last short synthetic message that looks like an error.
    for s in reversed(synthetics):
        if re.search(r"stream aborted", s, re.IGNORECASE):
            continue
        if len(s) > 200:
            continue
        if re.search(r"\b(error|exceeds|timeout)\b", s, re.IGNORECASE):
            out["crash"] = clip(s, 300)
            break
    return out


def commits_in_window(working_dir, start, end):
    """git log between session start and end (ISO timestamps)."""
    if not (start and end):
        return []
    try:
        r = subprocess.run(
            ["git", "-C", working_dir, "log", "--oneline",
             f"--since={start}", f"--until={end}"],
            capture_output=True, text=True, timeout=10)
        return [l for l in r.stdout.splitlines() if l.strip()]
    except Exception:
        return []


def git_state(working_dir):
    out = {"is_repo": False}
    try:
        r = subprocess.run(["git", "-C", working_dir, "rev-parse", "--is-inside-work-tree"],
                           capture_output=True, text=True, timeout=10)
        if r.returncode != 0:
            return out
        out["is_repo"] = True
        out["branch"] = subprocess.run(["git", "-C", working_dir, "branch", "--show-current"],
                                       capture_output=True, text=True, timeout=10).stdout.strip()
        log = subprocess.run(["git", "-C", working_dir, "log", "--oneline", "-5"],
                             capture_output=True, text=True, timeout=10).stdout.strip()
        status = subprocess.run(["git", "-C", working_dir, "status", "--short"],
                                capture_output=True, text=True, timeout=10).stdout.strip()
        out["last_commits"] = [l for l in log.splitlines() if l.strip()]
        out["dirty_files"] = [l[3:].strip() for l in status.splitlines() if l.strip()]
    except Exception as e:
        out["error"] = str(e)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--working-dir", required=True)
    ap.add_argument("--sessions-dir", default=DEFAULT_SESSIONS)
    ap.add_argument("--limit", type=int, default=6)
    a = ap.parse_args()

    folders = []
    try:
        for name in sorted(os.listdir(a.sessions_dir), reverse=True):
            p = os.path.join(a.sessions_dir, name)
            if not os.path.isdir(p):
                continue
            if not re.match(r'^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}', name):
                continue
            folders.append(name)
            if len(folders) >= a.limit:
                break
    except Exception as e:
        print(json.dumps({"error": f"cannot list sessions dir: {e}"}))
        sys.exit(1)

    sessions = []
    for f in folders:
        s = load_session(f, a.sessions_dir, a.working_dir)
        # Only include sessions that ran in this working dir (or unknown).
        if s["working_dir"] and s["working_dir"] != os.path.abspath(a.working_dir):
            continue
        s["commits_during_session"] = commits_in_window(
            a.working_dir, s["created"], s["updated"])
        sessions.append(s)

    def key(s):
        # LAST MODIFIED, not created: a session touched 2h ago is "more previous"
        # than one created later but abandoned. This defines the walk order.
        ts = parse_ts(s.get("updated")) or parse_ts(s.get("created"))
        return ts or datetime.min.replace(tzinfo=None)
    sessions.sort(key=key, reverse=True)

    report = {
        "working_dir": a.working_dir,
        "generated_at": datetime.now().isoformat(timespec="seconds"),
        "note": "first_user/last_user are POINTERS only. Read <sessions-dir>/<folder>/chat.json yourself to understand each session's story.",
        "sessions": sessions,
        "git": git_state(a.working_dir),
    }
    print(json.dumps(report, indent=1))


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""os-vision — Give the AI eyes on your X11 desktop."""

import argparse
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime


def run(cmd, timeout=10):
    """Run a shell command, return (returncode, stdout, stderr)."""
    try:
        r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
        return r.returncode, r.stdout.strip(), r.stderr.strip()
    except subprocess.TimeoutExpired:
        return -1, "", "timeout"
    except Exception as e:
        return -1, "", str(e)


def check_deps():
    """Verify OS, session type, and required tools."""
    if platform.system() != "Linux":
        return {"ok": False, "error": f"Not Linux (detected: {platform.system()})",
                "hint": "os-vision requires Linux with an X11 session."}

    session = os.environ.get("XDG_SESSION_TYPE", "")
    display = os.environ.get("DISPLAY", "")
    if session == "wayland" or not display:
        return {"ok": False, "error": f"No X11 session (session_type={session or 'unknown'}, DISPLAY={display or 'unset'})",
                "hint": "os-vision requires an X11 session. Try: startx or switch to X11 in your display manager."}

    deps = {}
    install_cmds = {}
    for tool in ["xdotool", "import", "xrandr"]:
        deps[tool] = shutil.which(tool) is not None
        if not deps[tool]:
            install_cmds[tool] = {
                "xdotool": "sudo apt install xdotool",
                "import": "sudo apt install imagemagick",
                "xrandr": "sudo apt install x11-xserver-utils",
            }.get(tool, f"sudo apt install {tool}")

    missing = [t for t, v in deps.items() if not v]
    if missing:
        return {"ok": False, "missing": missing,
                "install": [install_cmds[t] for t in missing]}

    return {"ok": True, "session": "x11", "deps": deps}


def extract_app(title):
    """Extract a short app name from a window title."""
    t = title.strip()
    # Terminal: "user@host: ~/path"
    if re.match(r'^\w+@[\w.]+:\s', t):
        return "Terminal"
    # VS Code: "file - project - Visual Studio Code" or "● file - project — VS Code"
    if 'Visual Studio Code' in t or 'VS Code' in t:
        return "VS Code"
    # Firefox: "Page Title — Mozilla Firefox"
    if 'Mozilla Firefox' in t:
        # Get the page title part
        m = re.match(r'^(.+?)\s*[—-]\s*Mozilla Firefox', t)
        if m:
            return m.group(1).strip()[:40]
        return "Firefox"
    # Chromium: "Page Title - Chromium"
    if 'Chromium' in t:
        m = re.match(r'^(.+?)\s*-\s*Chromium', t)
        if m:
            return m.group(1).strip()[:40]
        return "Chromium"
    # GIMP: "*[Untitled]... – GIMP" or "file.xcf – GIMP"
    if '– GIMP' in t or '- GIMP' in t:
        return "GIMP"
    # gedit: "file (dir) - gedit"
    if t.endswith('- gedit'):
        return "gedit"
    # Generic: "something - AppName"
    m = re.search(r'[-—]\s*(.+)$', t)
    if m:
        candidate = m.group(1).strip()
        # Only use if it looks like an app name (short, no path)
        if len(candidate) < 30 and '/' not in candidate and '~' not in candidate:
            return candidate
    # Fallback: first word
    words = t.split()
    return words[0] if words else t[:20]


# Windows to filter out (system chrome, not user apps)
FILTER_PATTERNS = [
    r'^@!',              # mutter workspace indicators
    r'^mutter guard',    # mutter internal
    r'^gnome-shell',     # shell background
    r'^Firefox$',        # hidden firefox helper
]


def get_windows():
    """List all visible windows with id, app, title, position, size, pid."""
    windows = []
    rc, out, _ = run("xdotool search --onlyvisible --name '' 2>/dev/null")
    if rc != 0:
        return windows

    for line in out.split("\n"):
        line = line.strip()
        if not line:
            continue
        wid = line
        rc, title, _ = run(f"xdotool getwindowname {wid} 2>/dev/null")
        if rc != 0 or not title:
            continue
        # Filter system chrome
        if any(re.search(p, title) for p in FILTER_PATTERNS):
            continue
        rc, geo, _ = run(f"xdotool getwindowgeometry {wid} 2>/dev/null")
        if rc != 0:
            continue
        # Parse: Position: X,Y  Geometry: WxH
        pos_m = re.search(r"Position:\s*(-?\d+),(-?\d+)", geo)
        geo_m = re.search(r"Geometry:\s*(\d+)x(\d+)", geo)
        if not pos_m or not geo_m:
            continue
        x, y = int(pos_m.group(1)), int(pos_m.group(2))
        w, h = int(geo_m.group(1)), int(geo_m.group(2))
        if w <= 1 and h <= 1:
            continue  # skip hidden/1x1 windows
        if x < -50 or y < -50:
            continue  # skip off-screen
        # Get PID
        rc, pid_str, _ = run(f"xdotool getwindowpid {wid} 2>/dev/null")
        pid = int(pid_str) if pid_str.isdigit() else None
        app = extract_app(title)
        windows.append({"id": int(wid), "app": app, "title": title, "x": x, "y": y, "w": w, "h": h, "pid": pid})
    return windows


def cmd_list(args):
    windows = get_windows()
    print(json.dumps({"windows": windows}, indent=2))


def cmd_find(args):
    pattern = args.pattern
    windows = get_windows()
    matches = [w for w in windows if re.search(pattern, w["title"], re.IGNORECASE)]
    print(json.dumps({"pattern": pattern, "matches": matches}, indent=2))


def cmd_screenshot(args):
    tmp_dir = args.output or os.environ.get("SQUID_OS_TMP", tempfile.gettempdir())
    os.makedirs(tmp_dir, exist_ok=True)
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    out_path = os.path.join(tmp_dir, f"os-vision-{ts}.png")

    if args.full:
        rc, out, err = run(f"import -window root {out_path} 2>&1")
    elif args.region:
        # Format: WxH+X+Y
        rc, out, err = run(f"import -window root -crop {args.region} +repage {out_path} 2>&1")
    elif args.id:
        rc, out, err = run(f"import -window {args.id} {out_path} 2>&1")
    elif args.name:
        # Find by name first
        windows = get_windows()
        matches = [w for w in windows if re.search(args.name, w["title"], re.IGNORECASE)]
        if not matches:
            print(json.dumps({"error": f"No window matching '{args.name}'"}))
            return
        if len(matches) > 1:
            print(json.dumps({"error": f"Multiple windows match '{args.name}'", "matches": matches}))
            return
        rc, out, err = run(f"import -window {matches[0]['id']} {out_path} 2>&1")
    else:
        print(json.dumps({"error": "Specify --id, --name, --full, or --region"}))
        return

    if rc != 0 or not os.path.exists(out_path):
        print(json.dumps({"error": f"Screenshot failed: {err or out}"}))
        return

    # Get dimensions
    rc, dims, _ = run(f"identify -format '%wx%h' {out_path} 2>/dev/null")
    size = dims if rc == 0 else "unknown"
    bytes_ = os.path.getsize(out_path)
    print(json.dumps({"path": out_path, "size": size, "bytes": bytes_}, indent=2))


def cmd_info(args):
    wid = args.id
    info = {"id": wid}

    rc, title, _ = run(f"xdotool getwindowname {wid} 2>/dev/null")
    info["title"] = title

    rc, geo, _ = run(f"xdotool getwindowgeometry {wid} 2>/dev/null")
    if rc == 0:
        pos_m = re.search(r"Position:\s*(-?\d+),(-?\d+)", geo)
        geo_m = re.search(r"Geometry:\s*(\d+)x(\d+)", geo)
        if pos_m:
            info["x"], info["y"] = int(pos_m.group(1)), int(pos_m.group(2))
        if geo_m:
            info["w"], info["h"] = int(geo_m.group(1)), int(geo_m.group(2))

    rc, pid_str, _ = run(f"xdotool getwindowpid {wid} 2>/dev/null")
    if pid_str.isdigit():
        info["pid"] = int(pid_str)
        # Get CWD
        cwd_path = f"/proc/{pid_str}/cwd"
        if os.path.exists(cwd_path):
            info["cwd"] = os.readlink(cwd_path)
        # Get process name
        rc, comm, _ = run(f"ps -p {pid_str} -o comm= 2>/dev/null")
        info["process"] = comm
        # Get process tree
        rc, tree, _ = run(f"pstree -p {pid_str} 2>/dev/null | head -15")
        info["tree"] = tree

    print(json.dumps(info, indent=2))


def cmd_monitors(args):
    rc, out, _ = run("xrandr --query 2>/dev/null")
    if rc != 0:
        print(json.dumps({"error": "xrandr failed"}))
        return

    monitors = []
    for line in out.split("\n"):
        m = re.match(r"(\S+)\s+connected\s+(\d+)x(\d+)\+(\d+)\+(\d+)", line)
        if m:
            monitors.append({
                "name": m.group(1),
                "resolution": f"{m.group(2)}x{m.group(3)}",
                "w": int(m.group(2)),
                "h": int(m.group(3)),
                "x": int(m.group(4)),
                "y": int(m.group(5)),
            })
    print(json.dumps({"monitors": monitors}, indent=2))


def main():
    parser = argparse.ArgumentParser(description="os-vision — AI eyes on your X11 desktop")
    sub = parser.add_subparsers(dest="command")

    # check
    sub.add_parser("check", help="Verify deps and session type")

    # list
    sub.add_parser("list", help="List all visible windows")

    # find
    p_find = sub.add_parser("find", help="Find windows by regex")
    p_find.add_argument("pattern", help="Regex pattern to match window titles")

    # screenshot
    p_shot = sub.add_parser("screenshot", help="Take a screenshot")
    p_shot.add_argument("--id", type=int, help="Window ID")
    p_shot.add_argument("--name", help="Window name (regex)")
    p_shot.add_argument("--full", action="store_true", help="Full desktop")
    p_shot.add_argument("--region", help="Region: WxH+X+Y")
    p_shot.add_argument("--output", help="Output directory (default: temp)")

    # info
    p_info = sub.add_parser("info", help="Window info: PID, CWD, process tree")
    p_info.add_argument("--id", type=int, required=True, help="Window ID")

    # monitors
    sub.add_parser("monitors", help="Monitor layout")

    args = parser.parse_args()

    if args.command is None:
        parser.print_help()
        sys.exit(1)

    cmds = {
        "check": cmd_check,
        "list": cmd_list,
        "find": cmd_find,
        "screenshot": cmd_screenshot,
        "info": cmd_info,
        "monitors": cmd_monitors,
    }
    cmds[args.command](args)


def cmd_check(args):
    result = check_deps()
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()

---
name: browser-use
description: "Invoke when the user asks to browse websites, visit URLs, read web content, check feeds, fill forms, click buttons, extract data from sites, or automate any browser task. Supports both visible Chrome control and headless background browsing."
version: 0.1.0
allowed-tools: bash read_file write_file
---

## Overview
Control your browser to browse sites, read content, click elements, fill forms, scroll feeds, and automate web tasks. Works with your existing Chrome — visible (you can watch) or headless (runs in background). First use installs dependencies automatically, subsequent uses are instant.

## Variables
- `<skill-folder>` — directory containing this SKILL.md
- `<config-dir>` — Squid-OS config directory (~/.config/squid-os)
- `<log-dir>` — Squid-OS logs directory (~/.config/squid-os/logs/browser-use)

## Instructions
1. **Check if bootstrapped** — run the bootstrap script. On first use it installs uv, browser-use, and browser-harness. On subsequent uses it just verifies Chrome is running:

```bash
bash <skill-folder>/scripts/bootstrap.sh
```

2. **Determine mode** — ask the user if they want visible or headless browsing if unclear. Default to visible unless they say "in background" or "headless". For headless, set `HEADLESS=1`:

```bash
HEADLESS=1 bash <skill-folder>/scripts/bootstrap.sh
```

3. **Navigate to a site** using `new_tab(url)` (first navigation) or `goto_url(url)` (subsequent):

```bash
BU_CDP_WS="$(cat <config-dir>/browser-use.json | python3 -c 'import sys,json; print(json.load(sys.stdin)["cdp_ws"])')" browser-harness <<'PY'
new_tab("https://example.com")
wait_for_load()
print(page_info())
PY
```

4. **Read page content** — use `js("document.body.innerText")` for full text or targeted `js("...")` for specific elements.

5. **Click elements** — find via accessibility tree then click by coordinates:

```bash
BU_CDP_WS="$(cat <config-dir>/browser-use.json | python3 -c 'import sys,json; print(json.load(sys.stdin)["cdp_ws"])')" browser-harness <<'PY'
nodes = cdp("Accessibility.getFullAXTree")["nodes"]
for n in nodes:
    if n.get("name") == "Submit":
        box = cdp("DOM.getBoxModel", backendNodeId=n["backendDOMNodeId"])["model"]["content"]
        x, y = sum(box[0::2])/4, sum(box[1::2])/4
        click_at_xy(x, y)
        break
PY
```

6. **Scroll** — use `js("window.scrollBy(0, 500)")` then `import time; time.sleep(2)` to wait for lazy-loaded content.

7. **First-time output** — if bootstrap just installed dependencies, tell the user briefly:
> "Browser automation ready. Chrome is running with remote control enabled."

8. **Normal output** — for each task, return a concise summary of what you found or did. No technical details about CDP, heredocs, or Python unless asked.

## Rules
- Always run bootstrap.sh before any browser-harness command.
- For headless mode, prefix with `HEADLESS=1`.
- Use heredoc syntax (`<<'PY'`) for multi-line Python — single quotes prevent shell interpolation.
- Always use `new_tab(url)` for first navigation, `goto_url(url)` for subsequent.
- After navigation always call `wait_for_load()`.
- Never hardcode provider credentials — read from `<config-dir>/endpoints.json` when needed.
- If Chrome isn't available (headless server), suggest Browser Use Cloud: `browser-harness auth login` then `start_remote_daemon("name")`.
- To uninstall: `uv tool uninstall browser-use && uv tool uninstall browser-harness && rm <config-dir>/browser-use.json`.

## Output Format
```
## Browser Task
- URL: [site visited]
- Mode: visible / headless
- Result: [concise summary of what was found or done]
```

## Examples
**First use:**
User: "check my LinkedIn feed"
Agent runs bootstrap (installs deps), then navigates to linkedin.com and reads feed.
Output: "Browser automation ready. Chrome is running with remote control enabled. Here are your top posts: ..."

**Normal use:**
User: "go to reddit and find AI news"
Agent navigates to reddit, scrolls, extracts posts.
Output: Concise summary of top posts found.

**Headless:**
User: "scrape that site in the background"
Agent runs with `HEADLESS=1`, no Chrome window appears.
Output: Results only, no mention of headless unless relevant.

## Resources

### Scripts
- [bootstrap.sh](scripts/bootstrap.sh) — Installs deps, launches Chrome (visible or headless), saves CDP config
- [run-task.sh](scripts/run-task.sh) — Quick task runner with auto-bootstrap check

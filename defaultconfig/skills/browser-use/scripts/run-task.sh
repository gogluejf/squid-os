#!/usr/bin/env bash
# Run a browser task via browser-harness
# Usage: run-task.sh "PY_CODE_HERE" [HEADLESS]

CONFIG_DIR="$HOME/.config/squid-os"
BROWSER_USE_CONFIG="$CONFIG_DIR/browser-use.json"
PATH="$HOME/.local/bin:$PATH"

PY_CODE="$1"
HEADLESS="${2:-0}"

# Check if bootstrap is needed
if ! command -v browser-harness &>/dev/null; then
    echo "NEED_BOOTSTRAP"
    exit 1
fi

# Check if Chrome CDP is running
if [ "$HEADLESS" = "1" ]; then
    # For headless, always re-bootstrap to ensure headless mode
    bash "$CONFIG_DIR/skills/browser-use/scripts/bootstrap.sh" HEADLESS=1 >/dev/null 2>&1
fi

if ! curl -s "http://127.0.0.1:9222/json/version" >/dev/null 2>&1; then
    echo "NEED_BOOTSTRAP"
    exit 1
fi

# Get CDP WS
CDP_WS=$(python3 -c "import json; print(json.load(open('${BROWSER_USE_CONFIG}'))['cdp_ws'])")

# Run the task
BU_CDP_WS="$CDP_WS" timeout 30 browser-harness <<< "$PY_CODE" 2>&1

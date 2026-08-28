#!/usr/bin/env bash
set -e

CONFIG_DIR="$HOME/.config/squid-os"
LOG_DIR="$CONFIG_DIR/logs/browser-use"
BROWSER_USE_CONFIG="$CONFIG_DIR/browser-use.json"

mkdir -p "$LOG_DIR"

log() {
    echo "[browser-use bootstrap] $*" | tee -a "$LOG_DIR/bootstrap.log"
}

HEADLESS="${HEADLESS:-0}"

# Step 1: Install uv if missing
if ! command -v uv &>/dev/null; then
    log "uv not found, installing..."
    if pip3 install uv 2>&1 | tee -a "$LOG_DIR/bootstrap.log"; then
        export PATH="$HOME/.local/bin:$PATH"
        log "uv installed via pip3"
    elif curl -LsSf https://astral.sh/uv/install.sh | sh 2>&1 | tee -a "$LOG_DIR/bootstrap.log"; then
        export PATH="$HOME/.local/bin:$PATH"
        log "uv installed via official installer"
    else
        log "ERROR: Failed to install uv. Try: pip3 install uv"
        exit 1
    fi
else
    log "uv already installed"
fi

# Step 2: Install browser-use + browser-harness if missing
if ! command -v browser-use &>/dev/null; then
    log "browser-use not found, installing..."
    if uv tool install browser-use 2>&1 | tee -a "$LOG_DIR/bootstrap.log"; then
        log "browser-use installed"
    else
        log "ERROR: Failed to install browser-use"
        exit 1
    fi
else
    log "browser-use already installed"
fi

if ! command -v browser-harness &>/dev/null; then
    log "browser-harness not found, installing..."
    if uv tool install browser-harness 2>&1 | tee -a "$LOG_DIR/bootstrap.log"; then
        log "browser-harness installed"
    else
        log "ERROR: Failed to install browser-harness"
        exit 1
    fi
else
    log "browser-harness already installed"
fi

# Step 3: Start Chrome with CDP if not already running
CDP_PORT=9222
if curl -s "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1; then
    log "Chrome already running with CDP on port ${CDP_PORT}"
else
    CHROME_BIN=""
    for bin in chromium chromium-browser google-chrome-stable google-chrome; do
        if command -v "$bin" &>/dev/null; then
            CHROME_BIN="$bin"
            break
        fi
    done

    if [ -z "$CHROME_BIN" ]; then
        log "WARNING: No Chrome/Chromium found. Use Browser Use Cloud instead."
    else
        LAUNCH_ARGS="--remote-debugging-port=${CDP_PORT} --no-first-run --disable-background-timer-throttling"
        if [ "$HEADLESS" = "1" ]; then
            LAUNCH_ARGS="${LAUNCH_ARGS} --headless=new --disable-gpu --no-sandbox"
            log "Launching ${CHROME_BIN} in HEADLESS mode..."
        else
            log "Launching ${CHROME_BIN} in visible mode..."
        fi
        setsid "$CHROME_BIN" $LAUNCH_ARGS >/dev/null 2>&1 &
        disown
        sleep 3
        if curl -s "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1; then
            log "Chrome launched successfully"
        else
            log "WARNING: Chrome failed to start with CDP"
        fi
    fi
fi

# Step 4: Get CDP WebSocket URL and save config
CDP_WS=""
if curl -s "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1; then
    CDP_WS=$(curl -s "http://127.0.0.1:${CDP_PORT}/json/version" | python3 -c "import sys,json; print(json.load(sys.stdin)['webSocketDebuggerUrl'])")
    log "CDP WebSocket URL: ${CDP_WS}"

    python3 -c "
import json
config = {'cdp_ws': '${CDP_WS}', 'cdp_port': ${CDP_PORT}, 'headless': ${HEADLESS}}
with open('${BROWSER_USE_CONFIG}', 'w') as f:
    json.dump(config, f, indent=2)
"
    log "Config saved"
else
    log "No local Chrome available."
fi

log "Bootstrap complete."

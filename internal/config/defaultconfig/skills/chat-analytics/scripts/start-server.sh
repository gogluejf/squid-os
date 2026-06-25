#!/bin/bash
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PIDFILE="/tmp/chat-analytics.pid"
LOGFILE="/tmp/chat-analytics.log"

cd "$SKILL_DIR"

# ensure symlink exists
ln -sfn ~/.config/squid-os/sessions "$SKILL_DIR/sessions"

# stop old tracked pid if it exists
if [ -f "$PIDFILE" ]; then
  OLD_PID="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [ -n "${OLD_PID:-}" ] && kill -0 "$OLD_PID" 2>/dev/null; then
    kill "$OLD_PID" 2>/dev/null || true
    sleep 1
  fi
  rm -f "$PIDFILE"
fi

# fallback cleanup
pkill -f "server.py" 2>/dev/null || true
sleep 1

python3 scripts/server.py > "$LOGFILE" 2>&1 &
SERVER_PID=$!
echo "$SERVER_PID" > "$PIDFILE"

echo "started pid=$SERVER_PID port=17771 log=$LOGFILE"

#!/bin/bash
set -euo pipefail

PIDFILE="/tmp/chat-analytics.pid"

if [ -f "$PIDFILE" ]; then
  PID="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [ -n "${PID:-}" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    sleep 1
    if kill -0 "$PID" 2>/dev/null; then
      kill -9 "$PID" 2>/dev/null || true
    fi
    echo "stopped pid=$PID"
  fi
  rm -f "$PIDFILE"
fi

pkill -f "server.py" 2>/dev/null || true

#!/bin/bash
set -euo pipefail

PIDFILE="/tmp/chat-analytics.pid"
URL="http://localhost:17771/api/sessions"

if [ -f "$PIDFILE" ]; then
  PID="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [ -n "${PID:-}" ] && kill -0 "$PID" 2>/dev/null; then
    if curl -fsS "$URL" >/dev/null 2>&1; then
      echo "running pid=$PID url=http://localhost:17771"
      exit 0
    fi
  fi
fi

echo "stopped"
exit 1

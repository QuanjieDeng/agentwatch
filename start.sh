#!/usr/bin/env bash
set -euo pipefail

tool_dir="$(cd "$(dirname "$0")" && pwd)"
port="${AGENTWATCH_PORT:-7788}"

cd "$tool_dir"
( sleep 1; xdg-open "http://localhost:$port" >/dev/null 2>&1 || true ) &
exec go run . -port "$port"

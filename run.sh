#!/usr/bin/env bash
# Launches the ML service and the Go backend together for local dev.
# Requires: go >= 1.22, python3 >= 3.10
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"

# --- ML service ---
cd "$ROOT/ml"
if [ ! -d ".venv" ]; then
  python3 -m venv .venv
fi
# shellcheck disable=SC1091
source .venv/bin/activate
pip install -q -r requirements.txt

uvicorn app:app --host 0.0.0.0 --port 8000 &
ML_PID=$!
echo "ml service started (pid=$ML_PID)"

cleanup() {
  echo "shutting down…"
  kill "$ML_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# --- Go backend ---
cd "$ROOT/backend"
ADDR=":8080" ML_URL="http://localhost:8000" WEB_DIR="$ROOT/backend/web" \
  go run .

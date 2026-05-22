#!/usr/bin/env bash
# Launches ML (:8000) and Go backend (:8080) together.
# Requires: go >= 1.22, python3 >= 3.10
# Linux/macOS/Git Bash on Windows. On Windows use: source .venv/Scripts/activate
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go is not installed or not in PATH."
  echo "Install: https://go.dev/dl/  (then restart the terminal)"
  exit 1
fi

# --- ML service ---
cd "$ROOT/ml"
if [ ! -d ".venv" ]; then
  python3 -m venv .venv 2>/dev/null || python -m venv .venv
fi

if [ -f ".venv/Scripts/activate" ]; then
  # Windows (Git Bash)
  # shellcheck disable=SC1091
  source .venv/Scripts/activate
elif [ -f ".venv/bin/activate" ]; then
  # Linux / macOS
  # shellcheck disable=SC1091
  source .venv/bin/activate
else
  echo "ERROR: venv not found in ml/.venv"
  exit 1
fi

pip install -q -r requirements.txt

uvicorn app:app --host 127.0.0.1 --port 8000 &
ML_PID=$!
echo "ml service started (pid=$ML_PID) -> http://127.0.0.1:8000/health"

cleanup() {
  echo "shutting down…"
  kill "$ML_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# --- Go backend (foreground; blocks until Ctrl+C) ---
cd "$ROOT/backend"
export ADDR=":8080"
export ML_URL="http://127.0.0.1:8000"
export WEB_DIR="$ROOT/backend/web"
echo "backend starting -> http://127.0.0.1:8080"
go run .

#!/usr/bin/env bash
# Launches ML (:8000) and Go backend (:8080) together.
# Requires: go >= 1.22, python >= 3.10
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
ML_DIR="$ROOT/ml"
VENV_DIR="$ML_DIR/.venv"

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go is not installed or not in PATH."
  echo "Install: https://go.dev/dl/  (then restart the terminal)"
  exit 1
fi

choose_python() {
  if [ -n "${PYTHON_BIN:-}" ]; then
    if command -v "$PYTHON_BIN" >/dev/null 2>&1; then
      command -v "$PYTHON_BIN"
      return 0
    fi
    echo "ERROR: PYTHON_BIN=$PYTHON_BIN not found."
    exit 1
  fi

  for candidate in python3.12 python3 python; do
    if command -v "$candidate" >/dev/null 2>&1; then
      command -v "$candidate"
      return 0
    fi
  done

  echo "ERROR: Python not found. Install Python >= 3.10 or run with: PYTHON_BIN=/path/to/python ./run.sh"
  exit 1
}

# --- ML service ---
if [ ! -d "$VENV_DIR" ]; then
  PYTHON_BIN="$(choose_python)"
  "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

if [ -f "$VENV_DIR/Scripts/activate" ]; then
  # Windows (Git Bash)
  # shellcheck disable=SC1091
  source "$VENV_DIR/Scripts/activate"
elif [ -f "$VENV_DIR/bin/activate" ]; then
  # Linux / macOS
  # shellcheck disable=SC1091
  source "$VENV_DIR/bin/activate"
else
  echo "ERROR: venv not found in $VENV_DIR"
  exit 1
fi

python -m pip install -q -r "$ML_DIR/requirements.txt"

cd "$ML_DIR"
uvicorn app:app --host 127.0.0.1 --port 8000 &
ML_PID=$!
echo "ml service started (pid=$ML_PID) -> http://127.0.0.1:8000/health"

cleanup() {
  echo "shutting down..."
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

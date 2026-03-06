#!/bin/bash
# =============================================================================
# Smoke / integration test for runanywhere-server
#
# 1. Smoke: run binary --help (no model needed).
# 2. Integration: if MODEL_PATH is set or passed, start server, GET /health,
#    assert status and v3 fields (stt_available, tts_available, embeddings_available), stop.
#
# Usage:
#   ./scripts/smoke-test-server.sh                    # smoke only (--help)
#   ./scripts/smoke-test-server.sh /path/to/model.gguf   # smoke + integration
#   MODEL_PATH=/path/to/model.gguf ./scripts/smoke-test-server.sh
#
# Prerequisites:
#   - Server built: ./scripts/build-server.sh  (binary at build-server/tools/runanywhere-server)
#   - Integration: a GGUF model at MODEL_PATH (e.g. from tests/scripts/download-test-models.sh)
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="${PROJECT_DIR}/build-server"
BIN="${RAC_SERVER_BIN:-${BUILD_DIR}/tools/runanywhere-server}"
PORT="${RAC_SERVER_TEST_PORT:-0}"   # 0 = pick a free port

if [[ ! -x "$BIN" ]]; then
    echo "Server binary not found or not executable: $BIN"
    echo "Build with: $SCRIPT_DIR/build-server.sh"
    exit 1
fi

# --- Smoke: --help
echo "Smoke: $BIN --help"
"$BIN" --help | head -5
echo ""

MODEL_PATH="${1:-${MODEL_PATH:-}}"
if [[ -z "$MODEL_PATH" ]]; then
    echo "No MODEL_PATH; skipping integration (start server + GET /health)."
    echo "To run integration: $0 /path/to/model.gguf  or  MODEL_PATH=/path/to/model.gguf $0"
    exit 0
fi

if [[ ! -f "$MODEL_PATH" ]]; then
    echo "Model file not found: $MODEL_PATH"
    exit 1
fi

# Pick a free port if not set
if [[ "$PORT" == "0" ]]; then
    if command -v python3 &>/dev/null; then
        PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()')
    else
        PORT=$(( 18080 + (RANDOM % 1000) ))
    fi
fi
BASE_URL="http://127.0.0.1:${PORT}"

# Start server in background
echo "Integration: starting server on port $PORT with model $MODEL_PATH"
"$BIN" --model "$MODEL_PATH" --port "$PORT" --host 127.0.0.1 &
PID=$!
cleanup() {
    if ! kill -0 "$PID" 2>/dev/null; then
        return
    fi

    kill "$PID" 2>/dev/null || true

    # Avoid indefinite hangs if the server ignores SIGTERM during shutdown.
    for _ in {1..20}; do
        if ! kill -0 "$PID" 2>/dev/null; then
            break
        fi
        sleep 0.25
    done

    if kill -0 "$PID" 2>/dev/null; then
        echo "Server did not exit after SIGTERM; forcing SIGKILL"
        kill -9 "$PID" 2>/dev/null || true
    fi

    wait "$PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for server to be ready (health returns 200)
echo "Waiting for server..."
for i in {1..60}; do
    if curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/health" 2>/dev/null | grep -q 200; then
        break
    fi
    sleep 0.5
    if ! kill -0 "$PID" 2>/dev/null; then
        echo "Server process exited early"
        exit 1
    fi
done

HEALTH=$(curl -s "$BASE_URL/health")
if [[ -z "$HEALTH" ]]; then
    echo "GET /health returned empty"
    exit 1
fi

echo "GET /health response: $HEALTH"
STATUS=$(echo "$HEALTH" | sed -n 's/.*"status"\s*:\s*"\([^"]*\)".*/\1/p')
if [[ "$STATUS" != "ok" ]]; then
    echo "Expected status=ok, got status=$STATUS"
    exit 1
fi

# v3: capability fields present (optional; server may omit if not built with v2 backends)
for field in stt_available tts_available embeddings_available; do
    if echo "$HEALTH" | grep -q "\"$field\""; then
        echo "  $field present"
    fi
done

echo "Smoke + integration passed."

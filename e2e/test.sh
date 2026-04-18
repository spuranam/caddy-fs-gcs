#!/usr/bin/env bash
set -euo pipefail

GCS_PORT="${GCS_PORT:-4443}"
CADDY_PORT="${CADDY_PORT:-8080}"
BUCKET="${BUCKET:-e2e-docsy-bucket}"
CADDY_BIN="${CADDY_BIN:-dist/caddy-fs-gcs}"
BASE_URL="http://localhost:${CADDY_PORT}"

# Verify dependencies
for cmd in fake-gcs-server curl "$CADDY_BIN"; do
  if ! command -v "$cmd" >/dev/null 2>&1 && [ ! -x "$cmd" ]; then
    echo "❌ Required command not found: $cmd"
    exit 1
  fi
done

# Kill stale processes on our ports
for port in "$GCS_PORT" "$CADDY_PORT"; do
  pids=$(lsof -ti :"$port" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    echo "   Killing stale processes on port $port"
    echo "$pids" | xargs kill 2>/dev/null || true
    sleep 2
    echo "$pids" | xargs kill -9 2>/dev/null || true
    sleep 1
  fi
done

# --- Start fake-gcs-server ---
rm -rf /tmp/fake-gcs-storage
mkdir -p /tmp/fake-gcs-storage
fake-gcs-server \
  -scheme http \
  -port "$GCS_PORT" \
  -data "e2e/gcs-root" \
  -filesystem-root /tmp/fake-gcs-storage \
  -backend filesystem \
  -public-host "localhost:${GCS_PORT}" > /tmp/fake-gcs-server.log 2>&1 &
GCS_PID=$!
CADDY_PID=""

cleanup() {
  echo ""
  echo "🛑 Shutting down..."
  [ -n "$CADDY_PID" ] && kill "$CADDY_PID" 2>/dev/null || true
  kill "$GCS_PID" 2>/dev/null || true
  [ -n "$CADDY_PID" ] && wait "$CADDY_PID" 2>/dev/null || true
  wait "$GCS_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for GCS emulator
echo "   Waiting for GCS emulator..."
for _ in $(seq 1 15); do
  if curl -s -o /dev/null "http://localhost:${GCS_PORT}/storage/v1/b" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$GCS_PID" 2>/dev/null; then
    echo "❌ fake-gcs-server crashed:"
    cat /tmp/fake-gcs-server.log
    exit 1
  fi
  sleep 1
done

if ! curl -s -o /dev/null "http://localhost:${GCS_PORT}/storage/v1/b" 2>/dev/null; then
  echo "❌ fake-gcs-server not responding:"
  cat /tmp/fake-gcs-server.log
  exit 1
fi
echo "✅ GCS emulator ready"

# --- Start Caddy ---
# The Go GCS SDK checks this variable automatically — when set, it redirects all GCS API calls to that local endpoint instead of storage.googleapis.com.
export STORAGE_EMULATOR_HOST="localhost:${GCS_PORT}"
"$CADDY_BIN" run --config e2e/Caddyfile.e2e --adapter caddyfile > /tmp/caddy-e2e.log 2>&1 &
CADDY_PID=$!

echo "   Waiting for Caddy..."
for _ in $(seq 1 10); do
  if curl -s -o /dev/null "${BASE_URL}/health" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$CADDY_PID" 2>/dev/null; then
    echo "❌ Caddy crashed:"
    cat /tmp/caddy-e2e.log
    exit 1
  fi
  sleep 1
done

if ! curl -s -o /dev/null "${BASE_URL}/health" 2>/dev/null; then
  echo "❌ Caddy not responding:"
  cat /tmp/caddy-e2e.log
  exit 1
fi
echo "✅ Caddy ready"
echo ""

# --- Run smoke tests ---
export BASE_URL
bash e2e/smoke.sh

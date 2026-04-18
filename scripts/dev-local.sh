#!/usr/bin/env bash
set -euo pipefail

GCS_PORT="${GCS_PORT:-4443}"
CADDY_PORT="${CADDY_PORT:-8080}"
DATA_DIR="${DATA_DIR:-test-data/static}"
CADDY_BIN="${CADDY_BIN:-dist/caddy}"
CADDY_CONFIG="${CADDY_CONFIG:-Caddyfile.dev}"

# Verify dependencies
for cmd in fake-gcs-server curl "$CADDY_BIN"; do
  if ! command -v "$cmd" >/dev/null 2>&1 && [ ! -x "$cmd" ]; then
    echo "❌ Required command not found: $cmd"
    exit 1
  fi
done

echo "🚀 Starting local development environment"
echo "   GCS emulator:  http://localhost:${GCS_PORT}"
echo "   Caddy server:  http://localhost:${CADDY_PORT}"
echo "   Data dir:      ${DATA_DIR}"
echo ""

# Kill stale processes on our ports
for port in "$GCS_PORT" "$CADDY_PORT"; do
  pids=$(lsof -ti :"$port" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    echo "   Killing stale processes on port $port: $pids"
    echo "$pids" | xargs kill -9 2>/dev/null || true
    sleep 1
  fi
done

# Start fake-gcs-server
rm -rf /tmp/fake-gcs-storage-dev
mkdir -p /tmp/fake-gcs-storage-dev
fake-gcs-server \
  -scheme http \
  -port "$GCS_PORT" \
  -data "$DATA_DIR" \
  -filesystem-root /tmp/fake-gcs-storage-dev \
  -backend filesystem \
  -public-host "localhost:${GCS_PORT}" > /tmp/fake-gcs-server-dev.log 2>&1 &
GCS_PID=$!

cleanup() {
  echo ""
  echo "🛑 Shutting down..."
  kill "$GCS_PID" 2>/dev/null || true
  wait "$GCS_PID" 2>/dev/null || true
  echo "✅ Stopped"
}
trap cleanup EXIT

# Wait for GCS emulator to be ready
echo "   Waiting for GCS emulator..."
for i in $(seq 1 15); do
  if curl -s -o /dev/null "http://localhost:${GCS_PORT}/storage/v1/b" 2>/dev/null; then
    echo "✅ GCS emulator running (PID $GCS_PID)"
    break
  fi
  if ! kill -0 "$GCS_PID" 2>/dev/null; then
    echo "❌ fake-gcs-server crashed:"
    cat /tmp/fake-gcs-server-dev.log
    exit 1
  fi
  sleep 1
done

if ! curl -s -o /dev/null "http://localhost:${GCS_PORT}/storage/v1/b" 2>/dev/null; then
  echo "❌ fake-gcs-server not responding after 15s:"
  cat /tmp/fake-gcs-server-dev.log
  exit 1
fi

# Start Caddy
export STORAGE_EMULATOR_HOST="localhost:${GCS_PORT}"
echo ""
echo "🌐 Starting Caddy — open http://localhost:${CADDY_PORT}/"
echo "   Press Ctrl+C to stop"
echo ""
exec "$CADDY_BIN" run --config "$CADDY_CONFIG" --adapter caddyfile

#!/usr/bin/env bash
set -euo pipefail

GCS_PORT="${GCS_PORT:-4443}"
CADDY_PORT="${CADDY_PORT:-8080}"
BUCKET="${BUCKET:-e2e-docsy-bucket}"
CADDY_BIN="${CADDY_BIN:-dist/caddy}"

# Verify dependencies
for cmd in fake-gcs-server curl "$CADDY_BIN"; do
  if ! command -v "$cmd" >/dev/null 2>&1 && [ ! -x "$cmd" ]; then
    echo "❌ Required command not found: $cmd"
    exit 1
  fi
done

echo "🚀 Starting e2e environment"
echo "   GCS emulator:  http://localhost:${GCS_PORT}"
echo "   Caddy server:  http://localhost:${CADDY_PORT}"
echo "   Bucket:        ${BUCKET}"
echo ""

# Kill stale processes on our ports
for port in "$GCS_PORT" "$CADDY_PORT"; do
  pids=$(lsof -ti :"$port" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    echo "   Killing stale processes on port $port: $pids"
    echo "$pids" | xargs kill 2>/dev/null || true
    sleep 2
    echo "$pids" | xargs kill -9 2>/dev/null || true
    sleep 1
  fi
done

# Start fake-gcs-server
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
for _ in $(seq 1 15); do
  if curl -s -o /dev/null "http://localhost:${GCS_PORT}/storage/v1/b" 2>/dev/null; then
    echo "✅ GCS emulator running (PID $GCS_PID)"
    break
  fi
  if ! kill -0 "$GCS_PID" 2>/dev/null; then
    echo "❌ fake-gcs-server crashed:"
    cat /tmp/fake-gcs-server.log
    exit 1
  fi
  sleep 1
done

# Verify it's actually ready
if ! curl -s -o /dev/null "http://localhost:${GCS_PORT}/storage/v1/b" 2>/dev/null; then
  echo "❌ fake-gcs-server not responding after 15s:"
  cat /tmp/fake-gcs-server.log
  exit 1
fi

# Start Caddy
# The Go GCS SDK checks this variable automatically — when set, it redirects all GCS API calls to that local endpoint instead of storage.googleapis.com.
export STORAGE_EMULATOR_HOST="localhost:${GCS_PORT}"
echo ""
echo "🌐 Starting Caddy — open http://localhost:${CADDY_PORT}/"
echo "   Press Ctrl+C to stop"
echo ""
exec "$CADDY_BIN" run --config e2e/Caddyfile.e2e --adapter caddyfile

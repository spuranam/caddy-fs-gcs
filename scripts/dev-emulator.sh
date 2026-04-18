#!/usr/bin/env bash
set -euo pipefail

GCS_PORT="${GCS_PORT:-4443}"
DATA_DIR="${DATA_DIR:-test-data/static}"

if ! command -v fake-gcs-server >/dev/null 2>&1; then
  echo "❌ fake-gcs-server not found."
  echo "   Install: go install github.com/fsouza/fake-gcs-server@latest"
  exit 1
fi

echo "🚀 Starting GCS emulator on http://localhost:${GCS_PORT}"
echo "   Data dir: ${DATA_DIR}"
echo ""
echo "To use with Caddy, set:"
echo "  export STORAGE_EMULATOR_HOST=localhost:${GCS_PORT}"
echo ""

rm -rf /tmp/fake-gcs-storage-emulator
mkdir -p /tmp/fake-gcs-storage-emulator
exec fake-gcs-server \
  -scheme http \
  -port "$GCS_PORT" \
  -data "$DATA_DIR" \
  -filesystem-root /tmp/fake-gcs-storage-emulator \
  -backend filesystem \
  -public-host "localhost:${GCS_PORT}"

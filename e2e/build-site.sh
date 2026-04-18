#!/usr/bin/env bash
set -euo pipefail

BUCKET="${BUCKET:-e2e-docsy-bucket}"
BASE_URL="${BASE_URL:-http://localhost:8080/}"

if ! command -v hugo >/dev/null 2>&1; then
  echo "❌ hugo not found."
  echo "   Install: brew install hugo"
  exit 1
fi

for cmd in git npm; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "❌ $cmd not found."
    exit 1
  fi
done

# Clone docsy-example if not present
if [ ! -d "e2e/docsy-site" ]; then
  echo "📦 Cloning google/docsy-example..."
  git clone --depth 1 https://github.com/google/docsy-example.git e2e/docsy-site
  rm -rf e2e/docsy-site/.git
fi

echo "🏗️  Building Docsy site..."

# Install npm deps if needed
if [ ! -d "e2e/docsy-site/node_modules" ]; then
  echo "   Installing npm dependencies..."
  (cd e2e/docsy-site && npm install)
fi

# Build Hugo site
(cd e2e/docsy-site && hugo --minify --baseURL "$BASE_URL" -d ../site-content)

# Prepare gcs-root/<bucket>/ for fake-gcs-server filesystem backend
rm -rf e2e/gcs-root
mkdir -p "e2e/gcs-root/${BUCKET}"
cp -R e2e/site-content/* "e2e/gcs-root/${BUCKET}/"

FILE_COUNT=$(find e2e/site-content -type f | wc -l | tr -d ' ')
SIZE=$(du -sh e2e/site-content | awk '{print $1}')
echo "✅ Site built: ${FILE_COUNT} files, ${SIZE}"

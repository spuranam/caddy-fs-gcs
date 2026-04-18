#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "🧪 Running e2e smoke tests against ${BASE_URL}"
echo ""

# Check server is reachable before running tests
if ! curl -s -o /dev/null --max-time 3 "${BASE_URL}/health" 2>/dev/null; then
  echo "❌ Server not reachable at ${BASE_URL}"
  echo "   Start it first: task e2e:serve"
  exit 1
fi

PASS=0
FAIL=0
TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

check() {
  local desc="$1"
  local url="$2"
  local expect_status="${3:-200}"
  local expect_content="${4:-}"

  local status
  status=$(curl -s -o "$TMPFILE" -w '%{http_code}' "$url" 2>/dev/null)
  local body
  body=$(cat "$TMPFILE" 2>/dev/null)

  if [ "$status" != "$expect_status" ]; then
    echo "  ❌ $desc — expected $expect_status, got $status"
    FAIL=$((FAIL + 1))
    return
  fi

  if [ -n "$expect_content" ] && ! echo "$body" | grep -q "$expect_content"; then
    echo "  ❌ $desc — missing expected content: $expect_content"
    FAIL=$((FAIL + 1))
    return
  fi

  echo "  ✅ $desc ($status)"
  PASS=$((PASS + 1))
}

check_header() {
  local desc="$1"
  local url="$2"
  local header="$3"
  local expect_value="$4"

  local value
  value=$(curl -s -I "$url" 2>/dev/null | grep -i "^${header}:" | head -1 | sed "s/^${header}: *//i" | tr -d '\r')

  if echo "$value" | grep -qi "$expect_value"; then
    echo "  ✅ $desc (${header}: ${value})"
    PASS=$((PASS + 1))
  else
    echo "  ❌ $desc — expected ${header} containing '$expect_value', got '${value}'"
    FAIL=$((FAIL + 1))
  fi
}

# --- Health checks ---
echo "📋 Health checks"
check "Health endpoint" "${BASE_URL}/health" 200 "healthy"
check "Ready endpoint"  "${BASE_URL}/ready"  200 "ready"

# --- HTML pages ---
echo ""
echo "📋 HTML pages"
check "Homepage"        "${BASE_URL}/"            200 "Goldydocs"
check "Docs section"    "${BASE_URL}/docs/"        200 "Documentation"
check "Blog section"    "${BASE_URL}/blog/"        200 ""
check "About page"      "${BASE_URL}/about/"       200 ""
check "Community page"  "${BASE_URL}/community/"   200 ""

# --- Static assets ---
echo ""
echo "📋 Static assets"
check "CSS (prism)"     "${BASE_URL}/css/prism.css"  200 ""
check "Sitemap"         "${BASE_URL}/sitemap.xml"    200 "sitemap"
check "robots.txt"      "${BASE_URL}/robots.txt"     200 "User-agent"

# --- Response headers ---
echo ""
echo "📋 Response headers"
check_header "X-Content-Type-Options" "${BASE_URL}/" "X-Content-Type-Options" "nosniff"
check_header "X-Frame-Options"        "${BASE_URL}/" "X-Frame-Options"        "DENY"
check_header "Cache-Control"          "${BASE_URL}/" "Cache-Control"          "public"
check_header "Content-Type HTML"      "${BASE_URL}/" "Content-Type"           "text/html"
check_header "X-Served-By"           "${BASE_URL}/" "X-Served-By"            "caddy-fs-gcs-e2e"

# --- Compression ---
echo ""
echo "📋 Compression"
COMPRESSED_SIZE=$(curl -s -H "Accept-Encoding: gzip" -o /dev/null -w '%{size_download}' "${BASE_URL}/")
UNCOMPRESSED_SIZE=$(curl -s -o /dev/null -w '%{size_download}' "${BASE_URL}/")
if [ "$COMPRESSED_SIZE" -lt "$UNCOMPRESSED_SIZE" ] 2>/dev/null; then
  echo "  ✅ Gzip compression working (${COMPRESSED_SIZE} < ${UNCOMPRESSED_SIZE} bytes)"
  PASS=$((PASS + 1))
else
  echo "  ⚠️  Compression may not be active (compressed=${COMPRESSED_SIZE}, raw=${UNCOMPRESSED_SIZE})"
fi

# --- Range requests ---
echo ""
echo "📋 Range requests"
RANGE_STATUS=$(curl -s -o /dev/null -w '%{http_code}' -r 0-99 "${BASE_URL}/robots.txt" 2>/dev/null || true)
if [ "$RANGE_STATUS" = "206" ]; then
  echo "  ✅ Range request returned 206 Partial Content"
  PASS=$((PASS + 1))
else
  echo "  ⚠️  Range request returned ${RANGE_STATUS:-error} (may depend on streaming config)"
fi

# --- Results ---
echo ""
echo "=========================================="
echo "Results: ${PASS} passed, ${FAIL} failed"
echo "=========================================="

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi

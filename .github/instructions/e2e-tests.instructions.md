---
description: "E2E and integration test conventions for caddy-fs-gcs. Uses fake-gcs-server for GCS emulation. Use when editing e2e tests or smoke tests."
applyTo: "e2e/**"
---

# E2E Test Conventions

## Running E2E Tests

```bash
task e2e:test    # Build, start servers, run smoke tests, shut down
task e2e:serve   # Start servers only (for manual testing)
task e2e:smoke   # Run smoke tests against running server
```

## Architecture

- **fake-gcs-server**: Local GCS emulator (no credentials needed)
- **Caddyfile.e2e**: E2E-specific Caddy configuration
- **smoke.sh**: Bash-based smoke tests using curl
- **Hugo Docsy site**: Realistic static site for testing

## Smoke Test Patterns

Use the `check` function for status + content assertions:

```bash
check "Description" "${BASE_URL}/path" 200 "expected_content"
```

Use `check_header` for response header assertions:

```bash
check_header "Description" "${BASE_URL}/path" "Header-Name" "expected_value"
```

## Health Endpoint Responses

Health endpoints return JSON (not plain text). Assert on JSON field values:

- `/health` returns `{"status":"healthy",...}`
- `/ready` returns `{"readiness":"ready",...}`

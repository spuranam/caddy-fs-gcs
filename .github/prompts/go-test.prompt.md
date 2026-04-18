---
description: "caddy-fs-gcs: Run Go tests with race detection, check coverage, and diagnose failures."
agent: "go-reviewer"
---
Run the Go test suite and report results:

1. Run `go test -race -count=1 ./...` -- report any failures
2. Run `task coverage:check` -- check coverage meets 85% threshold
3. Run `task coverage:report` -- report per-package coverage
4. If failures exist, diagnose root cause and suggest fixes
5. Identify packages with coverage below 85%

Focus on recently changed packages first. Use `git diff --name-only -- '*.go'` to identify them.

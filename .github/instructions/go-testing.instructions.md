---
description: "Go testing conventions for caddy-fs-gcs: table-driven tests, testify/assert, benchmarks, race detection, and coverage. Use when writing or editing Go test files."
applyTo: "**/*_test.go"
---

# Go Testing Conventions

## Framework

- Use standard `go test` with **table-driven tests**
- Use `testify/assert` for assertions
- Place mocks in the same package or `mock_test.go` files
- Use `t.Parallel()` for independent tests

## Race Detection

Always run with the `-race` flag during development:

```bash
go test -race ./...
```

## Coverage

```bash
task coverage:check
```

### Coverage Targets

| Code Type | Target |
| --------- | -------- |
| All packages (`pkg/...`) | 85%+ total |
| Critical paths (fs, cache, health) | 90%+ |
| Generated code / `cmd/` | Excluded |

### Patch Coverage

Every PR should maintain 85%+ total coverage per `.testcoverage.yml`.

- When adding new code, write tests for it in the same PR
- Never submit a new file with 0% coverage; at minimum test the happy path and one error path
- If a function is hard to test, extract the core logic into a helper and test that

## Benchmarks

Add benchmark tests for performance-sensitive code:

```go
func BenchmarkMyFeature(b *testing.B) {
    b.ReportAllocs()
    b.ResetTimer()

    for b.Loop() {
        // benchmark code
    }
}
```

## Caddy-Specific Testing

- Use `caddy.NewTestContext()` for provisioning tests when a Caddy context is needed
- Use `caddyfile.Tokenize` + `caddyfile.NewDispenser` for Caddyfile parsing tests
- Mock GCS clients with interface-based mocks, not real GCS connections

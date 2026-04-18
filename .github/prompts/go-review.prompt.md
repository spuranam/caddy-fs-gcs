---
description: "caddy-fs-gcs: Run Go code review on recent changes. Checks for idiomatic Go, security, error handling, concurrency, and project conventions."
agent: "go-reviewer"
---
Review the current Go code changes thoroughly. You MUST complete ALL phases below. Do not stop after finding a few issues.

## Phase 1: Automated checks

1. Run `go vet ./...` and `task lint`
2. Run `git diff --stat HEAD -- '*.go'` and `git status --short` to identify all changed/new files
3. Read the full diff for all changed files
4. Read the full contents of all new (untracked) files
5. Run `go test -coverprofile` on **every** changed package
6. Run `go test -race` on changed packages

## Phase 2: Systematic review (check EVERY item)

For each changed/new file, check ALL of these categories. Do not skip any.

### Security

- [ ] Path traversal (user-controlled paths not validated for containment)
- [ ] HTML injection (unescaped user input in error pages)
- [ ] Hardcoded secrets, tokens, or credentials
- [ ] Unsafe deserialization of untrusted input

### Error handling

- [ ] Ignored errors (unchecked error returns, `_ = someFunc()`)
- [ ] Missing error wrapping (`fmt.Errorf("context: %w", err)`)
- [ ] Panics used for recoverable errors
- [ ] Error messages that leak sensitive information

### Concurrency

- [ ] Goroutine leaks (goroutines that never exit)
- [ ] Race conditions (shared state without synchronization)
- [ ] Deadlock potential (inconsistent lock ordering)

### Code quality

- [ ] Functions over 60 lines (flag, suggest extraction)
- [ ] Nesting depth over 4 levels
- [ ] Non-idiomatic Go patterns

### caddy-fs-gcs conventions

- [ ] Caddy module interface guards present
- [ ] Logging uses `zap.Logger` from `ctx.Logger()` (never `fmt.Printf`)
- [ ] Error pages escape user input via `html.EscapeString()`
- [ ] GCS clients use `caddy.UsagePool`
- [ ] No magic values (use constants)
- [ ] Metrics use normalized route patterns

### Correctness

- [ ] Edge cases: nil inputs, empty slices, zero values handled
- [ ] Default values match documentation
- [ ] `defer cancel()` placed immediately after context creation

### Dead code

- [ ] New exported functions have callers outside test files
- [ ] No orphaned imports after refactoring

### Observability

- [ ] Metric labels use bounded cardinality (route patterns, not raw paths)

## Phase 3: Adversarial analysis

For each new feature or behavioral change, actively try to break it:

- What happens with nil/empty/zero inputs?
- What happens if GCS returns an error?
- What happens under concurrent access?
- Can this change cause a regression in existing behavior?

## Phase 4: Cross-file consistency

- [ ] Changes to types/interfaces are reflected in all implementations
- [ ] Changes to function signatures are reflected in all call sites
- [ ] Caddyfile parsing changes are reflected in tests

## Phase 5: Coverage analysis

1. Run `task coverage:check` for overall coverage
2. For **every** changed file, flag changed functions with coverage below 85%
3. For each gap, recommend specific test cases

## Phase 6: Self-review (MANDATORY)

After completing phases 1-5:

1. Re-read the full diff one more time
2. For each file you reviewed, ask: "What did I NOT check?"
3. Look for patterns: if you found one bug, are there related bugs?

## Output format

Use severity levels: CRITICAL > HIGH > MEDIUM > LOW > INFO
For each finding include: file, line, severity, description, and suggested fix.
End with a summary table: files reviewed, findings by severity, coverage status.

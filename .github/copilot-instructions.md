# caddy-fs-gcs - AI Agent Instructions

## Overview

Caddy v2 plugin that serves static files from Google Cloud Storage
buckets. Implements `fs.FS`/`fs.StatFS` backed by GCS, with attribute
caching, health checks, Prometheus metrics, OpenTelemetry tracing,
branded error pages, and configuration validation.

## Key Patterns

- **Plugin architecture**: Caddy module registration via `caddy.RegisterModule()` in `init()`
- **Filesystem**: `pkg/gcs/fs/` implements `fs.FS`, `fs.StatFS`, `fs.ReadDirFile`, `io.ReadSeeker`
- **Caddyfile parsing**: `UnmarshalCaddyfile` methods parse Caddyfile directives
- **Client pooling**: `caddy.UsagePool` for GCS client reuse across config reloads
- **Attribute cache**: TTL-bounded in-memory cache with sampled eviction in `pkg/gcs/fs/attrcache.go`
- **Error pages**: Embedded HTML templates in `pkg/gcs/errorpages/`
- **Health checks**: Composite health checker pattern in `pkg/observability/health/`
- **Metrics**: OTel + Prometheus in `pkg/observability/metrics/`
- **Tracing**: OTel spans in `pkg/observability/tracing/`
- **Validation**: Runtime config validation in `pkg/validation/`

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#specification)
- **Signing**: All commits must be GPG/SSH signed (`-S`) and include DCO sign-off (`-s`)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic
- **Logging**: Use `zap.Logger` from Caddy context (`ctx.Logger()`)

## Build & Test Commands

```bash
# Build
task build

# Test
task test                        # Run all tests
task coverage:check              # Check coverage meets 85% threshold

# Linting
task lint                        # Run golangci-lint (uses pinned version)
task lint:fix                    # Auto-fix lint issues

# Security
task vulncheck                   # govulncheck
gosec ./...                      # gosec static analysis

# E2E
task e2e:test                    # Full e2e with fake-gcs-server
```

The project uses `task` (go-task/task) for builds and linting.
**Always use `task lint` instead of running `golangci-lint` directly**
to ensure the correct pinned version is used.

## Critical Rules

- **Caddy module interface compliance**: Always maintain interface guards (`var _ caddy.Module = (*Type)(nil)`)
- **After any change**: Run `task test` and `task lint` to ensure everything passes
- **Test coverage**: Minimum 85% total coverage enforced by `.testcoverage.yml`. Every new file needs tests
- **No magic values**: Always define constants for thresholds, sizes, and timeouts
- **Git safety**: Never run `git commit`, `git push`, or `git commit --amend` unless the user explicitly asks

## Security Scanning

```bash
gosec ./...
task vulncheck
```

## Additional Conventions

Go coding conventions (struct tags, error handling, design patterns),
testing rules, and documentation requirements are in
`.github/instructions/*.instructions.md` files --
they load automatically when editing relevant files.

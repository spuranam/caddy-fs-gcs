---
description: "Go coding conventions for caddy-fs-gcs: struct tags, error handling, design principles, Caddy module patterns, and formatting. Use when writing or editing Go code."
applyTo: "**/*.go"
---

# Go Conventions

## Caddy Module Patterns

- Register modules via `caddy.RegisterModule()` in `init()`
- Always maintain interface guards: `var _ caddy.Module = (*Type)(nil)`
- Implement `Provision(ctx caddy.Context)` for setup, `Cleanup()` for teardown
- Parse Caddyfile directives in `UnmarshalCaddyfile(d *caddyfile.Dispenser)`
- Use `caddy.UsagePool` for shared resources across config reloads

## Error Handling

Always wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("failed to open GCS object: %w", err)
}
```

## Design Principles

- Accept interfaces, return structs
- Keep interfaces small (1-3 methods)
- Define interfaces where they are used, not where they are implemented
- Use constructor functions for dependency injection
- Always pass `context.Context` as first parameter for timeout/cancellation control
- No package-level mutable state (except `sync.Once` initialization patterns)

## Logging

- Use `zap.Logger` from Caddy context: `ctx.Logger()`
- Never use `log.Printf`, `fmt.Printf`, or `slog` directly
- Use structured fields: `logger.Info("msg", zap.String("key", val))`

## Secret Management

Read secrets from environment variables -- never hardcode.

## Formatting

- **gofmt** and **goimports** are mandatory -- no style debates
- Never use magic strings or numbers; always define constants
- Use `const` blocks for related constants

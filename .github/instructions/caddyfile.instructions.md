---
description: "Caddyfile configuration conventions for caddy-fs-gcs. Filesystem blocks, directive syntax, and error handling patterns. Use when editing Caddyfile or Caddyfile-related Go code."
applyTo: "{**/Caddyfile*,pkg/**/module.go,pkg/**/validation_endpoint.go}"
---

# Caddyfile Conventions

## Filesystem Blocks

Declare named filesystems in the global `{}` block:

```caddyfile
{
    filesystem my-fs gcs {
        bucket_name my-bucket
        cache_ttl 5m
        cache_max_entries 10000
    }
}
```

## Directive Parsing

- Use `d.Next()` to advance past the directive name
- Use `d.RemainingArgs()` for positional arguments
- Use `d.NextBlock(0)` + `d.Val()` switch for block subdirectives
- Return `d.ArgErr()` for missing required arguments
- Return `d.Errf("message")` for invalid values

## Health & Metrics Directives

```caddyfile
gcs_health {
    enable_detailed
    enable_metrics
}

gcs_metrics

prometheus {
    enable_health
}
```

## Error Pages

Use Caddy's `handle_errors` with the embedded `error_pages` filesystem:

```caddyfile
filesystem error-pages error_pages

handle_errors {
    @404 expression {err.status_code} == 404
    handle @404 {
        rewrite * /404.html
        templates
        file_server { fs error-pages }
    }
}
```

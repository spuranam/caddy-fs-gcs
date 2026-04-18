# Configuration Reference

Complete configuration guide for the Caddy GCS Proxy plugin covering the
`caddy.fs.gcs` filesystem module, observability directives, authentication
methods, and standard Caddy middleware composition.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [GCS Filesystem Module](#gcs-filesystem-module)
- [Authentication](#authentication)
- [Attribute Cache](#attribute-cache)
- [Compression](#compression)
- [Precompressed Assets](#precompressed-assets)
- [Error Handling](#error-handling)
- [Observability](#observability)
- [Security Headers](#security-headers)
- [Multi-Bucket Routing](#multi-bucket-routing)
- [Client Connection Pooling](#client-connection-pooling)
- [Event Emission](#event-emission)
- [Full Configuration Reference](#full-configuration-reference)

## Architecture Overview

The plugin registers `caddy.fs.gcs` — a Go `fs.FS` / `fs.StatFS` filesystem
backed by Google Cloud Storage. Users declare named filesystems in Caddy's
global block, then reference them from `file_server`:

```caddyfile
{
    filesystem my-gcs gcs {
        bucket_name my-bucket
    }
}

example.com {
    file_server { fs my-gcs }
}
```

Because the module implements standard `fs.FS` interfaces, Caddy's built-in
`file_server` handles streaming, range requests, content negotiation, directory
indexes, and `try_files` — all for free. Compression, caching headers, error
pages, and other features are composed via standard Caddy directives.

### Registered Modules

| Module ID                         | Type       | Purpose                                          |
| --------------------------------- | ---------- | ------------------------------------------------ |
| `caddy.fs.gcs`                    | Filesystem | GCS-backed `fs.FS` / `fs.StatFS` with L1 cache   |
| `caddy.fs.error_pages`            | Filesystem | Branded error pages embedded in the binary       |
| `http.handlers.gcs_metrics`       | Middleware | OTel request metrics (duration, errors, cache)   |
| `http.handlers.prometheus`        | Middleware | Prometheus `/metrics` scrape endpoint            |
| `http.handlers.health`            | Middleware | Health/readiness/liveness/startup JSON endpoints |
| `http.handlers.config_validation` | Middleware | Configuration validation endpoint                |

### Caddyfile Directives

| Directive           | Module                            | Ordering         |
| ------------------- | --------------------------------- | ---------------- |
| `gcs_metrics`       | `http.handlers.gcs_metrics`       | After `metrics`  |
| `gcs_health`        | `http.handlers.health`            | Before `respond` |
| `prometheus`        | `http.handlers.prometheus`        | Before `respond` |
| `config_validation` | `http.handlers.config_validation` | Before `respond` |

## GCS Filesystem Module

The `caddy.fs.gcs` module is declared inside a `filesystem` global block.

### Minimal Example

```caddyfile
{
    filesystem my-gcs gcs {
        bucket_name my-bucket
    }
}

example.com {
    file_server {
        fs my-gcs
        index index.html
    }
}
```

This uses Application Default Credentials and serves files from the bucket
root. Caddy's `file_server` handles `index.html` resolution, content types,
ETags, and range requests automatically.

### Filesystem Directives

| Directive               | Type     | Default | Description                                    |
| ----------------------- | -------- | ------- | ---------------------------------------------- |
| `bucket_name`           | string   | —       | **Required.** GCS bucket name                  |
| `path_prefix`           | string   | `""`    | Prefix prepended to all object lookups         |
| `project_id`            | string   | `""`    | Google Cloud project ID (billing/quota)        |
| `credentials_file`      | string   | `""`    | Path to service account key JSON               |
| `credentials_config`    | string   | `""`    | Path to external account (WIF) credential JSON |
| `service_account`       | string   | `""`    | Service account email for impersonation        |
| `use_workload_identity` | bool     | `false` | Enable GKE/GCE Workload Identity (ADC)         |
| `cache_ttl`             | duration | `5m`    | Attribute cache TTL (`0` to disable)           |
| `cache_max_entries`     | int      | `10000` | Maximum cached attribute entries               |

### Path Prefix Example

```caddyfile
{
    filesystem site-fs gcs {
        bucket_name my-bucket
        path_prefix sites/prod
    }
}
```

A request to `/css/app.css` opens `sites/prod/css/app.css` in the bucket.

### Environment Variable Fallback

The filesystem module supports Caddy placeholder expansion
(`{env.VAR}`) in config values and also falls back to well-known
environment variables when a directive is left empty:

| Directive          | Env Var Fallback       |
| ------------------ | ---------------------- |
| `bucket_name`      | `GCS_BUCKET`           |
| `credentials_file` | `GCS_CREDENTIALS_FILE` |

```caddyfile
{
    filesystem my-gcs gcs {
        bucket_name {env.GCS_BUCKET}
    }
}
```

If `bucket_name` is omitted entirely and `GCS_BUCKET` is set in the
environment, it is used automatically.

## Authentication

If none of the credential directives are set, Application Default Credentials
(ADC) are used. When the `STORAGE_EMULATOR_HOST` environment variable is set
(e.g., for fake-gcs-server), authentication is disabled automatically.

### Workload Identity Federation (Recommended)

Best for production Kubernetes deployments. No stored credentials required.

```caddyfile
filesystem prod-fs gcs {
    bucket_name my-bucket
    use_workload_identity
    project_id my-gcp-project
}
```

Requires:

- Kubernetes cluster with Workload Identity enabled
- IAM binding between the K8s service account and a GCP service account
- The GCP service account needs `roles/storage.objectViewer` on the bucket

### External Account / WIF Credential Configuration

For non-GKE environments using Workload Identity Federation with an external
identity provider (e.g., Azure EntraID, AWS, GitHub Actions):

```caddyfile
filesystem wif-fs gcs {
    bucket_name my-bucket
    credentials_config /etc/caddy/wif-credentials.json
    project_id my-gcp-project
}
```

### Service Account Key

For development and testing only:

```caddyfile
filesystem dev-fs gcs {
    bucket_name my-bucket
    credentials_file /path/to/service-account.json
}
```

### Service Account Impersonation

Impersonate a different service account using the base credentials (ADC,
WIF, or SA key). The base credentials must have the
`roles/iam.serviceAccountTokenCreator` role on the target service account.

```caddyfile
filesystem impersonated-fs gcs {
    bucket_name restricted-bucket
    service_account reader@my-project.iam.gserviceaccount.com
}
```

Impersonation can be combined with other credential methods:

```caddyfile
filesystem wif-impersonate gcs {
    bucket_name my-bucket
    credentials_config /etc/caddy/wif-credentials.json
    service_account reader@my-project.iam.gserviceaccount.com
}
```

### Application Default Credentials

For local development with `gcloud auth application-default login`:

```caddyfile
filesystem local-fs gcs {
    bucket_name my-bucket
    project_id my-gcp-project
}
```

### GCS Emulator (Local Dev)

When `STORAGE_EMULATOR_HOST` is set, the client connects to the emulator
with authentication disabled:

```bash
export STORAGE_EMULATOR_HOST=http://localhost:4443
```

## Attribute Cache

The `caddy.fs.gcs` module includes an in-memory L1 cache for GCS
`ObjectAttrs`. This avoids redundant Stat() round-trips — Caddy's
`file_server` calls both `Stat()` and `Open()` per request.

### Configuration

```caddyfile
filesystem cached-fs gcs {
    bucket_name my-bucket
    cache_ttl 10m
    cache_max_entries 20000
}
```

### Disabling the Cache

Set `cache_ttl` to `0`:

```caddyfile
filesystem uncached-fs gcs {
    bucket_name my-bucket
    cache_ttl 0
}
```

### Cache Behavior

- Entries expire after `cache_ttl` (default 5 minutes)
- Maximum `cache_max_entries` entries (default 10,000)
- **Sample-based eviction**: when at capacity, 20 random entries are
  sampled and expired ones evicted (O(1) amortized vs O(n) full scan)
- If no expired entries are found during sampling, the entry closest to
  expiry is evicted to guarantee room for the new entry
- **Negative caching**: 404 results are cached with a shorter TTL
  (1/10 of `cache_ttl`, minimum 1 second) to avoid repeated GCS
  round-trips for missing objects (including extensionless path misses)
- **Shared monotonic clock**: a single process-wide background ticker
  updates a shared clock every 100 ms, amortizing `time.Now()` syscalls
  across all cache instances regardless of config reloads
- **O(1) key index**: an auxiliary `keyIndex` map enables constant-time
  removal of individual entries without a full scan
- Thread-safe via `sync.RWMutex`
- Atomic hit/miss counters track cache effectiveness (`CacheStats()`)

### Cache Statistics

The `GCSFS` module exposes cumulative cache hit and miss counts via the
`CacheStats()` method. These counters use `sync/atomic` for lock-free
increment and are safe for concurrent access.

```go
hits, misses := gcsfs.CacheStats()
hitRatio := float64(hits) / float64(hits+misses)
```

Returns `(0, 0)` when the attribute cache is disabled.

## Compression

Use Caddy's built-in `encode` directive for on-the-fly compression:

```caddyfile
example.com {
    encode {
        zstd default
        gzip 5
        minimum_length 256
    }

    file_server { fs my-gcs }
}
```

### Compression Levels

| Algorithm | Levels                                    |
| --------- | ----------------------------------------- |
| zstd      | `fastest`, `default`, `better`, `best`    |
| gzip      | `1` (fast) through `9` (best compression) |

`minimum_length` sets the minimum response size before compression kicks in.

## Precompressed Assets

If your build pipeline generates `.gz` / `.zst` files alongside originals,
use `precompressed` in `file_server` to serve them directly without
on-the-fly compression overhead:

```caddyfile
file_server {
    fs my-gcs
    precompressed zstd gzip
}
```

When a client sends `Accept-Encoding: gzip` and `index.html.gz` exists in
the bucket, Caddy serves the pre-compressed version with the correct
`Content-Encoding` header. The GCS handler checks all candidate
precompressed sidecars concurrently to minimize latency.

## Error Handling

### Embedded Error Pages

The plugin embeds branded error pages (404, 403, 500, and a generic default)
in the binary via the `caddy.fs.error_pages` module. These use Caddy's
`templates` directive for dynamic content.

```caddyfile
{
    filesystem my-gcs gcs {
        bucket_name my-bucket
    }
    filesystem error-pages error_pages
}
```

### Fallback Chain

Use Caddy's `handle_errors` to serve embedded branded error pages:

```caddyfile
handle_errors {
    @404 expression {err.status_code} == 404
    handle @404 {
        rewrite * /404.html
        templates
        file_server {
            fs error-pages
        }
    }

    @5xx expression {err.status_code} >= 500
    handle @5xx {
        rewrite * /500.html
        templates
        file_server {
            fs error-pages
        }
    }

    # Generic fallback
    handle {
        rewrite * /default.html
        templates
        file_server {
            fs error-pages
        }
    }
}
```

### Available Template Placeholders

The embedded error pages use Caddy's `{{placeholder "..."}}` syntax:

| Placeholder                        | Description              |
| ---------------------------------- | ------------------------ |
| `http.error.status_code`           | HTTP status code         |
| `http.error.status_text`           | HTTP status text         |
| `http.error.message`               | Error message            |
| `http.request.orig_uri`            | Original request URI     |
| `http.request.host`                | Request host             |
| `http.request.header.X-Request-ID` | Request ID for debugging |

### Customizing Error Pages

Upload custom templates to your GCS bucket at `errors/404.html`,
`errors/403.html`, `errors/500.html`. They take priority over the
embedded defaults when served first via `try_files` or a `route` block.

## Observability

### GCS Metrics (`gcs_metrics`)

Wraps all handlers to record OTel metrics per request:

```caddyfile
example.com {
    gcs_metrics
    file_server { fs my-gcs }
}
```

### Health Endpoints (`gcs_health`)

Provides JSON health check endpoints for Kubernetes probes:

```caddyfile
example.com {
    gcs_health {
        enable_detailed
        enable_metrics
    }
}
```

| Path              | Purpose                     | Enabled by default |
| ----------------- | --------------------------- | ------------------ |
| `/health`         | Overall health status       | Yes                |
| `/ready`          | Readiness for traffic       | Yes                |
| `/live`           | Liveness probe              | Yes                |
| `/startup`        | Startup completion check    | Yes                |
| `{path}/detailed` | Extended health information | `enable_detailed`  |
| `{path}/metrics`  | Health-specific metrics     | `enable_metrics`   |

The `/detailed` and `/metrics` sub-paths are derived from `path` (default
`/health`). When `detailed_local_only` is `true` (the default), these
endpoints only respond to requests from loopback addresses (127.0.0.1 / ::1).

**Error redaction:** Health responses served to non-loopback clients
automatically redact internal error details (e.g. raw GCS API errors) to
prevent information leakage. Loopback clients see full error messages for
debugging.

#### Health Directive Options

| Directive             | Type   | Default    | Description                                       |
| --------------------- | ------ | ---------- | ------------------------------------------------- |
| `path`                | string | `/health`  | Health check endpoint path (alias: `health_path`) |
| `readiness_path`      | string | `/ready`   | Readiness endpoint path                           |
| `liveness_path`       | string | `/live`    | Liveness endpoint path                            |
| `startup_path`        | string | `/startup` | Startup endpoint path                             |
| `enable_detailed`     | bool   | `false`    | Enable detailed health info                       |
| `enable_metrics`      | bool   | `false`    | Enable health metrics                             |
| `detailed_local_only` | bool   | `true`     | Restrict detailed/metrics to loopback             |
| `label`               | k/v    | ---        | Custom label key-value pair                       |

### Prometheus Endpoint (`prometheus`)

Serves Prometheus metrics at `/metrics`:

```caddyfile
example.com {
    prometheus {
        enable_health
    }
}
```

#### Prometheus Directive Options

| Directive          | Type   | Default    | Description                                   |
| ------------------ | ------ | ---------- | --------------------------------------------- |
| `path`             | string | `/metrics` | Metrics endpoint path                         |
| `enable_health`    | bool   | `false`    | Enable `/health` on this handler              |
| `enable_debug`     | bool   | `false`    | Enable `/debug/metrics`                       |
| `debug_local_only` | bool   | `true`     | Restrict `/debug/metrics` to loopback clients |
| `label`            | k/v    | —          | Custom label key-value pair                   |

### Kubernetes Probe Configuration

```yaml
livenessProbe:
  httpGet:
    path: /live
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10

startupProbe:
  httpGet:
    path: /startup
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 5
  failureThreshold: 30
```

## Security Headers

Use Caddy's built-in `header` directive for security headers:

```caddyfile
example.com {
    header {
        -Server
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy no-referrer-when-downgrade
        ?Content-Security-Policy "default-src 'none'; img-src 'self'; style-src 'self'"
    }
}
```

### Built-in Security

The following protections are always active in the `caddy.fs.gcs` module:

- **Path traversal prevention** — `fs.ValidPath()` enforcement, `..` rejection
- **Input validation** — path normalization and bucket name validation
- **Error translation** — internal GCS errors mapped to safe `fs.ErrNotExist` / `fs.ErrInvalid`
- **Duplicate directive detection** — duplicate Caddyfile directives produce a clear error
- **Metrics cardinality protection** — HTTP route labels are normalized to the
  first path segment (e.g. `/css/style.css` → `/css/`) to prevent unbounded
  Prometheus series from attacker-crafted URLs
- **Error page XSS prevention** — embedded error pages use Caddy's `templates`
  directive which HTML-escapes user-controlled placeholders
- **Validation endpoint access control** — the `config_validation` handler
  restricts endpoints to loopback addresses by default (`local_only`)
- **Health endpoint error redaction** — health responses served to non-loopback
  clients redact internal error details to prevent information leakage

### Configuration Validation (`config_validation`)

Provides JSON endpoints for validating GCS configuration:

```caddyfile
example.com {
    config_validation {
        enable_live
        enable_dry_run
    }
}
```

| Path                | Purpose                     | Enabled by default |
| ------------------- | --------------------------- | ------------------ |
| `/validate`         | Validate configuration JSON | Yes                |
| `/validate/live`    | Live runtime validation     | `enable_live`      |
| `/validate/dry-run` | Dry-run configuration check | `enable_dry_run`   |
| `/validate/status`  | Current validation status   | Yes                |

#### Validation Directive Options

| Directive          | Type   | Default     | Description                                |
| ------------------ | ------ | ----------- | ------------------------------------------ |
| `path`             | string | `/validate` | Validation endpoint path                   |
| `enable_live`      | bool   | `false`     | Enable live validation endpoint            |
| `enable_dry_run`   | bool   | `false`     | Enable dry-run validation endpoint         |
| `strict_mode`      | bool   | `false`     | Enable strict validation rules             |
| `local_only`       | bool   | `true`      | Restrict access to loopback addresses only |
| `validate_on_load` | bool   | `false`     | Validate configuration on module load      |
| `label`            | k/v    | ---         | Custom label key-value pair                |

> **Security note:** `local_only` defaults to `true`, restricting validation
> endpoints to requests from `127.0.0.0/8` and `::1`. Set to `false` only if
> you have separate authentication/authorization in place.

## Client Connection Pooling

The `caddy.fs.gcs` filesystem module uses `caddy.UsagePool` to share GCS
clients across config reloads. When multiple filesystem blocks use the same
credentials, they share a single `*storage.Client` instance. The client is
only closed when the last reference is removed.

This avoids unnecessary client re-initialization and token re-exchange during
graceful config reloads — especially important in Kubernetes deployments with
Workload Identity where token exchange has latency.

Pool keys are derived from the credential configuration:

| Credential Type     | Pool Key Example                                   |
| ------------------- | -------------------------------------------------- |
| Emulator            | `emulator:localhost:4443`                          |
| WIF / External      | `wif:/etc/caddy/wif-credentials.json`              |
| Service Account Key | `sa:/path/to/service-account.json`                 |
| ADC (default)       | `adc`                                              |
| ADC + Impersonation | `adc\|impersonate:sa@proj.iam.gserviceaccount.com` |

> **Note:** When combining a service account key with impersonation
> (`credentials_file` + `service_account`), the pool key is derived only
> from the credentials file path (e.g., `sa:/path/to/file`). The
> impersonation is applied at the token source level, not in the pool key.

## Event Emission

The GCS filesystem emits events via Caddy's `caddyevents` system. Events
are optional — they fire only when the events app is available.

| Event                  | When                                           | Payload Keys                      |
| ---------------------- | ---------------------------------------------- | --------------------------------- |
| `gcs.object_not_found` | Object or bucket does not exist                | `bucket`, `object`, `op`          |
| `gcs.backend_error`    | Unexpected GCS error (not 404, not path error) | `bucket`, `object`, `op`, `error` |

Subscribe to events in the Caddyfile global block:

```caddyfile
{
    events {
        on gcs.backend_error exec /usr/local/bin/alert.sh {event.data.bucket} {event.data.error}
    }
}
```

## Multi-Bucket Routing

Serve multiple GCS buckets at different URL paths by declaring a named
filesystem for each bucket:

```caddyfile
{
    filesystem sre-docs gcs {
        bucket_name sre-docs-bucket
        use_workload_identity
        project_id my-project
    }
    filesystem tekton-docs gcs {
        bucket_name tekton-docs-bucket
        use_workload_identity
        project_id my-project
    }
    filesystem assets gcs {
        bucket_name shared-assets-bucket
        cache_ttl 15m
    }
}

docs.example.com {
    encode zstd gzip

    handle /sre/* {
        uri strip_prefix /sre
        file_server {
            fs sre-docs
            index index.html
            precompressed zstd gzip
        }
    }

    handle /tekton/* {
        uri strip_prefix /tekton
        file_server {
            fs tekton-docs
            index index.html
            precompressed zstd gzip
        }
    }

    handle /assets/* {
        uri strip_prefix /assets
        file_server { fs assets }
    }
}
```

## Full Configuration Reference

Complete example with every feature:

```caddyfile
{
    # GCS filesystems — one per bucket
    filesystem docs-fs gcs {
        bucket_name docs-bucket
        use_workload_identity
        project_id my-gcp-project
        cache_ttl 5m
        cache_max_entries 10000
    }

    filesystem assets-fs gcs {
        bucket_name assets-bucket
        cache_ttl 15m
    }

    # Branded error pages embedded in the binary
    filesystem error-pages error_pages
}

example.com {
    # Compression — zstd preferred, gzip fallback
    encode {
        zstd default
        gzip 5
        minimum_length 256
    }

    # Security headers
    header {
        -Server
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy no-referrer-when-downgrade
        Cross-Origin-Embedder-Policy require-corp
    }

    # Cache headers
    header Cache-Control "public, max-age=3600"

    # GCS request metrics (wraps all handlers)
    gcs_metrics

    # Health / readiness / liveness / startup
    gcs_health {
        enable_detailed
        enable_metrics
    }

    # Prometheus metrics
    prometheus {
        enable_health
    }

    # Docs site
    handle /docs/* {
        uri strip_prefix /docs
        file_server {
            fs docs-fs
            index index.html
            precompressed zstd gzip
        }
    }

    # Static assets
    handle /assets/* {
        uri strip_prefix /assets
        file_server {
            fs assets-fs
            precompressed zstd gzip
        }
    }

    # Error handling — branded error pages with fallback
    handle_errors {
        @404 expression {err.status_code} == 404
        handle @404 {
            rewrite * /404.html
            templates
            file_server {
                fs error-pages
            }
        }

        @5xx expression {err.status_code} >= 500
        handle @5xx {
            rewrite * /500.html
            templates
            file_server {
                fs error-pages
            }
        }

        handle {
            rewrite * /default.html
            templates
            file_server {
                fs error-pages
            }
        }
    }
}
```

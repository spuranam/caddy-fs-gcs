# Developer Guide

Architecture, code organization, API reference, testing, and contributing guide
for the Caddy GCS Proxy plugin.

## Table of Contents

- [Architecture](#architecture)
- [Development Setup](#development-setup)
- [Code Organization](#code-organization)
- [API Reference](#api-reference)
- [Testing](#testing)
- [Contributing](#contributing)

## Architecture

### System Overview

```text
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │────▶│   Caddy Server   │────▶│  Google Cloud   │
│  (Browser)  │◀────│   + GCS Plugin   │◀────│  Storage        │
└─────────────┘     └──────────────────┘     └─────────────────┘
```

The plugin registers `caddy.fs.gcs` — a Go `fs.FS` / `fs.StatFS` virtual
filesystem backed by Google Cloud Storage. Users declare named filesystems
in Caddy's global block, then reference them from `file_server`. Caddy's
built-in `file_server` handles streaming, range requests, content negotiation,
directory indexes, and `try_files` — the plugin is a thin filesystem adapter.

### Request Flow

```text
1. HTTP Request arrives at Caddy
2. gcs_metrics middleware records start time
3. gcs_health / prometheus intercept /health, /ready, /live, /metrics
4. file_server receives the request
5. file_server calls gcsFS.Stat(name)
   → L1 attrCache checked first
   → on miss: GCS ObjectAttrs fetched and cached
6. file_server calls gcsFS.Open(name) → returns gcsFile
7. gcsFile implements io.ReadSeeker via GCS NewRangeReader
   → Caddy handles Range, ETag, 304, Content-Type natively
8. encode middleware compresses response if applicable
9. Response sent
```

### Component Architecture

```text
caddy.fs.gcs (GCSFS)
├── BucketName, PathPrefix         — configuration
├── CacheTTL, CacheMaxEntries      — cache config
├── client    *storage.Client      — GCS API client (from UsagePool)
├── poolKey   string               — UsagePool key for client reuse
└── StatFS    gcsFS                — the actual fs.FS implementation
    ├── bucket   *storage.BucketHandle
    ├── prefix   string            — path prefix for Sub()
    ├── onEvent  func(...)         — event emission callback (caddyevents)
    ├── onCacheHit   func()        — metrics: attribute cache hit
    ├── onCacheMiss  func()        — metrics: attribute cache miss
    ├── onGCSOp      func(...)     — metrics: GCS API call timing
    ├── onStreamBytes func(...)    — metrics: bytes read from GCS
    └── cache    *attrCache        — L1 attribute cache
        ├── entries map[string]attrEntry (sync.RWMutex)
        ├── ttl     time.Duration
        ├── maxEntries int
        ├── hits   atomic.Int64    — cache hit counter
        └── misses atomic.Int64    — cache miss counter

gcsClients (caddy.UsagePool)       — package-level client pool
├── key → *storage.Client          — ref-counted by credential config
└── clientDestructor.Destruct()    — closes client when refs reach 0

caddy.fs.error_pages (EmbeddedErrorPages)
├── embed.FS                       — //go:embed caddy/*.html
└── FS       fs.FS                 — sub-filesystem (stripped "caddy/" prefix)

http.handlers.gcs_metrics (MetricsHandler)
├── OTel meter + instruments       — request duration, errors, cache hits
└── Wraps next handler, records metrics

http.handlers.health (HealthEndpointHandler)
├── /health, /ready, /live, /startup
├── /health/detailed, /health/metrics
└── Composite health checker

http.handlers.prometheus (PrometheusEndpointHandler)
├── /metrics (promhttp)
└── /debug/metrics, /metrics/health
```

## Development Setup

### Prerequisites

- **Go** 1.26+
- **Task** ([taskfile.dev](https://taskfile.dev/)) for build automation
- **Git**

### Getting Started

```bash
git clone https://github.com/spuranam/caddy-fs-gcs.git
cd caddy-fs-gcs
task install-tools   # install linter, formatter, etc.
task build           # build ./dist/caddy
task test            # run all tests
```

### Local Development with Emulator

Run against a fake GCS server (no GCP credentials needed):

```bash
task dev:local       # starts fake-gcs-server + Caddy with Caddyfile.dev
```

Or start just the emulator:

```bash
task dev:emulator    # starts fake-gcs-server only
```

### Common Tasks

```bash
task build           # Build the binary
task test            # Run tests
task lint            # golangci-lint
task format          # goimports + gofumpt + markdownlint
task coverage        # Generate coverage profile
task coverage:report # Per-package coverage summary
task coverage:compare # Compare coverage against saved baseline
task security:scan   # gosec + govulncheck + mod verify
task ci              # Full CI pipeline (lint + test + coverage + build)
task clean           # Remove build artifacts
task clean:cache     # Clean only Go caches (preserves artifacts)
task clean:build     # Clean only build artifacts (preserves caches)
task mod:check       # Check if go.mod and go.sum are in sync
task tag             # Create a signed annotated Git tag
```

Run `task --list-all` for the complete list.

## Code Organization

```text
caddy-fs-gcs/
├── cmd/caddy/                  # Caddy main entry point (imports plugin)
├── pkg/
│   ├── gcs/
│   │   ├── plugin.go           # Module + directive registration (init)
│   │   ├── fs/                 # caddy.fs.gcs — primary filesystem module
│   │   │   ├── module.go       # GCSFS Caddy module wiring + UnmarshalCaddyfile
│   │   │   ├── gcsfs.go        # fs.FS/StatFS/SubFS + io.ReadSeeker impl
│   │   │   ├── attrcache.go    # TTL-bounded concurrent attribute cache
│   │   │   ├── module_test.go
│   │   │   ├── gcsfs_test.go
│   │   │   └── attrcache_test.go
│   │   └── errorpages/         # Embedded branded error pages
│   │       ├── caddy_module.go # caddy.fs.error_pages module (EmbeddedErrorPages)
│   │       └── caddy/          # HTML templates (404, 403, 500, default)
│   ├── validation/             # Config validation & validation endpoint
│   │   ├── config.go           # ConfigValidator (bucket names, URLs, etc.)
│   │   └── validation_endpoint.go # Caddy module: config_validation endpoint
│   └── observability/
│       ├── metrics/            # OTel metrics + Prometheus endpoint + gcs_metrics
│       ├── health/             # Health endpoint (Caddy module: gcs_health)
│       ├── tracing/            # OTel distributed tracing
│       └── logger/             # Structured slog wrapper
├── e2e/                        # End-to-end test infrastructure
└── Taskfile.yaml               # Build automation
```

### Key Dependencies

| Dependency                            | Purpose                       |
| ------------------------------------- | ----------------------------- |
| `cloud.google.com/go/storage`         | GCS client                    |
| `github.com/caddyserver/caddy/v2`     | Caddy server framework        |
| `github.com/klauspost/compress`       | zstd compression              |
| `go.opentelemetry.io/otel`            | Metrics, tracing, propagation |
| `github.com/prometheus/client_golang` | Prometheus metrics exposition |
| `github.com/stretchr/testify`         | Test assertions and mocks     |

## API Reference

### GCSFS — Primary Module (`caddy.fs.gcs`)

The core filesystem module. Implements `fs.FS`, `fs.StatFS`, and `fs.SubFS`.

```go
type GCSFS struct {
    fs.StatFS `json:"-"`

    BucketName          string         `json:"bucket_name,omitempty"`
    PathPrefix          string         `json:"path_prefix,omitempty"`
    ProjectID           string         `json:"project_id,omitempty"`
    CredentialsFile     string         `json:"credentials_file,omitempty"`
    CredentialsConfig   string         `json:"credentials_config,omitempty"`
    ServiceAccount      string         `json:"service_account,omitempty"`
    UseWorkloadIdentity bool           `json:"use_workload_identity,omitempty"`
    CacheTTL            caddy.Duration `json:"cache_ttl,omitempty"`
    CacheMaxEntries     int            `json:"cache_max_entries,omitempty"`

    client  *storage.Client
    poolKey string            // UsagePool key for client reuse across reloads
}
```

#### Lifecycle Methods

| Method                     | Description                                                       |
| -------------------------- | ----------------------------------------------------------------- |
| `CaddyModule()`            | Returns module info (ID: `caddy.fs.gcs`)                          |
| `Provision(caddy.Context)` | Init client via UsagePool, cache, events; expands placeholders    |
| `Cleanup()`                | Release pool reference; client closed when last ref removed       |
| `UnmarshalCaddyfile(d)`    | Parse filesystem block directives (with duplicate detection)      |
| `CacheStats()`             | Returns `(hits, misses int64)` from the attribute cache           |
| `clientPoolKey()`          | Builds a deterministic pool key from the credential configuration |

#### Helper Functions

| Function                       | Description                                                |
| ------------------------------ | ---------------------------------------------------------- |
| `loadEventsApp(caddy.Context)` | Safely loads `*caddyevents.App`; returns nil outside Caddy |
| `clientDestructor.Destruct()`  | `caddy.Destructor` — closes the wrapped `*storage.Client`  |

#### Interfaces Implemented by GCSFS

| Interface               | Notes                                       |
| ----------------------- | ------------------------------------------- |
| `fs.FS`                 | `Open(name)` -> `gcsFile`                   |
| `fs.StatFS`             | `Stat(name)` -> `gcsFileInfo` (cache-aware) |
| `caddy.Module`          | Module registration                         |
| `caddy.Provisioner`     | Initialization                              |
| `caddy.CleanerUpper`    | Cleanup                                     |
| `caddyfile.Unmarshaler` | Caddyfile parsing                           |

#### Interfaces Implemented by gcsFS (inner filesystem)

| Interface   | Notes                                       |
| ----------- | ------------------------------------------- |
| `fs.FS`     | `Open(name)` -> `gcsFile`                   |
| `fs.StatFS` | `Stat(name)` -> `gcsFileInfo` (cache-aware) |
| `fs.SubFS`  | `Sub(dir)` -> new `gcsFS` with prefix       |

#### Interfaces Implemented by gcsFile

| Interface        | Notes                                  |
| ---------------- | -------------------------------------- |
| `fs.File`        | Basic file operations                  |
| `fs.ReadDirFile` | Directory listing via GCS prefix query |
| `io.ReadSeeker`  | Seek via GCS `NewRangeReader`          |

### gcsFile & gcsFileInfo

`gcsFile` implements `fs.File`, `fs.ReadDirFile`, and `io.ReadSeeker`.
`gcsFS` provides a `WithContext(ctx)` method that returns a shallow copy
carrying a per-request context.

- **Read**: streams from GCS via `NewRangeReader(ctx, offset, -1)`
- **Seek**: resets position and lazily reopens the reader on next `Read()`
- **ReadDir**: lists objects with `Prefix` and `/` delimiter
- **Stat**: returns `gcsFileInfo` from `ObjectAttrs`

`gcsFileInfo` wraps `*storage.ObjectAttrs` → `fs.FileInfo`.

### attrCache

TTL-bounded, concurrency-safe in-memory cache for `*storage.ObjectAttrs`
with sample-based eviction, negative caching, and a shared monotonic clock.

```go
func newAttrCache(ttl time.Duration, maxEntries int) *attrCache
func (c *attrCache) get(key string) (*storage.ObjectAttrs, bool)
func (c *attrCache) set(key string, attrs *storage.ObjectAttrs)
func (c *attrCache) setNotFound(key string) // negative cache entry
func (c *attrCache) stop()                  // halt background clock ticker
func (c *attrCache) len() int
func (c *attrCache) stats() (hits, misses int64)
func isNegative(attrs *storage.ObjectAttrs) bool
```

Key implementation details:

- **Sample-based eviction**: when at capacity, 20 random entries are
  sampled and expired ones evicted (O(1) amortized vs O(n) full scan)
- **Negative caching**: `setNotFound()` stores a sentinel entry with
  1/10 of the configured TTL (minimum 1 second)
- **Monotonic clock**: a background goroutine (`tickClock`) updates a
  shared `atomic.Int64` every 100 ms, amortizing `time.Now()` syscalls
- **O(1) key index**: an auxiliary `keyIndex map[string]int` maps object
  names to their slot in the cache slice, enabling constant-time removal
- **Value-type cache entries**: entries are stored as values (not
  pointers), reducing GC pressure for caches with many entries
- `stop()` must be called on teardown to halt the clock ticker

### EmbeddedErrorPages (`caddy.fs.error_pages`)

Serves branded error pages compiled into the binary:

```go
type EmbeddedErrorPages struct {
    fs.FS `json:"-"`
}
```

Files: `404.html`, `403.html`, `500.html`, `default.html` (in the `caddy/`
embed directory). The module provides an `fs.FS` that can be used with
Caddy's `file_server` inside `handle_errors` blocks to serve branded
error pages.

### Registered Caddy Modules

| Module ID                         | Type                        | Caddyfile Directive          | Purpose                    |
| --------------------------------- | --------------------------- | ---------------------------- | -------------------------- |
| `caddy.fs.gcs`                    | `GCSFS`                     | `filesystem ... gcs`         | GCS-backed filesystem      |
| `caddy.fs.error_pages`            | `EmbeddedErrorPages`        | `filesystem ... error_pages` | Embedded error pages       |
| `http.handlers.gcs_metrics`       | `MetricsHandler`            | `gcs_metrics`                | OTel metrics middleware    |
| `http.handlers.prometheus`        | `PrometheusEndpointHandler` | `prometheus`                 | Prometheus scrape endpoint |
| `http.handlers.health`            | `HealthEndpointHandler`     | `gcs_health`                 | Health/ready/live endpoint |
| `http.handlers.config_validation` | `ValidationEndpointHandler` | `config_validation`          | Config validation endpoint |

### OTel Metrics Instruments

| Instrument                     | Type               | Description               |
| ------------------------------ | ------------------ | ------------------------- |
| `http.server.request.duration` | Float64Histogram   | Request latency (seconds) |
| `http.server.request.total`    | Int64Counter       | Total requests            |
| `http.server.request.errors`   | Int64Counter       | Error count               |
| `http.server.response.size`    | Float64Histogram   | Response size (bytes)     |
| `gcs.operation.duration`       | Float64Histogram   | GCS API call latency      |
| `gcs.operation.total`          | Int64Counter       | GCS API calls             |
| `gcs.operation.errors`         | Int64Counter       | GCS API errors            |
| `gcs.streaming.bytes`          | Int64Counter       | Bytes streamed            |
| `gcs.concurrent.requests`      | Int64UpDownCounter | In-flight requests        |

The `gcs_metrics` `responseWriter` wrapper implements `io.ReaderFrom`
so that the kernel `sendfile(2)` optimization is preserved when metrics
are enabled.

The `gcs_health` startup probe verifies GCS storage connectivity before
marking startup complete. When health checkers are registered, the
`/startup` endpoint calls `CheckStorage()` on the first probe request
and only returns healthy once storage is reachable. When no health
checkers are registered, startup completes immediately.

## Testing

### Running Tests

```bash
task test                        # all tests
task test:cover                  # tests with coverage profile
task coverage:report             # per-package summary
task coverage:html               # HTML coverage report
task coverage:check              # check against minimum threshold
```

### Test Structure

Tests live alongside their source files (`*_test.go`). Key test files:

| File                                         | Coverage                                             |
| -------------------------------------------- | ---------------------------------------------------- |
| `pkg/gcs/fs/gcsfs_test.go`                   | fs.FS, Stat, Seek, error translation, event emission |
| `pkg/gcs/fs/attrcache_test.go`               | Cache TTL, eviction, concurrency, hit/miss stats     |
| `pkg/gcs/fs/module_test.go`                  | Pool reuse, duplicate directives, CacheStats, events |
| `pkg/gcs/errorpages/caddy_module_test.go`    | Caddy module provisioning                            |
| `pkg/validation/config_test.go`              | Config validation rules                              |
| `pkg/validation/validation_endpoint_test.go` | Validation HTTP endpoints                            |
| `pkg/observability/metrics/*_test.go`        | OTel metrics, Prometheus endpoint                    |
| `pkg/observability/health/*_test.go`         | Health endpoint                                      |
| `pkg/observability/tracing/*_test.go`        | OTel tracing                                         |
| `pkg/observability/logger/*_test.go`         | Structured logging                                   |

### Mocking

GCS operations are tested against `fake-gcs-server` in the E2E suite and
via in-memory fakes in unit tests. See `pkg/gcs/fs/gcsfs_test.go` for
examples.

### End-to-End Tests

```bash
task e2e:test    # builds site, starts emulator + Caddy, runs smoke tests, shuts down
task e2e:serve   # runs environment interactively
task e2e:smoke   # runs smoke tests against a running server
```

E2E tests use `fake-gcs-server` as a GCS emulator and serve a pre-built Hugo
Docsy site.

## Contributing

1. Fork and clone the repository
2. Create a feature branch: `git checkout -b feature/your-feature`
3. Make changes following Go conventions
4. Run quality checks:

   ```bash
   task test
   task lint
   task security:scan
   ```

5. Submit a pull request with a clear description

### Code Quality Checklist

- [ ] Tests cover new functionality
- [ ] `task lint` passes with zero issues
- [ ] `task test` passes
- [ ] Documentation updated if behavior changed

---

For configuration details, see [Configuration Reference](CONFIGURATION.md).
For operations and troubleshooting, see [Operations Guide](OPERATIONS.md).

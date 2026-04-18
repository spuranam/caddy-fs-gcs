# Operations Guide

Production operations guide covering performance tuning, monitoring, health checks,
and troubleshooting for the Caddy GCS Proxy.

## Table of Contents

- [Performance Tuning](#performance-tuning)
- [Attribute Cache Tuning](#attribute-cache-tuning)
- [Monitoring](#monitoring)
- [Health Checks](#health-checks)
- [Troubleshooting](#troubleshooting)
- [Deployment Recommendations](#deployment-recommendations)

## Performance Tuning

### Compression Tuning

Use Caddy's built-in `encode` directive with configurable compression levels:

```caddyfile
encode {
    zstd default
    gzip 5
    minimum_length 256
}
```

| Profile             | zstd level | gzip level | Notes                         |
| ------------------- | ---------- | ---------- | ----------------------------- |
| Low latency         | `fastest`  | `1`        | Minimal CPU, larger responses |
| Balanced (default)  | `default`  | `5`        | Good tradeoff                 |
| Maximum compression | `best`     | `9`        | High CPU, smallest responses  |

**Do not compress**: images (PNG, JPEG, WebP), video, audio, or already-compressed
archives. Caddy's `encode` directive handles this automatically based on
content type.

### Precompressed Assets

If your build pipeline generates `.gz` / `.zst` files alongside originals,
use `precompressed` to avoid on-the-fly compression overhead entirely.
The GCS handler checks all candidate sidecars concurrently to minimize
latency:

```caddyfile
file_server {
    fs my-gcs
    precompressed zstd gzip
}
```

### Range Requests & Streaming

The `caddy.fs.gcs` module implements `io.ReadSeeker` via GCS
`NewRangeReader`, so Caddy's `file_server` natively supports:

- HTTP Range requests → `206 Partial Content`
- Conditional requests → `304 Not Modified` (via ETag/Last-Modified)
- Resumable downloads
- Media seeking
- **Small forward seek optimization**: seeks of 32 KiB or less discard bytes
  from the existing reader instead of closing and reopening a new GCS HTTP
  connection, reducing latency for MIME-sniffing and small range adjustments

No configuration needed — this is always active.

## Attribute Cache Tuning

The `caddy.fs.gcs` module includes a built-in L1 attribute cache to avoid
redundant `Stat()` round-trips. Caddy's `file_server` calls both `Stat()` and
`Open()` per request; the cache ensures the second round-trip hits memory.

### Configuration

```caddyfile
filesystem my-gcs gcs {
    bucket_name my-bucket
    cache_ttl 5m
    cache_max_entries 10000
}
```

### Tuning by Workload

| Workload                    | `cache_ttl` | `cache_max_entries` |
| --------------------------- | ----------- | ------------------- |
| Frequently changing content | `1m`        | 5000                |
| Static documentation sites  | `10m`       | 20000               |
| Immutable assets (hashed)   | `30m`       | 50000               |
| Real-time content           | `0` (off)   | —                   |

### Cache Behavior

- Entries expire after `cache_ttl` (default 5 minutes)
- Maximum `cache_max_entries` entries (default 10,000)
- **Sample-based eviction**: when at capacity, up to 20 random entries
  are sampled and expired ones evicted (O(1) amortized)
- If no expired entries are found, the entry closest to expiry is evicted
  to guarantee room for new entries
- **Negative caching**: 404 results cached with 1/10 of `cache_ttl`
  (minimum 1 second) to reduce GCS round-trips for missing objects,
  including extensionless paths that require a directory probe
- **Shared process-wide clock**: a single background ticker (100 ms)
  is shared across all cache instances to minimize goroutine overhead
- Thread-safe via `sync.RWMutex`

## Monitoring

### OpenTelemetry Metrics

The `gcs_metrics` directive wraps all handlers to record OTel metrics.
The `prometheus` directive serves a Prometheus scrape endpoint.

```caddyfile
example.com {
    gcs_metrics

    prometheus {
        enable_health
    }

    file_server { fs my-gcs }
}
```

#### Key Metrics

| Metric                         | Type               | Description               |
| ------------------------------ | ------------------ | ------------------------- |
| `http.server.request.duration` | Float64Histogram   | Request latency (seconds) |
| `http.server.request.total`    | Int64Counter       | Total HTTP requests       |
| `http.server.request.errors`   | Int64Counter       | HTTP error count          |
| `http.server.response.size`    | Float64Histogram   | Response size (bytes)     |
| `gcs.operation.duration`       | Float64Histogram   | GCS API call latency      |
| `gcs.operation.total`          | Int64Counter       | GCS API calls             |
| `gcs.operation.errors`         | Int64Counter       | GCS API errors            |
| `gcs.streaming.bytes`          | Int64Counter       | Total bytes streamed      |
| `gcs.concurrent.requests`      | Int64UpDownCounter | In-flight request count   |
| `gcs.cache.hits`               | Int64Counter       | Cache hits                |
| `gcs.cache.misses`             | Int64Counter       | Cache misses              |

#### Prometheus Scrape Config

The `prometheus` directive serves metrics on the user-facing HTTP port
(not the admin API):

```yaml
scrape_configs:
  - job_name: "caddy-fs-gcs"
    static_configs:
      - targets: ["caddy-fs-gcs:8080"]
    metrics_path: "/metrics"
    scrape_interval: 15s
```

### Distributed Tracing

The plugin produces OpenTelemetry traces with service name `caddy-fs-gcs`.
Supported exporters:

- OTLP over gRPC
- OTLP over HTTP
- stdout (for debugging)

Traces include `http.method`, `http.url`, `http.user_agent`,
`http.remote_addr` attributes, with per-request spans for GCS operations.

Configurable sampling ratio from 0.0 (no traces) to 1.0 (all traces).
The sample rate is set by the caller (e.g., via environment or
Caddyfile configuration); no built-in default is applied by this module.

### Structured Logging

Configure via Caddy's built-in log directive:

```caddyfile
log {
    output stdout
    format json
    level INFO
}
```

The observability logger enriches entries with trace context (`trace_id`,
`span_id`) when available.

## Health Checks

### Endpoints

The `gcs_health` directive registers the following endpoints:

```caddyfile
gcs_health {
    enable_detailed
    enable_metrics
}
```

| Endpoint  | Default Path       | Purpose                                   |
| --------- | ------------------ | ----------------------------------------- |
| Health    | `/health`          | Overall status + per-component checks     |
| Detailed  | `/health/detailed` | Extended health information               |
| Metrics   | `/health/metrics`  | Health metrics data                       |
| Readiness | `/ready`           | Ready to serve traffic                    |
| Liveness  | `/live`            | Process alive confirmation                |
| Startup   | `/startup`         | Startup probe (verifies GCS connectivity) |

All paths are configurable via the `gcs_health` block.

### Health Response Format

```json
{
  "status": "healthy",
  "message": "Health endpoint is working",
  "timestamp": "2026-04-05T10:30:00Z",
  "details": {
    "service": "caddy-fs-gcs",
    "uptime": "2h15m30s"
  }
}
```

### Kubernetes Integration

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

## Troubleshooting

### Common Issues

#### Objects Not Found (404)

1. Check `bucket_name` in the `filesystem` block is correct
2. Check `path_prefix` — with prefix `sites/prod`, file `index.html`
   reads `sites/prod/index.html` from GCS
3. Check `file_server { index index.html }` — directory requests need an index
4. Verify the object exists: `gsutil ls gs://my-bucket/path/to/file`

#### Authentication Failures

1. **Workload Identity**: verify the K8s service account has the IAM binding:

   ```bash
   gcloud iam service-accounts get-iam-policy SA@PROJECT.iam.gserviceaccount.com
   ```

2. **External account (WIF)**: verify `credentials_config` path is correct
   and the JSON file contains a valid external account configuration
3. **Service account key**: verify `credentials_file` path is correct and readable
4. **ADC**: run `gcloud auth application-default login`
5. **Emulator**: ensure `STORAGE_EMULATOR_HOST` is set before starting Caddy
6. Check the service account has `roles/storage.objectViewer` on the bucket

#### High Attribute Cache Miss Rate

1. Check `cache_ttl` — too short causes frequent GCS round-trips
2. Check `cache_max_entries` — if your site has > 10,000 unique paths, increase this
3. Set `cache_ttl 0` to disable caching and isolate whether it's a cache issue

#### Slow Responses

1. Add `gcs_metrics` and check `gcs.operation.duration` — if high, the issue
   is GCS latency (consider longer `cache_ttl`)
2. Enable compression: `encode zstd gzip`
3. Use `precompressed zstd gzip` to serve pre-built compressed assets
4. Check the Prometheus endpoint for request latency distributions

### Debug Logging

Set Caddy's log level to debug for detailed request logging:

```caddyfile
{
    log {
        level DEBUG
    }
}
```

## Deployment Recommendations

### Container Image

```bash
task build-container-image    # build the image
task push-container-image     # push to registry
```

### Resource Sizing

| Workload       | CPU | Memory | Replicas |
| -------------- | --- | ------ | -------- |
| Low traffic    | 0.5 | 256 MB | 1–2      |
| Medium traffic | 1.0 | 512 MB | 2–3      |
| High traffic   | 2.0 | 1 GB   | 3+       |

### Production Checklist

- [ ] Use Workload Identity for authentication (no service account keys)
- [ ] Add `encode { zstd default; gzip 5; minimum_length 256 }` for compression
- [ ] Add `precompressed zstd gzip` to `file_server` if build generates compressed assets
- [ ] Configure `gcs_health` with Kubernetes liveness/readiness/startup probes
- [ ] Add `gcs_metrics` and `prometheus` directives for metrics
- [ ] Set `Cache-Control` headers via Caddy's `header` directive
- [ ] Tune `cache_ttl` and `cache_max_entries` for your access patterns
- [ ] Set security headers (`X-Content-Type-Options`, `X-Frame-Options`, etc.)
- [ ] Configure `handle_errors` with branded error page fallback chain

---

For configuration details, see [Configuration Reference](CONFIGURATION.md).
For architecture and API reference, see [Developer Guide](DEVELOPER-GUIDE.md).

# Caddy GCS Proxy

A Caddy v2 plugin for serving files from Google Cloud Storage. Implements
Go's `fs.FS` / `fs.StatFS` interfaces so GCS buckets plug directly into Caddy's built-in
`file_server`, gaining directory listings, `try_files`, `precompressed`, range requests,
and all standard middleware — no monolithic custom handler required.

## 🚀 **Quick Start**

```bash
# 1. Clone the repository
git clone https://github.com/spuranam/caddy-fs-gcs.git
cd caddy-fs-gcs

# 2. Build Caddy with the plugin
task build

# 3. Start Caddy
./dist/caddy run --config ./Caddyfile.dev
```

## ✨ **Features**

### 🔐 **Authentication & Security**

- **Workload Identity Federation**: Secure authentication without service account keys
- **External Account / WIF Credentials**: For non-GKE environments (Azure EntraID, AWS, GitHub Actions)
- **Service Account Keys**: Development and testing authentication
- **Application Default Credentials**: Local development authentication
- **Security Headers**: Automatic security header management via Caddy's `header` directive
- **Input Validation**: Path traversal protection and object name validation

### ⚡ **Performance & Reliability**

- **`fs.FS` / `fs.StatFS` Integration**: Plugs into Caddy's `file_server` for native streaming, range requests, and
  content negotiation
- **`io.ReadSeeker` Support**: GCS range reads (`NewRangeReader`) for efficient seek/partial content
- **L1 Attribute Cache**: TTL-bounded, concurrency-safe in-memory cache for `ObjectAttrs` — avoids redundant `Stat()` round-trips
- **Compression**: Caddy's built-in `encode` directive with configurable zstd/gzip levels
- **Precompressed Assets**: Serve `.gz`/`.zst` files alongside originals via `precompressed`
- **Multi-Bucket Routing**: Serve multiple GCS buckets at different URL paths with named filesystems

### 📊 **Observability & Monitoring**

- **Health Checks**: JSON health/readiness/liveness/startup endpoints via `gcs_health` directive
- **Prometheus Metrics**: OTel-based metrics with Prometheus scrape endpoint via `prometheus` directive
- **GCS Request Metrics**: Per-request duration, error counts, cache hits via `gcs_metrics` directive
- **Structured Logging**: Caddy's built-in structured logging with correlation ID support
- **Distributed Tracing**: OpenTelemetry tracing with configurable exporters

### 🎨 **Error Pages**

- **Embedded Branded Templates**: 404, 403, 500, and default error pages compiled into the binary (`caddy.fs.error_pages`)
- **GCS Bucket Overrides**: Upload custom error pages to the bucket; they take priority over embedded defaults
- **Caddy Template Variables**: Dynamic content via `{{placeholder "http.error.status_code"}}` etc.

### 🛠️ **Management & Operations**

- **Taskfile Integration**: Comprehensive build, test, and deployment automation
- **Kubernetes Ready**: Proper liveness/readiness/startup probes
- **Container Support**: Containerized deployment with `task build-container-image`
- **End-to-End Testing**: E2E tests with fake-gcs-server serving a Hugo Docsy site

## **Configuration**

Minimal Caddyfile using the `caddy.fs.gcs` filesystem module:

```caddyfile
{
    filesystem my-gcs gcs {
        bucket_name my-bucket
        use_workload_identity
        project_id my-gcp-project
    }
}

example.com {
    file_server { fs my-gcs }
}
```

For all configuration options and examples, see
[Configuration Reference](docs/CONFIGURATION.md).

## 📚 **Documentation**

- **[📚 Complete Documentation](docs/README.md)** - Comprehensive guides, examples, and reference materials
- **[🔧 Configuration Reference](docs/CONFIGURATION.md)** - All directives, auth methods, path handling, K8s deployment
- **[📊 Operations Guide](docs/OPERATIONS.md)** - Performance tuning, monitoring, troubleshooting, health checks
- **[👨‍💻 Developer Guide](docs/DEVELOPER-GUIDE.md)** - Architecture, API reference, testing, contributing

## 🚀 **Development**

### Using Taskfile

This project uses [Taskfile](https://taskfile.dev/) for better cross-platform support and cleaner syntax.

#### Common Tasks

```bash
# Show all available tasks
task --list-all

# Development workflow
task build              # Build the CLI binary
task test               # Run test suite
task lint               # Run golangci-lint
task lint:fix           # Run golangci-lint and auto-fix issues
task format             # Auto-format code and markdown
task format:check       # Check formatting without modifying files

# Testing & Coverage
task test:cover         # Run tests with coverage profile
task coverage           # Generate coverage profile
task coverage:report    # Per-package coverage summary
task coverage:html      # Generate HTML coverage report
task coverage:check     # Check coverage meets minimum threshold
task coverage:baseline  # Save current coverage as baseline
task coverage:compare   # Compare coverage against saved baseline
task coverage:diff      # Alias for coverage:compare

# Security
task security:scan      # Comprehensive security scan (gosec + govulncheck + mod verify)
task vulncheck          # Check for known vulnerabilities in dependencies

# Local Development
task dev:local          # Run locally with fake-gcs-server
task dev:emulator       # Start only the fake-gcs-server emulator
task certs              # Generate TLS certificates for local dev (mkcert)

# End-to-End Testing
task e2e:test           # Build, start servers, run smoke tests, shut down
task e2e:serve          # Run e2e environment interactively
task e2e:smoke          # Run smoke tests against running server

# Container & CI
task build-container-image  # Build the container image
task push-container-image   # Push container image
task ci                     # Full CI pipeline (lint + test + coverage + build)
task release                # Release workflow

# Utilities
task clean              # Clean build artifacts and caches
task clean:cache        # Clean only Go caches (preserves artifacts)
task clean:build        # Clean only build artifacts (preserves caches)
task vet                # Run go vet static checks
task mod                # Download and tidy Go modules
task mod:check          # Check if go.mod and go.sum are in sync
task mod:update         # Upgrade all direct and indirect dependencies
task mod:verify         # Verify Go module integrity and checksums
task install-tools      # Install all development tools
task tag                # Create a signed annotated Git tag
```

### Manual Build (without Taskfile)

If you prefer not to use Taskfile, you can build manually:

```bash
# Check Go version (1.26+ required)
go version

# Download dependencies
go mod download
go mod tidy

# Run tests
go test -v ./...

# Install xcaddy
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Build Caddy with plugin
xcaddy build --with github.com/spuranam/caddy-fs-gcs@latest --output caddy-fs-gcs

# Test the binary
./caddy-fs-gcs version
```

## 🧪 **Testing**

```bash
task test               # Run all tests
task test:cover         # Tests with coverage
task coverage:report    # Per-package coverage summary
task e2e:test           # End-to-end tests
```

For testing details, fuzz tests, and coverage analysis, see [Developer Guide](docs/DEVELOPER-GUIDE.md).

## 🚀 **Deployment**

### Container

```bash
# Build container image
task build-container-image

# Push container image
task push-container-image
```

## 🤝 **Contributing**

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `task test`
5. Run linter: `task lint`
6. Run security scans: `task security:scan`
7. Submit a pull request

## 🆘 **Support**

- **Documentation**: [Complete Documentation](docs/README.md) for comprehensive guides
- **Issues**: [GitHub Issues](https://github.com/spuranam/caddy-fs-gcs/issues)
- **Discussions**: [GitHub Discussions](https://github.com/spuranam/caddy-fs-gcs/discussions)

## Caddy Plugin refs

- [Extending Caddy](https://caddyserver.com/docs/extending-caddy)
- [Plugin Development Tutorial](https://www.mintlify.com/caddyserver/caddy/dev/plugin-tutorial)
- [Writing a Caddy Plugin Part I](https://moebuta.org/posts/writing-a-caddy-plugin-part-i/)
- [Writing a Caddy Plugin Part II](https://moebuta.org/posts/writing-a-caddy-plugin-part-ii/)

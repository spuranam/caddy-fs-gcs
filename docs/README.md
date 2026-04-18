# Caddy GCS Proxy — Documentation

Caddy v2 plugin for serving files from Google Cloud Storage.
Implements Go's `fs.FS` / `fs.StatFS` interfaces so GCS buckets plug directly
into Caddy's built-in `file_server`.

## Quick Start

```bash
git clone https://github.com/spuranam/caddy-fs-gcs.git
cd caddy-fs-gcs
task build
cp refs/examples/Caddyfile.minimal Caddyfile   # edit with your bucket
./dist/caddy run --config Caddyfile
```

## Guides

| Guide                                       | Description                                                    |
| ------------------------------------------- | -------------------------------------------------------------- |
| [Configuration Reference](CONFIGURATION.md) | Filesystem directives, auth methods, observability, examples   |
| [Operations Guide](OPERATIONS.md)           | Performance tuning, monitoring, troubleshooting, health checks |
| [Developer Guide](DEVELOPER-GUIDE.md)       | Architecture, API reference, testing, contributing             |

## Additional Resources

| Resource                                             | Description                               |
| ---------------------------------------------------- | ----------------------------------------- |
| [Embedded Error Pages](../pkg/gcs/errorpages/caddy/) | Caddy-native branded error page templates |

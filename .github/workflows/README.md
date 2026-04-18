# GitHub Actions Workflows

This directory contains GitHub Actions workflows for automated CI/CD pipelines.

## Workflows

### 1. `test.yml` - CI

- **Triggers**: On pull requests and pushes to `main`
- **Jobs**:
  - **Lint**: Runs `task lint`, `go vet`, `go mod tidy` check, and `govulncheck`
  - **Test**: Runs tests with coverage check (85% threshold), uploads to Codecov
- **Features**:
  - Skips runs when only documentation files change
  - Uses Go version from `go.mod`
  - Caches Go modules for faster builds
  - Fork-safe: lint only runs on same-repo PRs

### 2. `benchmark.yml` - Benchmark Comparison

- **Triggers**: On pull requests to `main` (only when `.go`/`go.mod`/`go.sum` files change)
- **Jobs**:
  - Detects changed packages with benchmark tests
  - Runs benchmarks on both PR and base branch
  - Compares with `benchstat` and posts results as a PR comment
- **Features**:
  - Only benchmarks packages that actually changed
  - Updates existing PR comment instead of creating duplicates

### 3. `codeql.yml` - CodeQL Security Analysis

- **Triggers**: On push/PR to `main` and weekly schedule (Monday 06:00 UTC)
- **Jobs**:
  - Runs GitHub CodeQL analysis for Go
- **Features**:
  - Weekly scheduled scans catch vulnerabilities in dependencies

### 4. `release.yml` - Release

- **Triggers**: On version tags (`v*`)
- **Jobs**:
  - Verifies tests pass, then runs GoReleaser
  - Includes cosign signing and SBOM generation via Syft
- **Features**:
  - Uses goreleaser for multi-platform builds
  - Keyless signing via OIDC (Sigstore)

### 5. `dco.yml` - DCO Check

- **Triggers**: On pull requests to `main`
- **Jobs**:
  - Verifies all commits have `Signed-off-by` lines (Developer Certificate of Origin)

## Optional Secrets

Configure in GitHub repo settings > Secrets and variables > Actions:

| Secret | Purpose | Required |
| -------- | --------- | ---------- |
| `CODECOV_TOKEN` | Test coverage reporting | No |

## Workflow Permissions

- `test.yml`: Default (read repository contents)
- `benchmark.yml`: `contents: read`, `pull-requests: write` (for PR comments)
- `codeql.yml`: `actions: read`, `security-events: write`, `contents: read`
- `release.yml`: `contents: write`, `packages: write`, `id-token: write` (for cosign)
- `dco.yml`: Default (read repository contents)

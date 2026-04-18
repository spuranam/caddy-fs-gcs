package pkg

import "runtime/debug"

// Build-time variables injected via -ldflags:
//
//	-X github.com/spuranam/caddy-fs-gcs/pkg.Version=2.0.0
//	-X github.com/spuranam/caddy-fs-gcs/pkg.BuildTime=2026-04-14T00:00:00Z
//	-X github.com/spuranam/caddy-fs-gcs/pkg.Commit=abc123
//
// When building with xcaddy (which does not forward custom ldflags),
// the init function below falls back to Go's embedded VCS metadata
// (vcs.revision, vcs.time) that Go 1.18+ records automatically.
var (
	// Version is the caddy-fs-gcs module version, shared across all
	// sub-packages to avoid duplicated magic strings.
	Version = "1.0.0"

	// BuildTime is the UTC timestamp when the binary was built.
	BuildTime = "unknown"

	// Commit is the full git commit SHA the binary was built from.
	Commit = "unknown"
)

// Default values for build-time variables, used to detect whether
// ldflags were applied.
const (
	defaultVersion   = "1.0.0"
	defaultBuildTime = "unknown"
	defaultCommit    = "unknown"
)

// ServiceName is the canonical service name used in health endpoints,
// metrics labels, and validation responses.
const ServiceName = "caddy-fs-gcs"

func init() { //nolint:gochecknoinits // Populate build metadata from Go's embedded VCS info.
	initBuildInfo(debug.ReadBuildInfo)
}

// initBuildInfo populates Version, BuildTime, and Commit from Go's
// embedded build metadata when ldflags were not used.  The reader
// function is injected so tests can supply synthetic build info.
func initBuildInfo(reader func() (*debug.BuildInfo, bool)) {
	info, ok := reader()
	if !ok {
		return
	}

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == defaultCommit {
				Commit = s.Value
			}
		case "vcs.time":
			if BuildTime == defaultBuildTime {
				BuildTime = s.Value
			}
		}
	}

	if Version == defaultVersion {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			Version = v
		}
	}
}

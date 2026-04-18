package pkg

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitBuildInfo_VCSFallback(t *testing.T) {
	// Not parallel: mutates shared package-level vars.

	tests := []struct {
		name        string
		settings    []debug.BuildSetting
		mainVersion string
		initVersion string
		initCommit  string
		initBuild   string
		wantVersion string
		wantCommit  string
		wantBuild   string
	}{
		{
			name: "populates commit and build time from VCS",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123def456"},
				{Key: "vcs.time", Value: "2026-04-14T12:00:00Z"},
			},
			mainVersion: "(devel)",
			initVersion: defaultVersion,
			initCommit:  defaultCommit,
			initBuild:   defaultBuildTime,
			wantVersion: defaultVersion,
			wantCommit:  "abc123def456",
			wantBuild:   "2026-04-14T12:00:00Z",
		},
		{
			name: "populates version from module info",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
			},
			mainVersion: "v1.2.3",
			initVersion: defaultVersion,
			initCommit:  defaultCommit,
			initBuild:   defaultBuildTime,
			wantVersion: "v1.2.3",
			wantCommit:  "abc123",
			wantBuild:   defaultBuildTime,
		},
		{
			name:        "does not overwrite ldflags values",
			settings:    []debug.BuildSetting{{Key: "vcs.revision", Value: "should-not-use"}},
			mainVersion: "v9.9.9",
			initVersion: "2.0.0",
			initCommit:  "deadbeef",
			initBuild:   "2026-01-01T00:00:00Z",
			wantVersion: "2.0.0",
			wantCommit:  "deadbeef",
			wantBuild:   "2026-01-01T00:00:00Z",
		},
		{
			name:        "skips devel version",
			settings:    nil,
			mainVersion: "(devel)",
			initVersion: defaultVersion,
			initCommit:  defaultCommit,
			initBuild:   defaultBuildTime,
			wantVersion: defaultVersion,
			wantCommit:  defaultCommit,
			wantBuild:   defaultBuildTime,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: subtests mutate shared package-level vars.

			// Save and restore package-level vars.
			origV, origC, origB := Version, Commit, BuildTime
			t.Cleanup(func() {
				Version = origV
				Commit = origC
				BuildTime = origB
			})

			Version = tc.initVersion
			Commit = tc.initCommit
			BuildTime = tc.initBuild

			reader := func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					Main:     debug.Module{Version: tc.mainVersion},
					Settings: tc.settings,
				}, true
			}

			initBuildInfo(reader)

			assert.Equal(t, tc.wantVersion, Version)
			assert.Equal(t, tc.wantCommit, Commit)
			assert.Equal(t, tc.wantBuild, BuildTime)
		})
	}
}

func TestInitBuildInfo_NoBuildInfo(t *testing.T) {
	// Not parallel: mutates shared package-level vars.

	origV, origC, origB := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version = origV
		Commit = origC
		BuildTime = origB
	})

	Version = defaultVersion
	Commit = defaultCommit
	BuildTime = defaultBuildTime

	reader := func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	initBuildInfo(reader)

	assert.Equal(t, defaultVersion, Version)
	assert.Equal(t, defaultCommit, Commit)
	assert.Equal(t, defaultBuildTime, BuildTime)
}

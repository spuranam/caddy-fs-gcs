package fs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"google.golang.org/api/option"
)

func TestGCSFSCaddyModule(t *testing.T) {
	var g GCSFS
	info := g.CaddyModule()
	if info.ID != "caddy.fs.gcs" {
		t.Fatalf("ID = %q, want %q", info.ID, "caddy.fs.gcs")
	}
	m := info.New()
	if _, ok := m.(*GCSFS); !ok {
		t.Fatalf("New() returned %T, want *GCSFS", m)
	}
}

func TestCleanupNilClient(t *testing.T) {
	g := &GCSFS{}
	if err := g.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
}

func TestCleanupWithClient(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	client, err := storage.NewClient(context.Background(), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("storage.NewClient: %v", err)
	}
	g := &GCSFS{client: client}
	if err := g.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
}

func TestProvisionEmptyBucket(t *testing.T) {
	g := &GCSFS{}
	err := g.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "bucket_name is required") {
		t.Fatalf("expected bucket_name error, got: %v", err)
	}
}

func TestProvisionWithEmulator(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{BucketName: "test-bucket"}
	err := g.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	defer g.Cleanup()

	if g.StatFS == nil {
		t.Fatal("StatFS is nil after Provision")
	}
	if g.client == nil {
		t.Fatal("client is nil after Provision")
	}
}

func TestProvisionWithPathPrefix(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{
		BucketName: "test-bucket",
		PathPrefix: "/sites/prod/",
	}
	err := g.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	defer g.Cleanup()

	gfs, ok := g.StatFS.(*gcsFS)
	if !ok {
		t.Fatalf("StatFS is %T, want *gcsFS", g.StatFS)
	}
	if gfs.prefix != "sites/prod" {
		t.Errorf("prefix = %q, want %q", gfs.prefix, "sites/prod")
	}
}

func TestProvisionCacheDefaults(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{BucketName: "test-bucket"}
	if err := g.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	defer g.Cleanup()

	gfs := g.StatFS.(*gcsFS)
	if gfs.cache == nil {
		t.Fatal("cache should not be nil with default TTL")
	}
}

func TestProvisionCacheDisabled(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{
		BucketName: "test-bucket",
		CacheTTL:   caddy.Duration(-1),
	}
	if err := g.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	defer g.Cleanup()

	gfs := g.StatFS.(*gcsFS)
	if gfs.cache != nil {
		t.Fatal("cache should be nil when TTL is negative")
	}
}

func TestProvisionCacheDisabledViaCaddyfile(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{
		BucketName:  "test-bucket",
		CacheTTL:    0,
		cacheTTLSet: true, // simulates "cache_ttl 0" in Caddyfile
	}
	if err := g.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	defer g.Cleanup()

	gfs := g.StatFS.(*gcsFS)
	if gfs.cache != nil {
		t.Fatal("cache should be nil when cache_ttl 0 is explicitly set")
	}
}

func TestProvisionWithCredentialsConfig(t *testing.T) {
	// When STORAGE_EMULATOR_HOST is not set and CredentialsConfig is set,
	// the client should attempt to use WithAuthCredentialsFile(ExternalAccount).
	// We can't actually authenticate, but we verify Provision builds the option
	// without panicking. It will fail because the file doesn't exist.
	if os.Getenv("STORAGE_EMULATOR_HOST") != "" {
		t.Skip("STORAGE_EMULATOR_HOST is set")
	}
	g := &GCSFS{
		BucketName:        "test-bucket",
		CredentialsConfig: "/nonexistent/wif-config.json",
	}
	// This may succeed or fail depending on how the SDK handles the missing file.
	// The important thing is it doesn't panic and takes the CredentialsConfig branch.
	_ = g.Provision(caddy.Context{})
	g.Cleanup()
}

func TestProvisionWithCredentialsFile(t *testing.T) {
	if os.Getenv("STORAGE_EMULATOR_HOST") != "" {
		t.Skip("STORAGE_EMULATOR_HOST is set")
	}
	g := &GCSFS{
		BucketName:      "test-bucket",
		CredentialsFile: "/nonexistent/sa-key.json",
	}
	_ = g.Provision(caddy.Context{})
	g.Cleanup()
}

// ---------- UnmarshalCaddyfile tests ----------

func TestUnmarshalCaddyfileMinimal(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile() error: %v", err)
	}
	if g.BucketName != "my-bucket" {
		t.Errorf("BucketName = %q, want %q", g.BucketName, "my-bucket")
	}
}

func TestUnmarshalCaddyfileAllOptions(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
		path_prefix /sites/prod
		project_id my-project
		credentials_file /path/to/sa.json
		credentials_config /path/to/wif.json
		service_account sa@proj.iam.gserviceaccount.com
		use_workload_identity
		cache_ttl 10m
		cache_max_entries 5000
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile() error: %v", err)
	}
	if g.BucketName != "my-bucket" {
		t.Errorf("BucketName = %q", g.BucketName)
	}
	if g.PathPrefix != "/sites/prod" {
		t.Errorf("PathPrefix = %q", g.PathPrefix)
	}
	if g.ProjectID != "my-project" {
		t.Errorf("ProjectID = %q", g.ProjectID)
	}
	if g.CredentialsFile != "/path/to/sa.json" {
		t.Errorf("CredentialsFile = %q", g.CredentialsFile)
	}
	if g.CredentialsConfig != "/path/to/wif.json" {
		t.Errorf("CredentialsConfig = %q", g.CredentialsConfig)
	}
	if g.ServiceAccount != "sa@proj.iam.gserviceaccount.com" {
		t.Errorf("ServiceAccount = %q", g.ServiceAccount)
	}
	if !g.UseWorkloadIdentity {
		t.Error("UseWorkloadIdentity should be true")
	}
	if g.CacheTTL == 0 {
		t.Error("CacheTTL should be set")
	}
	if g.CacheMaxEntries != 5000 {
		t.Errorf("CacheMaxEntries = %d, want 5000", g.CacheMaxEntries)
	}
}

func TestUnmarshalCaddyfileMissingBucket(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		path_prefix /sites
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for missing bucket_name")
	}
}

func TestUnmarshalCaddyfileInvalidDirective(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
		unknown_option value
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for unknown directive")
	}
}

func TestUnmarshalCaddyfileInvalidCacheTTL(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
		cache_ttl not-a-duration
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for invalid cache_ttl")
	}
}

func TestUnmarshalCaddyfileInvalidCacheMaxEntries(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
		cache_max_entries abc
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for invalid cache_max_entries")
	}
}

func TestUnmarshalCaddyfileNegativeCacheMaxEntries(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
		cache_max_entries -1
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for negative cache_max_entries")
	}
}

func TestUnmarshalCaddyfileZeroCacheMaxEntries(t *testing.T) {
	d := caddyfile.NewTestDispenser(`gcs {
		bucket_name my-bucket
		cache_max_entries 0
	}`)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for zero cache_max_entries")
	}
}

func TestGCSFSInterfaceAssertions(t *testing.T) {
	var g GCSFS
	_ = fs.StatFS(&g)
	_ = caddy.Module(&g)
	_ = caddy.Provisioner(&g)
	_ = caddy.CleanerUpper(&g)
	_ = caddyfile.Unmarshaler(&g)
}

// ---------- P2: UsagePool tests ----------

func TestClientPoolKey(t *testing.T) {
	tests := []struct {
		name    string
		f       GCSFS
		envHost string
		wantKey string
	}{
		{
			name:    "emulator",
			f:       GCSFS{BucketName: "b"},
			envHost: "localhost:4443",
			wantKey: "emulator:localhost:4443",
		},
		{
			name:    "wif_credentials",
			f:       GCSFS{BucketName: "b", CredentialsConfig: "/path/wif.json"},
			wantKey: "wif:/path/wif.json",
		},
		{
			name:    "sa_credentials",
			f:       GCSFS{BucketName: "b", CredentialsFile: "/path/sa.json"},
			wantKey: "sa:/path/sa.json",
		},
		{
			name:    "adc_default",
			f:       GCSFS{BucketName: "b"},
			wantKey: "adc",
		},
		{
			name:    "adc_with_impersonation",
			f:       GCSFS{BucketName: "b", ServiceAccount: "sa@proj.iam.gserviceaccount.com"},
			wantKey: "adc|impersonate:sa@proj.iam.gserviceaccount.com",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envHost != "" {
				t.Setenv("STORAGE_EMULATOR_HOST", tc.envHost)
			} else {
				t.Setenv("STORAGE_EMULATOR_HOST", "")
			}
			got := tc.f.clientPoolKey()
			if got != tc.wantKey {
				t.Errorf("clientPoolKey() = %q, want %q", got, tc.wantKey)
			}
		})
	}
}

func TestProvisionPoolReuse(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")

	g1 := &GCSFS{BucketName: "bucket1"}
	if err := g1.Provision(caddy.Context{}); err != nil {
		t.Fatalf("g1.Provision: %v", err)
	}
	defer g1.Cleanup() //nolint:errcheck

	g2 := &GCSFS{BucketName: "bucket2"}
	if err := g2.Provision(caddy.Context{}); err != nil {
		t.Fatalf("g2.Provision: %v", err)
	}
	defer g2.Cleanup() //nolint:errcheck

	// Same emulator credentials → same pool key.
	if g1.poolKey != g2.poolKey {
		t.Errorf("pool keys differ: %q vs %q", g1.poolKey, g2.poolKey)
	}

	// Both share the same underlying client.
	if g1.client != g2.client {
		t.Error("expected same *storage.Client for matching credentials")
	}

	// Pool should track 2 references.
	refs, ok := gcsClients.References(g1.poolKey)
	if !ok {
		t.Fatal("pool key not found")
	}
	if refs != 2 {
		t.Errorf("references = %d, want 2", refs)
	}
}

// ---------- P3: Duplicate directive tests ----------

func TestUnmarshalCaddyfileDuplicateDirective(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "duplicate_bucket_name",
			input: `gcs {
				bucket_name first
				bucket_name second
			}`,
		},
		{
			name: "duplicate_path_prefix",
			input: `gcs {
				bucket_name my-bucket
				path_prefix /a
				path_prefix /b
			}`,
		},
		{
			name: "duplicate_cache_ttl",
			input: `gcs {
				bucket_name my-bucket
				cache_ttl 5m
				cache_ttl 10m
			}`,
		},
		{
			name: "duplicate_credentials_file",
			input: `gcs {
				bucket_name my-bucket
				credentials_file /a.json
				credentials_file /b.json
			}`,
		},
		{
			name: "duplicate_use_workload_identity",
			input: `gcs {
				bucket_name my-bucket
				use_workload_identity
				use_workload_identity
			}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(tc.input)
			var g GCSFS
			if err := g.UnmarshalCaddyfile(d); err == nil {
				t.Fatal("expected error for duplicate directive")
			}
		})
	}
}

// ---------- P5: CacheStats tests ----------

func TestCacheStats(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{BucketName: "test-bucket"}
	if err := g.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	defer g.Cleanup() //nolint:errcheck

	hits, misses := g.CacheStats()
	if hits != 0 || misses != 0 {
		t.Errorf("initial stats = (%d, %d), want (0, 0)", hits, misses)
	}
}

func TestCacheStatsDisabled(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "localhost:1")
	g := &GCSFS{BucketName: "test-bucket", CacheTTL: caddy.Duration(-1)}
	if err := g.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	defer g.Cleanup() //nolint:errcheck

	hits, misses := g.CacheStats()
	if hits != 0 || misses != 0 {
		t.Errorf("disabled cache stats = (%d, %d), want (0, 0)", hits, misses)
	}
}

func TestLoadEventsAppNilContext(t *testing.T) {
	// Zero-value context has no running config — should return nil, not panic.
	app := loadEventsApp(caddy.Context{})
	if app != nil {
		t.Error("expected nil events app for zero-value context")
	}
}

// ---------- UnmarshalCaddyfile missing-argument error branches ----------

func TestUnmarshalCaddyfileMissingArgs(t *testing.T) {
	// Each directive that takes a value should fail when the value is missing.
	// This covers the `return d.ArgErr()` branches for each `case`.
	tests := []struct {
		name  string
		input string
	}{
		{"bucket_name no arg", "gcs {\n\tbucket_name\n}"},
		{"path_prefix no arg", "gcs {\n\tbucket_name b\n\tpath_prefix\n}"},
		{"project_id no arg", "gcs {\n\tbucket_name b\n\tproject_id\n}"},
		{"credentials_file no arg", "gcs {\n\tbucket_name b\n\tcredentials_file\n}"},
		{"credentials_config no arg", "gcs {\n\tbucket_name b\n\tcredentials_config\n}"},
		{"service_account no arg", "gcs {\n\tbucket_name b\n\tservice_account\n}"},
		{"cache_ttl no arg", "gcs {\n\tbucket_name b\n\tcache_ttl\n}"},
		{"cache_max_entries no arg", "gcs {\n\tbucket_name b\n\tcache_max_entries\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(tt.input)
			var g GCSFS
			if err := g.UnmarshalCaddyfile(d); err == nil {
				t.Fatalf("expected error for %q", tt.name)
			}
		})
	}
}

func TestUnmarshalCaddyfileEmptyDispenser(t *testing.T) {
	d := caddyfile.NewTestDispenser(``)
	var g GCSFS
	if err := g.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for empty dispenser")
	}
}

func TestClassifyGCSError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, "none"},
		{"object not exist", storage.ErrObjectNotExist, "not_found"},
		{"bucket not exist", storage.ErrBucketNotExist, "bucket_not_found"},
		{"deadline exceeded", context.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, "canceled"},
		{"wrapped deadline", fmt.Errorf("op failed: %w", context.DeadlineExceeded), "timeout"},
		{"unknown error", errors.New("something else"), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyGCSError(tt.err)
			if got != tt.want {
				t.Errorf("classifyGCSError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestGCSFSMetricsCallbacks(t *testing.T) {
	t.Parallel()

	t.Run("nil callbacks do not panic", func(t *testing.T) {
		t.Parallel()
		fsys := &gcsFS{} // all callbacks nil
		fsys.recordCacheHit()
		fsys.recordCacheMiss()
		fsys.recordGCSOp("attrs", time.Now(), nil)
		fsys.recordStreamBytes(1024)
	})

	t.Run("callbacks are invoked", func(t *testing.T) {
		t.Parallel()
		var cacheHits, cacheMisses int
		var ops []string
		var totalBytes int64

		fsys := &gcsFS{
			onCacheHit:  func() { cacheHits++ },
			onCacheMiss: func() { cacheMisses++ },
			onGCSOp: func(op string, dur time.Duration, err error) {
				ops = append(ops, op)
			},
			onStreamBytes: func(n int64) { totalBytes += n },
		}

		fsys.recordCacheHit()
		fsys.recordCacheHit()
		fsys.recordCacheMiss()
		fsys.recordGCSOp("attrs", time.Now().Add(-time.Millisecond), nil)
		fsys.recordGCSOp("read", time.Now().Add(-time.Millisecond), errors.New("fail"))
		fsys.recordStreamBytes(512)
		fsys.recordStreamBytes(1024)
		fsys.recordStreamBytes(0) // zero bytes should be ignored

		if cacheHits != 2 {
			t.Errorf("cacheHits = %d, want 2", cacheHits)
		}
		if cacheMisses != 1 {
			t.Errorf("cacheMisses = %d, want 1", cacheMisses)
		}
		if len(ops) != 2 || ops[0] != "attrs" || ops[1] != "read" {
			t.Errorf("ops = %v, want [attrs read]", ops)
		}
		if totalBytes != 1536 {
			t.Errorf("totalBytes = %d, want 1536", totalBytes)
		}
	})
}

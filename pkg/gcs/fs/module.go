package fs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyevents"
	observability "github.com/spuranam/caddy-fs-gcs/pkg/observability/metrics"
	"go.uber.org/zap"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/option"
)

func init() { //nolint:gochecknoinits // Caddy module registration requires init().
	caddy.RegisterModule((*GCSFS)(nil))
}

// Default values for GCSFS configuration.
const (
	defaultCacheTTL        = 5 * time.Minute
	defaultCacheMaxEntries = 10000
	gcsReadOnlyScope       = "https://www.googleapis.com/auth/devstorage.read_only"
)

// gcsClients pools GCS clients across config reloads. Keyed by credential
// fingerprint so that GCSFS instances with the same credentials share a
// single *storage.Client. The client is closed (via Destruct) only when
// the last reference is removed.
var gcsClients = caddy.NewUsagePool()

// clientDestructor wraps *storage.Client so it satisfies caddy.Destructor.
type clientDestructor struct {
	client *storage.Client
}

func (d *clientDestructor) Destruct() error {
	return d.client.Close()
}

// GCSFS is a Caddy virtual filesystem module for Google Cloud Storage.
// It implements fs.StatFS so it can be used with Caddy's built-in
// file_server via the filesystem global option:
//
//	{
//	    filesystem my-gcs gcs {
//	        bucket_name my-bucket
//	    }
//	}
//
//	example.com {
//	    file_server { fs my-gcs }
//	}
type GCSFS struct {
	fs.StatFS `json:"-"`

	// BucketName is the name of the Google Cloud Storage bucket.
	BucketName string `json:"bucket_name,omitempty"`

	// PathPrefix is an optional prefix prepended to all object lookups.
	// For example, "sites/prod" causes Open("index.html") to read
	// "sites/prod/index.html" from the bucket.
	PathPrefix string `json:"path_prefix,omitempty"`

	// ProjectID is the Google Cloud project ID (used for billing/quota).
	ProjectID string `json:"project_id,omitempty"`

	// CredentialsFile is the path to a service account key JSON file.
	CredentialsFile string `json:"credentials_file,omitempty"`

	// CredentialsConfig is the path to an external account (WIF)
	// credential configuration JSON file.
	CredentialsConfig string `json:"credentials_config,omitempty"`

	// ServiceAccount is the service account email to impersonate.
	ServiceAccount string `json:"service_account,omitempty"`

	// UseWorkloadIdentity enables GKE/GCE Workload Identity (ADC).
	UseWorkloadIdentity bool `json:"use_workload_identity,omitempty"`

	// CacheTTL is how long cached object attributes remain valid.
	// Default: 5m. Set to "0" to disable the attribute cache.
	CacheTTL caddy.Duration `json:"cache_ttl,omitempty"`

	// cacheTTLSet tracks whether cache_ttl was explicitly set in the Caddyfile,
	// allowing us to distinguish "not set" (use default 5m) from "set to 0"
	// (disable cache).
	cacheTTLSet bool

	// CacheMaxEntries is the maximum number of cached attribute entries.
	// Default: 10000.
	CacheMaxEntries int `json:"cache_max_entries,omitempty"`

	// cacheMaxEntriesSet tracks whether cache_max_entries was explicitly set
	// in the Caddyfile, allowing us to distinguish "not set" (use default
	// 10000) from "set to 0" (use default — 0 is not a valid size).
	cacheMaxEntriesSet bool

	client  *storage.Client
	poolKey string // key in the gcsClients UsagePool
}

// Compile-time interface assertions.
var (
	_ fs.StatFS             = (*GCSFS)(nil)
	_ caddy.Module          = (*GCSFS)(nil)
	_ caddy.Provisioner     = (*GCSFS)(nil)
	_ caddy.CleanerUpper    = (*GCSFS)(nil)
	_ caddyfile.Unmarshaler = (*GCSFS)(nil)
)

// CaddyModule returns the Caddy module information.
func (*GCSFS) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.fs.gcs",
		New: func() caddy.Module { return new(GCSFS) },
	}
}

// Provision sets up the GCS client and creates the underlying fs.StatFS.
func (f *GCSFS) Provision(ctx caddy.Context) error {
	logger := ctx.Logger()

	// Expand Caddy placeholders so users can write
	// bucket_name {env.GCS_BUCKET} etc. in the filesystem block.
	repl := caddy.NewReplacer()
	f.BucketName = repl.ReplaceAll(f.BucketName, "")
	f.PathPrefix = repl.ReplaceAll(f.PathPrefix, "")
	f.CredentialsFile = repl.ReplaceAll(f.CredentialsFile, "")
	f.CredentialsConfig = repl.ReplaceAll(f.CredentialsConfig, "")
	f.ServiceAccount = repl.ReplaceAll(f.ServiceAccount, "")

	// Fall back to well-known environment variables when config is empty.
	if f.BucketName == "" {
		f.BucketName = os.Getenv("GCS_BUCKET")
	}
	if f.CredentialsFile == "" {
		f.CredentialsFile = os.Getenv("GCS_CREDENTIALS_FILE")
	}

	if f.BucketName == "" {
		return fmt.Errorf("caddy.fs.gcs: bucket_name is required")
	}

	// Use the client pool to share GCS clients across config reloads.
	key := f.clientPoolKey()
	val, _, err := gcsClients.LoadOrNew(key, func() (caddy.Destructor, error) {
		var opts []option.ClientOption
		if os.Getenv("STORAGE_EMULATOR_HOST") != "" {
			opts = append(opts, option.WithoutAuthentication())
		} else if f.CredentialsConfig != "" {
			opts = append(opts, option.WithAuthCredentialsFile(option.ExternalAccount, f.CredentialsConfig))
		} else if f.CredentialsFile != "" {
			opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, f.CredentialsFile))
		} else if f.UseWorkloadIdentity {
			// Explicitly use ADC — no additional options needed, but log it.
			logger.Info("using workload identity (ADC) for GCS authentication")
		}

		// Service account impersonation: if configured, create impersonated
		// credentials and pass them as a client option. This works with any
		// base credential (ADC, SA key, WIF).
		if f.ServiceAccount != "" && os.Getenv("STORAGE_EMULATOR_HOST") == "" {
			ts, impErr := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
				TargetPrincipal: f.ServiceAccount,
				Scopes:          []string{gcsReadOnlyScope},
			})
			if impErr != nil {
				return nil, fmt.Errorf("impersonate service account %q: %w", f.ServiceAccount, impErr)
			}
			opts = append(opts, option.WithTokenSource(ts))
			logger.Info("impersonating service account for GCS",
				zap.String("service_account", f.ServiceAccount))
		}

		client, clientErr := storage.NewClient(ctx, opts...)
		if clientErr != nil {
			return nil, clientErr
		}
		return &clientDestructor{client: client}, nil
	})
	if err != nil {
		return err
	}
	f.poolKey = key
	f.client = val.(*clientDestructor).client

	// Attribute cache — default 5m / 10000 entries.
	// When cacheTTLSet is true and CacheTTL == 0, the user explicitly
	// disabled the cache via "cache_ttl 0".
	ttl := time.Duration(f.CacheTTL)
	if !f.cacheTTLSet && f.CacheTTL == 0 {
		ttl = defaultCacheTTL
	}
	maxEntries := f.CacheMaxEntries
	if !f.cacheMaxEntriesSet && maxEntries == 0 {
		maxEntries = defaultCacheMaxEntries
	}

	// Use context.Background() for the filesystem's stored context rather
	// than the provisioning context, which is cancelled on config reload.
	// Per-request contexts can be injected via gcsFS.WithContext().
	fsys := newGCSFS(context.Background(), f.client, f.BucketName)

	if ttl > 0 {
		fsys.cache = newAttrCache(ttl, maxEntries)
	}

	// Wire up event emission (non-fatal if events app is unavailable).
	if events := loadEventsApp(ctx); events != nil {
		caddyCtx := ctx
		fsys.onEvent = func(name string, data map[string]any) {
			events.Emit(caddyCtx, name, data)
		}
	}

	// Wire metrics callbacks so GCS operations, cache hits/misses, and
	// streaming bytes are recorded via the OTel/Prometheus instruments.
	bucket := f.BucketName
	fsys.onCacheHit = func() {
		observability.RecordCacheHit(context.Background(), bucket, "attr")
	}
	fsys.onCacheMiss = func() {
		observability.RecordCacheMiss(context.Background(), bucket, "attr")
	}
	fsys.onGCSOp = func(op string, dur time.Duration, err error) {
		status := "success"
		if err != nil {
			status = "error"
			observability.RecordGCSError(context.Background(), op, bucket, classifyGCSError(err))
		}
		observability.RecordGCSOperation(context.Background(), op, bucket, status, dur)
	}
	fsys.onStreamBytes = func(n int64) {
		observability.RecordStreamingBytes(context.Background(), bucket, n)
	}

	if f.PathPrefix != "" {
		prefix := strings.TrimPrefix(f.PathPrefix, "/")
		prefix = strings.TrimSuffix(prefix, "/")
		sub, subErr := fsys.Sub(prefix)
		if subErr != nil {
			_, _ = gcsClients.Delete(f.poolKey) // best-effort cleanup on Sub() failure
			f.poolKey = ""
			return subErr
		}
		f.StatFS = sub.(*gcsFS)
	} else {
		f.StatFS = fsys
	}

	return nil
}

// Cleanup releases the pooled GCS client reference. The client is
// closed only when all GCSFS instances sharing it have been cleaned up.
func (f *GCSFS) Cleanup() error {
	// Stop the cache clock ticker.
	if gfs, ok := f.StatFS.(*gcsFS); ok && gfs.cache != nil {
		gfs.cache.stop()
	}
	if f.poolKey != "" {
		_, err := gcsClients.Delete(f.poolKey)
		return err
	}
	// Fallback for clients not managed by the pool.
	if f.client != nil {
		return f.client.Close()
	}
	return nil
}

// CacheStats returns cumulative cache hit and miss counts.
// Returns (0, 0) if the attribute cache is disabled.
func (f *GCSFS) CacheStats() (hits, misses int64) {
	if gfs, ok := f.StatFS.(*gcsFS); ok && gfs.cache != nil {
		return gfs.cache.stats()
	}
	return 0, 0
}

// clientPoolKey returns a key that uniquely identifies the GCS client
// configuration. Instances with the same credentials share a single
// *storage.Client via the UsagePool.
func (f *GCSFS) clientPoolKey() string {
	if emulatorHost := os.Getenv("STORAGE_EMULATOR_HOST"); emulatorHost != "" {
		return "emulator:" + emulatorHost
	}
	if f.CredentialsConfig != "" {
		return "wif:" + f.CredentialsConfig
	}
	if f.CredentialsFile != "" {
		return "sa:" + f.CredentialsFile
	}
	key := "adc"
	if f.ServiceAccount != "" {
		key += "|impersonate:" + f.ServiceAccount
	}
	return key
}

// loadEventsApp safely loads the events app from a Caddy context.
// Returns nil if the events app is unavailable (e.g., during unit tests
// where caddy.Context{} has no running configuration).
func loadEventsApp(ctx caddy.Context) (app *caddyevents.App) {
	// Guard against zero-value caddy.Context which panics on App().
	defer func() {
		if r := recover(); r != nil {
			// Log at debug level — this is expected during unit tests with
			// a zero-value caddy.Context; not an error in production.
			if logger := ctx.Logger(); logger != nil {
				logger.Debug("events app unavailable", zap.Any("recover", r))
			}
			app = nil
		}
	}()
	eventsAppAny, err := ctx.App("events")
	if err != nil {
		return nil
	}
	app, _ = eventsAppAny.(*caddyevents.App)
	return app
}

// classifyGCSError returns a short label for a GCS error, suitable for
// use as a Prometheus metric attribute value.
func classifyGCSError(err error) string {
	if err == nil {
		return "none"
	}
	switch {
	case errors.Is(err, storage.ErrObjectNotExist):
		return "not_found"
	case errors.Is(err, storage.ErrBucketNotExist):
		return "bucket_not_found"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "unknown"
	}
}

// UnmarshalCaddyfile parses:
//
//	gcs {
//	    bucket_name         <name>
//	    path_prefix         <prefix>
//	    project_id          <id>
//	    credentials_file    <path>
//	    credentials_config  <path>
//	    service_account     <email>
//	    use_workload_identity
//	}
func (f *GCSFS) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	if !d.Next() {
		return d.ArgErr()
	}

	seen := make(map[string]bool)

	for nesting := d.Nesting(); d.NextBlock(nesting); {
		directive := d.Val()
		if seen[directive] {
			return d.Errf("duplicate directive: %s", directive)
		}
		seen[directive] = true

		switch directive {
		case "bucket_name":
			if !d.AllArgs(&f.BucketName) {
				return d.ArgErr()
			}
		case "path_prefix":
			if !d.AllArgs(&f.PathPrefix) {
				return d.ArgErr()
			}
		case "project_id":
			if !d.AllArgs(&f.ProjectID) {
				return d.ArgErr()
			}
		case "credentials_file":
			if !d.AllArgs(&f.CredentialsFile) {
				return d.ArgErr()
			}
		case "credentials_config":
			if !d.AllArgs(&f.CredentialsConfig) {
				return d.ArgErr()
			}
		case "service_account":
			if !d.AllArgs(&f.ServiceAccount) {
				return d.ArgErr()
			}
		case "use_workload_identity":
			f.UseWorkloadIdentity = true
		case "cache_ttl":
			var raw string
			if !d.AllArgs(&raw) {
				return d.ArgErr()
			}
			dur, err := caddy.ParseDuration(raw)
			if err != nil {
				return d.Errf("invalid cache_ttl %q: %v", raw, err)
			}
			f.CacheTTL = caddy.Duration(dur)
			f.cacheTTLSet = true
		case "cache_max_entries":
			var raw string
			if !d.AllArgs(&raw) {
				return d.ArgErr()
			}
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				return d.Errf("invalid cache_max_entries %q: must be a positive integer", raw)
			}
			f.CacheMaxEntries = n
			f.cacheMaxEntriesSet = true
		default:
			return d.Errf("%s not a valid caddy.fs.gcs option", d.Val())
		}
	}

	if f.BucketName == "" {
		return d.Err("bucket_name is required")
	}

	return nil
}

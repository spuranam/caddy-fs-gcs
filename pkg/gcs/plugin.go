package gcs

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	_ "github.com/spuranam/caddy-fs-gcs/pkg/gcs/errorpages" // registers caddy.fs.error_pages
	_ "github.com/spuranam/caddy-fs-gcs/pkg/gcs/fs"         // registers caddy.fs.gcs
	"github.com/spuranam/caddy-fs-gcs/pkg/observability/health"
	observability "github.com/spuranam/caddy-fs-gcs/pkg/observability/metrics"
	"github.com/spuranam/caddy-fs-gcs/pkg/validation"
)

//nolint:gochecknoinits // init is required for Caddy module registration
func init() {
	// Register observability modules
	caddy.RegisterModule(&observability.MetricsHandler{})
	caddy.RegisterModule((*observability.PrometheusEndpointHandler)(nil))
	caddy.RegisterModule((*health.HealthEndpointHandler)(nil))

	// Register validation modules
	caddy.RegisterModule((*validation.ValidationEndpointHandler)(nil))

	// Register Caddyfile directives so modules are usable from Caddyfile syntax.
	httpcaddyfile.RegisterHandlerDirective("gcs_metrics", observability.ParseCaddyfileMetrics)
	httpcaddyfile.RegisterHandlerDirective("prometheus", observability.ParseCaddyfilePrometheus)
	httpcaddyfile.RegisterHandlerDirective("gcs_health", health.ParseCaddyfileHealth)
	httpcaddyfile.RegisterHandlerDirective("config_validation", validation.ParseCaddyfileValidation)

	// Directive ordering — gcs_metrics wraps all handlers (early, like
	// Caddy's built-in metrics); the endpoint directives run before
	// file_server so they can intercept their specific paths.
	httpcaddyfile.RegisterDirectiveOrder("gcs_metrics", httpcaddyfile.After, "metrics")
	httpcaddyfile.RegisterDirectiveOrder("gcs_health", httpcaddyfile.Before, "respond")
	httpcaddyfile.RegisterDirectiveOrder("prometheus", httpcaddyfile.Before, "respond")
	httpcaddyfile.RegisterDirectiveOrder("config_validation", httpcaddyfile.Before, "respond")
}

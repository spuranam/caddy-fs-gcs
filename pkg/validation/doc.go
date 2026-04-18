// Package validation provides runtime configuration validation for caddy-fs-gcs.
//
// The ValidationEndpointHandler exposes HTTP endpoints for validating
// GCS configuration (bucket names, URLs, project IDs) at runtime. It
// registers as http.handlers.config_validation and is intended for
// operator use behind the local_only access gate.
//
// The ConfigValidator implements a rule-based validation engine with
// registered rules for common GCS configuration fields.
package validation

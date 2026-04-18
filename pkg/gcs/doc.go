// Package gcs is the top-level registration package for the Caddy GCS Proxy plugin.
//
// It registers all Caddy modules (metrics, health, validation, error pages, and
// the GCS filesystem) via init() and wires up the corresponding Caddyfile
// directives so they can be used in a Caddyfile configuration.
package gcs

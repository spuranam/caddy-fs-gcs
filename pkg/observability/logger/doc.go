// Package logger provides structured logging via slog with OpenTelemetry trace
// correlation. It follows the same patterns as the jqapi logger package, adapted
// for use in a Caddy v2 plugin.
//
// Usage:
//
//	logger.Init(logger.LevelInfo, true)           // bootstrap JSON logging
//	logger.Info("server started", "port", 8080)   // global logging
//	logger.InfoContext(ctx, "request", "path", p) // context-aware with trace IDs
package logger

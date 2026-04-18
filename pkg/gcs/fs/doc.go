// Package fs implements a read-only [fs.FS] backed by Google Cloud Storage.
//
// It is adapted from golang.org/x/build/internal/gcsfs (BSD-3-Clause) with the
// following modifications for caddy-fs-gcs:
//   - Write support removed
//   - [io.ReadSeeker] added via GCS range reads
//   - [fs.StatFS] support added
//   - GCS errors translated to standard [fs.PathError] values
//   - Optional attribute caching for reduced GCS API calls
//
// The filesystem is registered as a Caddy module (caddy.fs.gcs) and can be used
// with Caddy's file_server directive.
package fs

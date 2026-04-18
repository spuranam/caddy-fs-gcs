package errorpages

import (
	"embed"
	"io/fs"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

//go:embed caddy/*.html
var embeddedPages embed.FS

func init() { //nolint:gochecknoinits // Caddy module registration requires init().
	caddy.RegisterModule((*EmbeddedErrorPages)(nil))
}

// EmbeddedErrorPages is a Caddy filesystem module that serves branded error
// pages (404, 403, 500, default) embedded in the plugin binary. It registers
// as caddy.fs.error_pages and is intended for use with handle_errors:
//
//	{
//	    filesystem error-pages error_pages
//	}
//
//	handle_errors {
//	    @404 expression {err.status_code} == 404
//	    handle @404 {
//	        templates
//	        rewrite * /404.html
//	        file_server { fs error-pages }
//	    }
//	}
//
// GCS bucket error pages take priority — simply try the GCS filesystem
// first and fall back to embedded pages.
type EmbeddedErrorPages struct {
	fs.FS `json:"-"`
}

// Compile-time interface assertions.
var (
	_ fs.FS                 = (*EmbeddedErrorPages)(nil)
	_ caddy.Module          = (*EmbeddedErrorPages)(nil)
	_ caddy.Provisioner     = (*EmbeddedErrorPages)(nil)
	_ caddyfile.Unmarshaler = (*EmbeddedErrorPages)(nil)
)

// CaddyModule returns the Caddy module information.
func (*EmbeddedErrorPages) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.fs.error_pages",
		New: func() caddy.Module { return new(EmbeddedErrorPages) },
	}
}

// Provision strips the "caddy/" prefix from the embedded FS so files are
// accessed as /404.html, not /caddy/404.html.
func (e *EmbeddedErrorPages) Provision(_ caddy.Context) error {
	sub, err := fs.Sub(embeddedPages, "caddy")
	if err != nil {
		return err
	}
	e.FS = sub
	return nil
}

// UnmarshalCaddyfile accepts an empty block:
//
//	error_pages
func (e *EmbeddedErrorPages) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	if !d.Next() {
		return d.ArgErr()
	}
	// No sub-directives — the embedded pages are fixed.
	if d.NextBlock(d.Nesting()) {
		return d.Errf("caddy.fs.error_pages does not accept any configuration")
	}
	return nil
}

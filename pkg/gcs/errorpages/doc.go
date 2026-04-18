// Package errorpages provides branded error pages for the caddy-fs-gcs plugin.
//
// The EmbeddedErrorPages Caddy filesystem module serves pre-built HTML
// error pages (404, 403, 500, default) embedded in the plugin binary.
// Use with Caddy's handle_errors directive:
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
package errorpages

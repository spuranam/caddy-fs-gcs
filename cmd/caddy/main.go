package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	// Import standard Caddy modules
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	// Import our GCS plugin
	_ "github.com/spuranam/caddy-fs-gcs/pkg/gcs"
)

func main() {
	caddycmd.Main()
}

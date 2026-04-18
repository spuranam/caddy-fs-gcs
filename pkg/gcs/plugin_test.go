package gcs

import (
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleRegistration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		moduleID string
	}{
		{"gcs_metrics handler", "http.handlers.gcs_metrics"},
		{"prometheus handler", "http.handlers.prometheus"},
		{"gcs_health handler", "http.handlers.health"},
		{"config_validation handler", "http.handlers.config_validation"},
		{"gcs filesystem", "caddy.fs.gcs"},
		{"error_pages filesystem", "caddy.fs.error_pages"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mod, err := caddy.GetModule(tt.moduleID)
			require.NoError(t, err, "module %q should be registered", tt.moduleID)
			assert.NotNil(t, mod.New, "module %q New function should not be nil", tt.moduleID)

			instance := mod.New()
			assert.NotNil(t, instance, "module %q New() should return non-nil", tt.moduleID)
		})
	}
}

package validation

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConfigValidator(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(nil)
	assert.NotNil(t, validator)
	assert.NotEmpty(t, validator.rules)
}

func TestConfigValidator_ValidateField(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid_string", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_field", "valid_string", "string")
		assert.NoError(t, err)
	})

	t.Run("invalid_string", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_field", "", "string")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "string cannot be empty")
	})

	t.Run("valid_url", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_url", "https://example.com", "url")
		assert.NoError(t, err)
	})

	t.Run("invalid_url", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_url", "not-a-url", "url")
		assert.Error(t, err, "bare string without scheme/host should be rejected")
	})

	t.Run("valid_port", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_port", 8080, "port")
		assert.NoError(t, err)
	})

	t.Run("invalid_port", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_port", 70000, "port")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "port must be between 1 and 65535")
	})

	t.Run("valid_duration", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_duration", "30s", "duration")
		assert.NoError(t, err)
	})

	t.Run("invalid_duration", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_duration", "invalid", "duration")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid duration format")
	})

	t.Run("valid_positive_int", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_int", 42, "positive_int")
		assert.NoError(t, err)
	})

	t.Run("invalid_positive_int", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_int", -1, "positive_int")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "value must be positive")
	})

	t.Run("valid_boolean", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_bool", true, "boolean")
		assert.NoError(t, err)
	})

	t.Run("invalid_boolean", func(t *testing.T) {
		t.Parallel()
		err := validator.ValidateField("test_bool", "maybe", "boolean")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid boolean value")
	})
}

func TestConfigValidator_ValidateConfig(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid_config", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"name":    "test",
			"port":    8080,
			"enabled": true,
		}
		rules := map[string]string{
			"name":    "string",
			"port":    "port",
			"enabled": "boolean",
		}

		result := validator.ValidateConfig(config, rules)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid_config", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"name":    "",
			"port":    -1,
			"enabled": "maybe",
		}
		rules := map[string]string{
			"name":    "string",
			"port":    "port",
			"enabled": "boolean",
		}

		result := validator.ValidateConfig(config, rules)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
		assert.Len(t, result.Errors, 3)
	})

	t.Run("missing_required_field", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"name": "test",
		}
		rules := map[string]string{
			"name": "string",
			"port": "port",
		}

		result := validator.ValidateConfig(config, rules)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0].Message, "required field is missing")
	})
}

func TestConfigValidator_ValidateHTTPConfig(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid_http_config", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"listen_address":  "0.0.0.0",
			"port":            8080,
			"read_timeout":    "30s",
			"write_timeout":   "30s",
			"idle_timeout":    "60s",
			"max_connections": 1000,
		}

		result := validator.ValidateHTTPConfig(config)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid_http_config", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"listen_address":  "",
			"port":            -1,
			"read_timeout":    "invalid",
			"max_connections": -1,
		}

		result := validator.ValidateHTTPConfig(config)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
	})
}

func TestConfigValidator_ValidateGCSConfig(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid_gcs_config", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"project_id":         "test-project",
			"bucket_name":        "test-bucket",
			"credentials_file":   "/path/to/credentials.json",
			"max_connections":    100,
			"connection_timeout": "30s",
		}

		result := validator.ValidateGCSConfig(config)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("valid_without_project_id", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"bucket_name": "my-valid-bucket",
		}

		result := validator.ValidateGCSConfig(config)
		assert.True(t, result.Valid, "bucket-only config should be valid")
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid_bucket_name_ip_address", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"bucket_name": "192.168.0.1",
		}

		result := validator.ValidateGCSConfig(config)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("invalid_bucket_name_chars", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"bucket_name": "bucket@name!",
		}

		result := validator.ValidateGCSConfig(config)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("invalid_bucket_name_too_short", func(t *testing.T) {
		t.Parallel()
		config := map[string]any{
			"bucket_name": "ab",
		}

		result := validator.ValidateGCSConfig(config)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
	})
}

func TestConfigValidator_GetValidationSuggestions(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	errors := []ValidationError{
		{Field: "name", Rule: "string", Message: "string cannot be empty"},
		{Field: "port", Rule: "port", Message: "port must be between 1 and 65535"},
		{Field: "url", Rule: "url", Message: "invalid URL"},
		{Field: "email", Rule: "email", Message: "invalid email format"},
		{Field: "timeout", Rule: "duration", Message: "invalid duration"},
		{Field: "count", Rule: "positive_int", Message: "must be positive"},
		{Field: "flag", Rule: "boolean", Message: "must be true or false"},
		{Field: "pattern", Rule: "regex", Message: "invalid regex"},
		{Field: "other", Rule: "unknown_rule", Message: "something went wrong"},
	}

	suggestions := validator.GetValidationSuggestions(errors)
	assert.Len(t, suggestions, 9)
	assert.Contains(t, suggestions[0], "must be a non-empty string")
	assert.Contains(t, suggestions[1], "must be a port number between 1 and 65535")
	assert.Contains(t, suggestions[2], "must be a valid URL")
	assert.Contains(t, suggestions[3], "must be a valid email address")
	assert.Contains(t, suggestions[4], "must be a valid duration")
	assert.Contains(t, suggestions[5], "must be a positive integer")
	assert.Contains(t, suggestions[6], "must be true or false")
	assert.Contains(t, suggestions[7], "must be a valid regular expression")
	assert.Contains(t, suggestions[8], "something went wrong")
}

func TestConfigValidator_GetAvailableRules(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	rules := validator.GetAvailableRules()
	assert.NotEmpty(t, rules)
	assert.Contains(t, rules, "string")
	assert.Contains(t, rules, "url")
	assert.Contains(t, rules, "port")
	assert.Contains(t, rules, "duration")
	assert.Contains(t, rules, "positive_int")
	assert.Contains(t, rules, "boolean")
}

func TestConfigValidator_GetRule(t *testing.T) {
	t.Parallel()
	validator := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	rule, exists := validator.GetRule("string")
	assert.True(t, exists)
	assert.Equal(t, "string", rule.Name)
	assert.Equal(t, "Validates that the value is a non-empty string", rule.Description)

	_, exists = validator.GetRule("nonexistent")
	assert.False(t, exists)
}

func TestConfigValidator_ValidateSecurityConfig(t *testing.T) {
	t.Parallel()
	v := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		cfg := map[string]any{
			"csp_enabled":         true,
			"hsts_enabled":        true,
			"hsts_max_age":        "1h",
			"cors_enabled":        false,
			"cors_origins":        "*",
			"rate_limit_enabled":  true,
			"rate_limit_requests": 100,
			"rate_limit_window":   "1m",
		}
		result := v.ValidateSecurityConfig(cfg)
		assert.True(t, result.Valid)
	})

	t.Run("invalid boolean", func(t *testing.T) {
		t.Parallel()
		cfg := map[string]any{
			"csp_enabled": "notabool",
		}
		result := v.ValidateSecurityConfig(cfg)
		assert.False(t, result.Valid)
	})
}

func TestConfigValidator_ValidateMetricsConfig(t *testing.T) {
	t.Parallel()
	v := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		cfg := map[string]any{
			"enabled":        true,
			"namespace":      "myapp",
			"subsystem":      "gcs",
			"metrics_path":   "/metrics",
			"listen_address": ":9090",
		}
		result := v.ValidateMetricsConfig(cfg)
		assert.True(t, result.Valid)
	})

	t.Run("invalid boolean", func(t *testing.T) {
		t.Parallel()
		cfg := map[string]any{
			"enabled": "notabool",
		}
		result := v.ValidateMetricsConfig(cfg)
		assert.False(t, result.Valid)
	})
}

func TestConfigValidator_ValidateRuntimeConfig(t *testing.T) {
	t.Parallel()
	v := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		cfg := map[string]any{
			"log_level":       "debug",
			"max_connections": 50,
			"read_timeout":    "30s",
			"write_timeout":   "30s",
			"idle_timeout":    "60s",
		}
		result := v.ValidateRuntimeConfig(cfg)
		assert.True(t, result.Valid)
	})

	t.Run("empty config", func(t *testing.T) {
		t.Parallel()
		result := v.ValidateRuntimeConfig(map[string]any{})
		assert.True(t, result.Valid)
	})
}

func TestConfigValidator_ValidateField_EdgeCases(t *testing.T) {
	t.Parallel()
	v := NewConfigValidator(slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("nil value required field", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("name", nil, "string")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required field is missing")
	})

	t.Run("unknown rule", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("name", "val", "nonexistent_rule")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown validation rule")
	})

	t.Run("email valid", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("email", "user@example.com", "email")
		assert.NoError(t, err)
	})

	t.Run("email invalid", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("email", "not-an-email", "email")
		assert.Error(t, err)
	})

	t.Run("regex valid", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("pattern", "^test.*$", "regex")
		assert.NoError(t, err)
	})

	t.Run("regex invalid", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("pattern", "[invalid", "regex")
		assert.Error(t, err)
	})

	t.Run("boolean string true", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("flag", "true", "boolean")
		assert.NoError(t, err)
	})

	t.Run("port as string", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("port", "8080", "port")
		assert.NoError(t, err)
	})

	t.Run("positive_int as string", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("count", "42", "positive_int")
		assert.NoError(t, err)
	})

	t.Run("duration wrong type", func(t *testing.T) {
		t.Parallel()
		err := v.ValidateField("timeout", 123, "duration")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected duration")
	})
}

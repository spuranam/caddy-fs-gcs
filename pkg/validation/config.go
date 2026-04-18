package validation

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Compiled regexes for GCS bucket name validation.
var (
	bucketNameCharsRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$`)
	ipAddressRe       = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
)

// ConfigValidator provides configuration validation
//
// patternEmail is compiled once at package level to avoid per-call overhead.
var patternEmail = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

type ConfigValidator struct {
	logger         *slog.Logger
	mu             sync.RWMutex
	rules          map[string]ValidationRule
	StrictMode     bool
	ValidateOnLoad bool
	CustomRules    map[string]ValidationRule
}

// ValidationRule defines a validation rule
type ValidationRule struct {
	Name        string
	Description string
	Validate    func(any) error
	Required    bool
	Type        string
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
	Rule    string `json:"rule"`
	Code    string `json:"code,omitempty"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
	Rule    string `json:"rule"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

// ValidationResult contains validation results
type ValidationResult struct {
	Valid     bool                `json:"valid"`
	Errors    []ValidationError   `json:"errors,omitempty"`
	Warnings  []ValidationWarning `json:"warnings,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
}

// GCSConfig represents GCS configuration
type GCSConfig struct {
	ProjectID         string `json:"project_id"`
	BucketName        string `json:"bucket_name"`
	CredentialsFile   string `json:"credentials_file,omitempty"`
	CredentialsConfig string `json:"credentials_config,omitempty"`
	MaxConnections    int    `json:"max_connections,omitempty"`
	ConnectionTimeout string `json:"connection_timeout,omitempty"`
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(logger *slog.Logger) *ConfigValidator {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	validator := &ConfigValidator{
		logger:         logger,
		rules:          make(map[string]ValidationRule),
		StrictMode:     false,
		ValidateOnLoad: true,
		CustomRules:    make(map[string]ValidationRule),
	}

	// Register default validation rules
	validator.registerDefaultRules()

	return validator
}

// registerDefaultRules registers default validation rules
func (v *ConfigValidator) registerDefaultRules() {
	// String validation rules
	v.RegisterRule("string", ValidationRule{
		Name:        "string",
		Description: "Validates that the value is a non-empty string",
		Validate: func(value any) error {
			if str, ok := value.(string); !ok {
				return fmt.Errorf("expected string, got %T", value)
			} else if strings.TrimSpace(str) == "" {
				return errors.New("string cannot be empty")
			}
			return nil
		},
		Required: true,
		Type:     "string",
	})

	// URL validation rules
	v.RegisterRule("url", ValidationRule{
		Name:        "url",
		Description: "Validates that the value is a valid absolute URL with scheme and host",
		Validate: func(value any) error {
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", value)
			}
			u, err := url.Parse(str)
			if err != nil {
				return fmt.Errorf("invalid URL: %w", err)
			}
			if u.Scheme == "" || u.Host == "" {
				return errors.New("URL must have a scheme and host (e.g. https://example.com)")
			}
			return nil
		},
		Required: true,
		Type:     "string",
	})

	// Email validation rules
	v.RegisterRule("email", ValidationRule{
		Name:        "email",
		Description: "Validates that the value is a valid email address",
		Validate: func(value any) error {
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", value)
			}
			if !patternEmail.MatchString(str) {
				return errors.New("invalid email format")
			}
			return nil
		},
		Required: true,
		Type:     "string",
	})

	// Port validation rules
	v.RegisterRule("port", ValidationRule{
		Name:        "port",
		Description: "Validates that the value is a valid port number (1-65535)",
		Validate: func(value any) error {
			var port int
			switch v := value.(type) {
			case int:
				port = v
			case int64:
				port = int(v)
			case string:
				if _, err := fmt.Sscanf(v, "%d", &port); err != nil {
					return fmt.Errorf("invalid port format: %w", err)
				}
			default:
				return fmt.Errorf("expected port number, got %T", value)
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("port must be between 1 and 65535, got %d", port)
			}
			return nil
		},
		Required: true,
		Type:     "integer",
	})

	// Duration validation rules
	v.RegisterRule("duration", ValidationRule{
		Name:        "duration",
		Description: "Validates that the value is a valid duration",
		Validate: func(value any) error {
			var duration time.Duration
			switch v := value.(type) {
			case time.Duration:
				duration = v
			case string:
				var err error
				duration, err = time.ParseDuration(v)
				if err != nil {
					return fmt.Errorf("invalid duration format: %w", err)
				}
			default:
				return fmt.Errorf("expected duration, got %T", value)
			}
			if duration < 0 {
				return errors.New("duration cannot be negative")
			}
			return nil
		},
		Required: true,
		Type:     "duration",
	})

	// Positive integer validation rules
	v.RegisterRule("positive_int", ValidationRule{
		Name:        "positive_int",
		Description: "Validates that the value is a positive integer",
		Validate: func(value any) error {
			var num int
			switch v := value.(type) {
			case int:
				num = v
			case int64:
				num = int(v)
			case string:
				if _, err := fmt.Sscanf(v, "%d", &num); err != nil {
					return fmt.Errorf("invalid integer format: %w", err)
				}
			default:
				return fmt.Errorf("expected integer, got %T", value)
			}
			if num <= 0 {
				return fmt.Errorf("value must be positive, got %d", num)
			}
			return nil
		},
		Required: true,
		Type:     "integer",
	})

	// Boolean validation rules
	v.RegisterRule("boolean", ValidationRule{
		Name:        "boolean",
		Description: "Validates that the value is a boolean",
		Validate: func(value any) error {
			switch v := value.(type) {
			case bool:
				return nil
			case string:
				str := strings.ToLower(v)
				if str == "true" || str == "false" {
					return nil
				}
				return fmt.Errorf("invalid boolean value: %s", v)
			default:
				return fmt.Errorf("expected boolean, got %T", value)
			}
		},
		Required: true,
		Type:     "boolean",
	})

	// Regex validation rules
	v.RegisterRule("regex", ValidationRule{
		Name:        "regex",
		Description: "Validates that the value is a valid regular expression",
		Validate: func(value any) error {
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", value)
			}
			if _, err := regexp.Compile(str); err != nil {
				return fmt.Errorf("invalid regex: %w", err)
			}
			return nil
		},
		Required: true,
		Type:     "string",
	})

	// Optional validation rules (not required)
	v.RegisterRule("optional_string", ValidationRule{
		Name:        "optional_string",
		Description: "Validates that the value is a string (if present)",
		Validate: func(value any) error {
			if value == nil || value == "" {
				return nil // Skip validation for empty values
			}
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", value)
			}
			if strings.TrimSpace(str) == "" {
				return errors.New("string cannot be empty")
			}
			return nil
		},
		Required: false,
		Type:     "string",
	})

	v.RegisterRule("optional_positive_int", ValidationRule{
		Name:        "optional_positive_int",
		Description: "Validates that the value is a positive integer (if present)",
		Validate: func(value any) error {
			if value == nil || value == 0 {
				return nil // Skip validation for empty/zero values
			}
			if num, ok := value.(int); ok && num <= 0 {
				return errors.New("must be a positive integer")
			}
			return nil
		},
		Required: false,
		Type:     "integer",
	})

	v.RegisterRule("optional_duration", ValidationRule{
		Name:        "optional_duration",
		Description: "Validates that the value is a valid duration (if present)",
		Validate: func(value any) error {
			if value == nil || value == "" {
				return nil // Skip validation for empty values
			}
			if str, ok := value.(string); ok {
				if _, err := time.ParseDuration(str); err != nil {
					return fmt.Errorf("invalid duration: %w", err)
				}
			}
			return nil
		},
		Required: false,
		Type:     "string",
	})

	// GCS bucket name validation (3-63 chars, lowercase, no IP-like names)
	v.RegisterRule("bucket_name", ValidationRule{
		Name:        "bucket_name",
		Description: "Validates that the value is a valid GCS bucket name",
		Validate: func(value any) error {
			str, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", value)
			}
			if strings.TrimSpace(str) == "" {
				return errors.New("bucket name cannot be empty")
			}
			if len(str) < 3 || len(str) > 63 {
				return fmt.Errorf("bucket name must be 3-63 characters, got %d", len(str))
			}
			if !bucketNameCharsRe.MatchString(str) {
				return errors.New("bucket name may only contain lowercase letters, digits, hyphens, underscores, and dots")
			}
			if str[0] == '-' || str[0] == '.' || str[len(str)-1] == '-' || str[len(str)-1] == '.' {
				return errors.New("bucket name must not start or end with a hyphen or dot")
			}
			if strings.Contains(str, "..") {
				return errors.New("bucket name must not contain consecutive dots")
			}
			if ipAddressRe.MatchString(str) {
				return errors.New("bucket name must not be an IP address")
			}
			return nil
		},
		Required: true,
		Type:     "string",
	})
}

// RegisterRule registers a validation rule
func (v *ConfigValidator) RegisterRule(name string, rule ValidationRule) {
	v.mu.Lock()
	v.rules[name] = rule
	v.mu.Unlock()
	v.logger.Debug("validation rule registered",
		"name", name,
		"description", rule.Description)
}

// ValidateField validates a single field
func (v *ConfigValidator) ValidateField(fieldName string, value any, ruleName string) error {
	v.mu.RLock()
	rule, exists := v.rules[ruleName]
	v.mu.RUnlock()
	if !exists {
		return fmt.Errorf("unknown validation rule: %s", ruleName)
	}

	if value == nil && rule.Required {
		return ValidationError{
			Field:   fieldName,
			Message: "required field is missing",
			Value:   value,
			Rule:    ruleName,
		}
	}

	if value != nil {
		if err := rule.Validate(value); err != nil {
			return ValidationError{
				Field:   fieldName,
				Message: err.Error(),
				Value:   value,
				Rule:    ruleName,
			}
		}
	}

	return nil
}

// ValidateConfig validates a configuration map
func (v *ConfigValidator) ValidateConfig(config map[string]any, rules map[string]string) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	for fieldName, ruleName := range rules {
		value, exists := config[fieldName]
		v.mu.RLock()
		ruleRequired := v.rules[ruleName].Required
		v.mu.RUnlock()
		if !exists && ruleRequired {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   fieldName,
				Message: "required field is missing",
				Value:   nil,
				Rule:    ruleName,
			})
			continue
		}

		if exists {
			if err := v.ValidateField(fieldName, value, ruleName); err != nil {
				if validationErr, ok := errors.AsType[ValidationError](err); ok {
					result.Valid = false
					result.Errors = append(result.Errors, validationErr)
				} else {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Field:   fieldName,
						Message: err.Error(),
						Value:   value,
						Rule:    ruleName,
					})
				}
			}
		}
	}

	v.logger.Info("configuration validation completed",
		"valid", result.Valid,
		"error_count", len(result.Errors),
		"warning_count", len(result.Warnings))

	return result
}

// GetAvailableRules returns all available validation rules.
// The returned map is a shallow copy, safe for concurrent use.
func (v *ConfigValidator) GetAvailableRules() map[string]ValidationRule {
	v.mu.RLock()
	defer v.mu.RUnlock()
	cp := make(map[string]ValidationRule, len(v.rules))
	for k, r := range v.rules {
		cp[k] = r
	}
	return cp
}

// GetRule returns a specific validation rule
func (v *ConfigValidator) GetRule(name string) (ValidationRule, bool) {
	v.mu.RLock()
	rule, exists := v.rules[name]
	v.mu.RUnlock()
	return rule, exists
}

// ValidateHTTPConfig validates HTTP server configuration
func (v *ConfigValidator) ValidateHTTPConfig(config map[string]any) *ValidationResult {
	rules := map[string]string{
		"listen_address":  "string",
		"port":            "port",
		"read_timeout":    "duration",
		"write_timeout":   "duration",
		"idle_timeout":    "duration",
		"max_connections": "positive_int",
	}

	return v.ValidateConfig(config, rules)
}

// ValidateGCSConfig validates GCS configuration
func (v *ConfigValidator) ValidateGCSConfig(config map[string]any) *ValidationResult {
	rules := map[string]string{
		"project_id":  "optional_string",
		"bucket_name": "bucket_name",
		// Optional fields - only validate if present
		"credentials_file":   "optional_string",
		"credentials_config": "optional_string",
		"max_connections":    "optional_positive_int",
		"connection_timeout": "optional_duration",
	}

	return v.ValidateConfig(config, rules)
}

// ValidateSecurityConfig validates security configuration
func (v *ConfigValidator) ValidateSecurityConfig(config map[string]any) *ValidationResult {
	rules := map[string]string{
		"csp_enabled":         "boolean",
		"hsts_enabled":        "boolean",
		"hsts_max_age":        "duration",
		"cors_enabled":        "boolean",
		"cors_origins":        "string",
		"rate_limit_enabled":  "boolean",
		"rate_limit_requests": "positive_int",
		"rate_limit_window":   "duration",
	}

	return v.ValidateConfig(config, rules)
}

// ValidateMetricsConfig validates metrics configuration
func (v *ConfigValidator) ValidateMetricsConfig(config map[string]any) *ValidationResult {
	rules := map[string]string{
		"enabled":        "boolean",
		"namespace":      "string",
		"subsystem":      "string",
		"metrics_path":   "string",
		"listen_address": "string",
	}

	return v.ValidateConfig(config, rules)
}

// GetValidationSuggestions returns suggestions for fixing validation errors
func (v *ConfigValidator) GetValidationSuggestions(errors []ValidationError) []string {
	suggestions := make([]string, 0, len(errors))

	for _, err := range errors {
		switch err.Rule {
		case "string":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a non-empty string", err.Field))
		case "url":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a valid URL (e.g., https://example.com)", err.Field))
		case "email":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a valid email address (e.g., user@example.com)", err.Field))
		case "port":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a port number between 1 and 65535", err.Field))
		case "duration":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a valid duration (e.g., 30s, 5m, 1h)", err.Field))
		case "positive_int":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a positive integer", err.Field))
		case "boolean":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be true or false", err.Field))
		case "regex":
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' must be a valid regular expression", err.Field))
		default:
			suggestions = append(suggestions, fmt.Sprintf("Field '%s' has validation error: %s", err.Field, err.Message))
		}
	}

	return suggestions
}

// ValidateRuntimeConfig validates runtime configuration
func (v *ConfigValidator) ValidateRuntimeConfig(config map[string]any) *ValidationResult {
	rules := map[string]string{
		"log_level":       "optional_string",
		"max_connections": "optional_positive_int",
		"read_timeout":    "optional_duration",
		"write_timeout":   "optional_duration",
		"idle_timeout":    "optional_duration",
	}

	return v.ValidateConfig(config, rules)
}

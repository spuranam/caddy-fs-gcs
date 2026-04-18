package logger

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeString removes control characters and newlines from user input
// to prevent log injection attacks.
func SanitizeString(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// SanitizeQuery sanitizes query strings for logging, limiting length to 200 chars.
// Truncation is UTF-8 aware to avoid producing invalid rune sequences.
func SanitizeQuery(query string) string {
	const maxLen = 200
	if len(query) <= maxLen {
		return SanitizeString(query)
	}
	// Find the last valid rune boundary at or before maxLen.
	truncated := query[:maxLen]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return SanitizeString(truncated) + "..."
}

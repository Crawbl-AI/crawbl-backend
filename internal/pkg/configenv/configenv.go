// Package configenv provides helpers for resolving sensitive configuration
// values from environment variables or volume-mounted secret files.
package configenv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SecretString resolves a sensitive setting by checking, in order:
//  1. The environment variable KEY directly
//  2. A file at $SECRETS_DIR/<KEY> (volume-mounted secrets)
//  3. A file path from the KEY_FILE environment variable
//  4. The provided fallback value
//
// This allows the same code to work in local dev (env vars) and
// production with volume-mounted secrets.
func SecretString(key, fallback string) string {
	// 1. Direct env var.
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	// 2. File-based secret at $SECRETS_DIR/<KEY>.
	secretsDir := os.Getenv("SECRETS_DIR")
	if secretsDir == "" {
		secretsDir = defaultSecretsDir
	}
	if data, err := os.ReadFile(filepath.Join(secretsDir, key)); err == nil { // #nosec G304,G703 -- CLI tool, paths from developer config
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}

	// 3. Legacy KEY_FILE env var (file-based secret injection).
	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if filePath != "" {
		if raw, err := os.ReadFile(filePath); err == nil { // #nosec G304,G703 -- CLI tool, paths from developer config
			if value := strings.TrimSpace(string(raw)); value != "" {
				return value
			}
		}
	}

	return fallback
}

// StringOr returns the value of the environment variable key, or defaultValue
// if the variable is unset or empty.
func StringOr(key, defaultValue string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return defaultValue
}

// IntOr parses the environment variable key as an integer and returns it,
// or returns defaultValue if the variable is unset, empty, or not a valid integer.
func IntOr(key string, defaultValue int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// BoolOr parses the environment variable key as a boolean and returns it,
// or returns defaultValue if the variable is unset, empty, or not a valid boolean.
// Truthy values: "1", "true", "yes", "on" (case-insensitive).
func BoolOr(key string, defaultValue bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// DurationOr parses the environment variable key as a time.Duration and returns it,
// or returns defaultValue if the variable is unset, empty, or not a valid duration.
// The duration format follows time.ParseDuration conventions (e.g., "5m", "1h30m").
func DurationOr(key string, defaultValue time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultValue
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return defaultValue
	}
	return parsed
}

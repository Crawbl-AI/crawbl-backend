package configenv

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultSecretsDir is the default path to check for file-based secrets.
const defaultSecretsDir = "/mnt/secrets"

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
	if data, err := os.ReadFile(filepath.Join(secretsDir, key)); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}

	// 3. Legacy KEY_FILE env var (file-based secret injection).
	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if filePath != "" {
		if raw, err := os.ReadFile(filePath); err == nil {
			if value := strings.TrimSpace(string(raw)); value != "" {
				return value
			}
		}
	}

	return fallback
}

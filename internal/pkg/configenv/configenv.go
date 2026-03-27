package configenv

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultSecretsDir is the default mount path for CSI Secrets Store volumes.
const defaultSecretsDir = "/mnt/secrets"

// SecretString resolves a sensitive setting by checking, in order:
//  1. The environment variable KEY directly
//  2. A file at $SECRETS_DIR/<KEY> (CSI Secrets Store mount)
//  3. A file path from the KEY_FILE environment variable
//  4. The provided fallback value
//
// This allows the same code to work in local dev (env vars) and production
// with CSI volume mounts.
func SecretString(key, fallback string) string {
	// 1. Direct env var.
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	// 2. CSI Secrets Store file mount at $SECRETS_DIR/<KEY>.
	secretsDir := os.Getenv("SECRETS_DIR")
	if secretsDir == "" {
		secretsDir = defaultSecretsDir
	}
	if data, err := os.ReadFile(filepath.Join(secretsDir, key)); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}

	// 3. Legacy KEY_FILE env var (Vault Agent Injector).
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

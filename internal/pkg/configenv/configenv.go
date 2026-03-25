package configenv

import (
	"os"
	"strings"
)

// SecretString resolves a sensitive setting from either KEY or KEY_FILE.
// Direct env wins; KEY_FILE is used as a fallback that reads the file contents.
// This is used to read secrets created by Vault Agent Injector.
func SecretString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if filePath == "" {
		return fallback
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fallback
	}

	value := strings.TrimSpace(string(raw))
	if value == "" {
		return fallback
	}

	return value
}

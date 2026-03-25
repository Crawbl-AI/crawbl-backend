package zeroclaw

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
)

// File permissions for bootstrap config (owner read/write only).
const configFilePerm = 0o600

// RuntimeVaultConfig holds Vault configuration for secret injection.
// Duplicated from controller package to avoid import cycles.
type RuntimeVaultConfig struct {
	Enabled  bool
	FileName string
}

var managedRootKeys = []string{
	"default_provider",
	"default_model",
	"default_temperature",
}

var managedAutonomyKeys = []string{
	"level",
	"workspace_only",
	"allowed_commands",
	"forbidden_paths",
	"auto_approve",
	"always_ask",
}

var managedHTTPRequestKeys = []string{
	"enabled",
	"allowed_domains",
	"max_response_size",
	"timeout_secs",
	"allow_private_hosts",
}

var managedWebFetchKeys = []string{
	"enabled",
	"allowed_domains",
	"blocked_domains",
	"max_response_size",
	"timeout_secs",
}

var managedWebSearchKeys = []string{
	"enabled",
	"provider",
	"brave_api_key",
	"searxng_instance_url",
	"max_results",
	"timeout_secs",
}

var managedGatewayKeys = []string{
	"port",
	"host",
	"allow_public_bind",
	"require_pairing",
}

func EnsureManagedConfig(bootstrapPath, livePath string, vaultConfig *RuntimeVaultConfig) error {
	bootstrapBytes, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return fmt.Errorf("read bootstrap config: %w", err)
	}

	if _, err := os.Stat(livePath); os.IsNotExist(err) {
		return writeConfigFile(livePath, bootstrapBytes)
	} else if err != nil {
		return fmt.Errorf("stat live config: %w", err)
	}

	bootstrapDoc, err := decodeTOMLDocument(bootstrapBytes)
	if err != nil {
		return fmt.Errorf("decode bootstrap config: %w", err)
	}

	liveBytes, err := os.ReadFile(livePath)
	if err != nil {
		return fmt.Errorf("read live config: %w", err)
	}
	liveDoc, err := decodeTOMLDocument(liveBytes)
	if err != nil {
		return fmt.Errorf("decode live config: %w", err)
	}

	mergeManagedConfig(liveDoc, bootstrapDoc)
	mergeManagedAPIKey(liveDoc, vaultConfig)

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(liveDoc); err != nil {
		return fmt.Errorf("encode merged config: %w", err)
	}

	return writeConfigFile(livePath, buf.Bytes())
}

func decodeTOMLDocument(raw []byte) (map[string]any, error) {
	doc := map[string]any{}
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

//nolint:cyclop,gocognit
func mergeManagedConfig(liveDoc, bootstrapDoc map[string]any) {
	for _, key := range managedRootKeys {
		if value, ok := bootstrapDoc[key]; ok {
			liveDoc[key] = value
		}
	}

	bootstrapAutonomy, ok := bootstrapDoc["autonomy"].(map[string]any)
	if ok {
		liveAutonomy, ok := liveDoc["autonomy"].(map[string]any)
		if !ok {
			liveAutonomy = map[string]any{}
			liveDoc["autonomy"] = liveAutonomy
		}

		for _, key := range managedAutonomyKeys {
			if value, ok := bootstrapAutonomy[key]; ok {
				liveAutonomy[key] = value
			}
		}
	}

	if bootstrapHTTP, ok := bootstrapDoc["http_request"].(map[string]any); ok {
		liveHTTP, ok := liveDoc["http_request"].(map[string]any)
		if !ok {
			liveHTTP = map[string]any{}
			liveDoc["http_request"] = liveHTTP
		}
		for _, key := range managedHTTPRequestKeys {
			if value, ok := bootstrapHTTP[key]; ok {
				liveHTTP[key] = value
			}
		}
	}

	if bootstrapWebFetch, ok := bootstrapDoc["web_fetch"].(map[string]any); ok {
		liveWebFetch, ok := liveDoc["web_fetch"].(map[string]any)
		if !ok {
			liveWebFetch = map[string]any{}
			liveDoc["web_fetch"] = liveWebFetch
		}
		for _, key := range managedWebFetchKeys {
			if value, ok := bootstrapWebFetch[key]; ok {
				liveWebFetch[key] = value
			}
		}
	}

	if bootstrapWebSearch, ok := bootstrapDoc["web_search"].(map[string]any); ok {
		liveWebSearch, ok := liveDoc["web_search"].(map[string]any)
		if !ok {
			liveWebSearch = map[string]any{}
			liveDoc["web_search"] = liveWebSearch
		}
		for _, key := range managedWebSearchKeys {
			if value, ok := bootstrapWebSearch[key]; ok {
				liveWebSearch[key] = value
			}
		}
	}

	bootstrapGateway, ok := bootstrapDoc["gateway"].(map[string]any)
	if !ok {
		return
	}

	liveGateway, ok := liveDoc["gateway"].(map[string]any)
	if !ok {
		liveGateway = map[string]any{}
		liveDoc["gateway"] = liveGateway
	}

	for _, key := range managedGatewayKeys {
		if value, ok := bootstrapGateway[key]; ok {
			liveGateway[key] = value
		}
	}
}

func writeConfigFile(path string, raw []byte) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), "config.toml.*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmpFile.Write(raw); err != nil {
		cleanup()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Chmod(tmpPath, configFilePerm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace live config: %w", err)
	}
	return nil
}

// readVaultSecret reads the API key from a Vault-injected file.
// Returns empty string if Vault is disabled or file cannot be read.
func readVaultSecret(vaultConfig *RuntimeVaultConfig) string {
	if vaultConfig == nil || !vaultConfig.Enabled {
		return ""
	}

	// Vault Agent injects secrets to /vault/secrets/<FileName>
	vaultPath := filepath.Join("/vault/secrets", vaultConfig.FileName)
	raw, err := os.ReadFile(vaultPath)
	if err != nil {
		// Log for debugging: file may not exist if Vault agent failed
		fmt.Fprintf(os.Stderr, "Vault secret file not found at %s: %v\n", vaultPath, err)
		return ""
	}

	value := strings.TrimSpace(string(raw))
	if value == "" {
		return ""
	}

	return value
}

// mergeManagedAPIKey sets the api_key in the live config from Vault or environment variables.
// Vault-injected files take precedence when Vault is enabled.
// Falls back to environment variables for dev/test compatibility.
func mergeManagedAPIKey(liveDoc map[string]any, vaultConfig *RuntimeVaultConfig) {
	// Try Vault first if enabled
	if vaultSecret := readVaultSecret(vaultConfig); vaultSecret != "" {
		fmt.Fprintln(os.Stderr, "Using API key from Vault-injected file")
		liveDoc["api_key"] = vaultSecret
		return
	}

	// Fallback to environment variables
	for _, envName := range []string{
		"OPENAI_API_KEY",
		"USERSWARM_API_KEY",
		"ZEROCLAW_API_KEY",
		"API_KEY",
		"OPENROUTER_API_KEY",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
	} {
		value := configenv.SecretString(envName, "")
		if value == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "Using API key from environment variable %s (Vault not enabled or file read failed)\n", envName)
		liveDoc["api_key"] = value
		return
	}
}

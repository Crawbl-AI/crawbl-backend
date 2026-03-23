package zeroclaw

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

var managedRootKeys = []string{
	"default_provider",
	"default_model",
	"default_temperature",
}

var managedGatewayKeys = []string{
	"port",
	"host",
	"allow_public_bind",
}

func EnsureManagedConfig(bootstrapPath, livePath string) error {
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
	mergeManagedAPIKey(liveDoc)

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

func mergeManagedConfig(liveDoc, bootstrapDoc map[string]any) {
	for _, key := range managedRootKeys {
		if value, ok := bootstrapDoc[key]; ok {
			liveDoc[key] = value
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
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace live config: %w", err)
	}
	return nil
}

func mergeManagedAPIKey(liveDoc map[string]any) {
	for _, envName := range []string{
		"USERSWARM_API_KEY",
		"ZEROCLAW_API_KEY",
		"API_KEY",
		"EDENAI_API_KEY",
		"OPENROUTER_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
	} {
		value := strings.TrimSpace(os.Getenv(envName))
		if value == "" {
			continue
		}
		liveDoc["api_key"] = value
		return
	}
}

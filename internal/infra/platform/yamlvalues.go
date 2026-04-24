// Package platform provides Pulumi platform infrastructure setup.
//
// This package also contains helpers for loading YAML configuration and Helm chart values files.
package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadStackConfig reads a key from a Pulumi stack config file (Pulumi.<env>.yaml)
// and unmarshals it into the provided target struct.
func LoadStackConfig(env, key string, target any) error {
	filename := fmt.Sprintf("Pulumi.%s.yaml", env)
	data, err := os.ReadFile(filename) // #nosec G304 -- CLI tool, paths from developer config
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}

	var file stackConfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse %s: %w", filename, err)
	}

	raw, ok := file.Config[key]
	if !ok {
		return fmt.Errorf("missing %s in %s", key, filename)
	}

	rawBytes, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}

	if err := yaml.Unmarshal(rawBytes, target); err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}

	return nil
}

// Load reads a YAML values file from the given directory and returns it as map[string]interface{}.
func Load(dir, name string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- CLI tool, paths from developer config
	if err != nil {
		return nil, fmt.Errorf("read values file %s: %w", name, err)
	}
	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse values file %s: %w", name, err)
	}
	return result, nil
}

// MustLoad reads a YAML values file from the given directory or panics.
func MustLoad(dir, name string) map[string]any {
	v, err := Load(dir, name)
	if err != nil {
		panic(err)
	}
	return v
}

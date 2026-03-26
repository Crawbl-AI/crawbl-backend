// Package yamlvalues loads Helm chart values from YAML files.
package yamlvalues

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML values file from the given directory and returns it as map[string]interface{}.
func Load(dir, name string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, fmt.Errorf("read values file %s: %w", name, err)
	}
	var result map[string]interface{}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse values file %s: %w", name, err)
	}
	return result, nil
}

// MustLoad reads a YAML values file from the given directory or panics.
func MustLoad(dir, name string) map[string]interface{} {
	v, err := Load(dir, name)
	if err != nil {
		panic(err)
	}
	return v
}

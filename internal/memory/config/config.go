// Package config provides configurable settings for the MemPalace memory system.
package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed noise_patterns.json
var noiseFS embed.FS

// NoiseConfig holds configurable noise filtering patterns.
type NoiseConfig struct {
	Patterns  []string `json:"patterns"`
	MinLength int      `json:"min_length"`
}

// LoadNoiseConfig loads noise patterns from the embedded JSON file.
func LoadNoiseConfig() (*NoiseConfig, error) {
	data, err := noiseFS.ReadFile("noise_patterns.json")
	if err != nil {
		return nil, fmt.Errorf("load noise config: %w", err)
	}
	var cfg NoiseConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse noise config: %w", err)
	}
	return &cfg, nil
}

// CompileNoisePattern builds a regexp from the configured patterns.
func (c *NoiseConfig) CompileNoisePattern() *regexp.Regexp {
	escaped := make([]string, len(c.Patterns))
	for i, p := range c.Patterns {
		escaped[i] = regexp.QuoteMeta(p)
	}
	pattern := fmt.Sprintf(`(?i)^(%s)[\s!.?]*$`, strings.Join(escaped, "|"))
	return regexp.MustCompile(pattern)
}

// Package config provides configurable settings for the MemPalace memory system.
package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed *.json
var configFS embed.FS

// NoiseConfig holds configurable noise filtering patterns.
type NoiseConfig struct {
	Patterns  []string `json:"patterns"`
	MinLength int      `json:"min_length"`
}

// LoadNoiseConfig loads noise patterns from the embedded JSON file.
func LoadNoiseConfig() (*NoiseConfig, error) {
	data, err := configFS.ReadFile("noise_patterns.json")
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

// ClassifyConfig holds configurable patterns for the memory classifier.
type ClassifyConfig struct {
	SegmentPatterns  map[string]string   `json:"segment_patterns"`
	MemoryMarkers    map[string][]string `json:"memory_markers"`
	ResolverPatterns []string            `json:"resolver_patterns"`
	CodeLinePatterns []string            `json:"code_line_patterns"`
}

// LoadClassifyConfig loads classifier patterns from the embedded JSON file.
func LoadClassifyConfig() (*ClassifyConfig, error) {
	data, err := configFS.ReadFile("classify_patterns.json")
	if err != nil {
		return nil, fmt.Errorf("load classify config: %w", err)
	}
	var cfg ClassifyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse classify config: %w", err)
	}
	return &cfg, nil
}

// CompileSegmentPatterns compiles the named segment detection regexes.
func (c *ClassifyConfig) CompileSegmentPatterns() map[string]*regexp.Regexp {
	compiled := make(map[string]*regexp.Regexp, len(c.SegmentPatterns))
	for name, pattern := range c.SegmentPatterns {
		compiled[name] = regexp.MustCompile(pattern)
	}
	return compiled
}

// CompileMarkers compiles all memory-type marker patterns.
func (c *ClassifyConfig) CompileMarkers() map[string][]*regexp.Regexp {
	compiled := make(map[string][]*regexp.Regexp, len(c.MemoryMarkers))
	for memType, patterns := range c.MemoryMarkers {
		for _, p := range patterns {
			r, err := regexp.Compile(p)
			if err == nil {
				compiled[memType] = append(compiled[memType], r)
			}
		}
	}
	return compiled
}

// CompileResolvers compiles the resolution-detection patterns.
func (c *ClassifyConfig) CompileResolvers() []*regexp.Regexp {
	var compiled []*regexp.Regexp
	for _, p := range c.ResolverPatterns {
		r, err := regexp.Compile(p)
		if err == nil {
			compiled = append(compiled, r)
		}
	}
	return compiled
}

// CompileCodeLines compiles the code-line detection patterns.
func (c *ClassifyConfig) CompileCodeLines() []*regexp.Regexp {
	var compiled []*regexp.Regexp
	for _, p := range c.CodeLinePatterns {
		r, err := regexp.Compile(p)
		if err == nil {
			compiled = append(compiled, r)
		}
	}
	return compiled
}

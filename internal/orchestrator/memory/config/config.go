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
	Sentiment        SentimentWords      `json:"sentiment"`
}

// SentimentWords is the positive/negative lexicon the classifier uses to
// tune sentiment-aware disambiguation. Kept alongside the regex patterns
// so all classifier tuning lives in a single JSON file.
type SentimentWords struct {
	Positive []string `json:"positive"`
	Negative []string `json:"negative"`
}

// CompilePositiveWords returns the positive lexicon as a lookup set.
func (c *ClassifyConfig) CompilePositiveWords() map[string]bool {
	return wordSet(c.Sentiment.Positive)
}

// CompileNegativeWords returns the negative lexicon as a lookup set.
func (c *ClassifyConfig) CompileNegativeWords() map[string]bool {
	return wordSet(c.Sentiment.Negative)
}

// wordSet materialises a slice of sentiment words into a lookup map.
func wordSet(words []string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
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
		compiled[memType] = compileRegexList(patterns)
	}
	return compiled
}

// compileRegexList compiles a slice of pattern strings into regexps,
// silently dropping any pattern that fails to compile. Shared by
// CompileMarkers, CompileResolvers, and CompileCodeLines so no call site
// has to own the compile-or-skip loop.
func compileRegexList(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		r, err := regexp.Compile(p)
		if err == nil {
			out = append(out, r)
		}
	}
	return out
}

// CompileResolvers compiles the resolution-detection patterns.
func (c *ClassifyConfig) CompileResolvers() []*regexp.Regexp {
	return compileRegexList(c.ResolverPatterns)
}

// CompileCodeLines compiles the code-line detection patterns.
func (c *ClassifyConfig) CompileCodeLines() []*regexp.Regexp {
	return compileRegexList(c.CodeLinePatterns)
}

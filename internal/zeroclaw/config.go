// Package zeroclaw provides configuration management for ZeroClaw AI agent runtimes.
//
// ZeroClaw is the AI agent runtime that runs inside each user's pod. This package
// handles two configuration layers:
//
//  1. Operator config (config/zeroclaw.yaml) — cluster-wide defaults loaded at startup.
//     Controls provider settings, tool permissions, autonomy rules, and reliability.
//
//  2. Per-user bootstrap config — generated from the operator config + UserSwarm CR spec.
//     Written to a ConfigMap, then merged into the PVC-backed live config by the init container.
//
// File layout:
//
//	config.go    — Operator-side types and YAML loading
//	toml.go      — Per-user TOML config generation (config.toml for the ConfigMap)
//	markdown.go  — Markdown template builders (SOUL.md, IDENTITY.md, TOOLS.md, AGENTS.md)
//	bootstrap.go — Init container logic: merge operator-managed keys into PVC live config
package zeroclaw

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Operator config (loaded from config/zeroclaw.yaml at startup)
// ---------------------------------------------------------------------------

// ZeroClawConfig holds cluster-wide defaults loaded from the operator's config file.
// These values are baked into the per-user bootstrap config.toml at provisioning time.
// The file is typically mounted as a ConfigMap in the webhook pod.
type ZeroClawConfig struct {
	Defaults    ZeroClawDefaults    `yaml:"defaults"`
	HTTPRequest ZeroClawHTTPRequest `yaml:"httpRequest"`
	WebFetch    ZeroClawWebFetch    `yaml:"webFetch"`
	WebSearch   ZeroClawWebSearch   `yaml:"webSearch"`
	Autonomy    ZeroClawAutonomy    `yaml:"autonomy"`
}

// ZeroClawDefaults controls global provider behavior: temperature, timeouts, retries.
type ZeroClawDefaults struct {
	Temperature       float64 `yaml:"temperature"`
	Timeout           int     `yaml:"timeout"`
	ShortTimeout      int     `yaml:"shortTimeout"`
	ProviderRetries   uint32  `yaml:"providerRetries"`
	ProviderBackoffMs uint64  `yaml:"providerBackoffMs"`
}

// ZeroClawHTTPRequest controls the http_request tool (raw HTTP calls from the agent).
type ZeroClawHTTPRequest struct {
	MaxResponseSize int      `yaml:"maxResponseSize"`
	AllowedDomains  []string `yaml:"allowedDomains"`
}

// ZeroClawWebFetch controls the web_fetch tool (read web page content).
type ZeroClawWebFetch struct {
	MaxResponseSize int `yaml:"maxResponseSize"`
}

// ZeroClawWebSearch controls the web_search tool (internet search).
type ZeroClawWebSearch struct {
	Provider   string `yaml:"provider"`
	MaxResults int    `yaml:"maxResults"`
}

// ZeroClawAutonomy controls what the agent can do without user approval.
type ZeroClawAutonomy struct {
	AllowedCommands []string `yaml:"allowedCommands"`
	ForbiddenPaths  []string `yaml:"forbiddenPaths"`
	AutoApprove     []string `yaml:"autoApprove"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// Built-in defaults used when no config file is provided or a field is missing.
const (
	DefaultTemperature          = 0.7
	DefaultMaxResponseSize      = 1_000_000
	DefaultTimeoutSecs          = 30
	DefaultMaxResponseSizeSmall = 500_000
	DefaultMaxResults           = 5
	DefaultTimeoutSecsShort     = 15
	DefaultProviderRetries      = 2
	DefaultProviderBackoffMs    = 500
)

// DefaultConfig returns a ZeroClawConfig populated with sensible built-in defaults.
// Used as the base before YAML overrides are applied, and as the fallback
// when no config file exists.
func DefaultConfig() *ZeroClawConfig {
	return &ZeroClawConfig{
		Defaults: ZeroClawDefaults{
			Temperature:       DefaultTemperature,
			Timeout:           DefaultTimeoutSecs,
			ShortTimeout:      DefaultTimeoutSecsShort,
			ProviderRetries:   DefaultProviderRetries,
			ProviderBackoffMs: DefaultProviderBackoffMs,
		},
		HTTPRequest: ZeroClawHTTPRequest{
			MaxResponseSize: DefaultMaxResponseSize,
			AllowedDomains:  []string{"*"},
		},
		WebFetch: ZeroClawWebFetch{
			MaxResponseSize: DefaultMaxResponseSizeSmall,
		},
		WebSearch: ZeroClawWebSearch{
			Provider:   "duckduckgo",
			MaxResults: DefaultMaxResults,
		},
		Autonomy: ZeroClawAutonomy{
			AllowedCommands: []string{
				"git", "ls", "cat", "grep", "find",
				"pwd", "wc", "head", "tail", "date", "sed",
			},
			ForbiddenPaths: []string{
				"/etc", "/root", "/usr", "/bin", "/sbin",
				"/lib", "/opt", "/boot", "/dev", "/proc",
				"/sys", "/var", "/tmp",
				"~/.ssh", "~/.gnupg", "~/.aws", "~/.config",
			},
			AutoApprove: []string{
				"file_read", "memory_recall", "web_search_tool",
				"web_fetch", "calculator", "glob_search",
				"content_search", "image_info", "weather",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// LoadConfig reads a ZeroClawConfig from a YAML file.
// If the file does not exist, DefaultConfig is returned (no error).
// This allows the webhook to start without a config file during development.
func LoadConfig(path string) (*ZeroClawConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read zeroclaw config %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse zeroclaw config %s: %w", path, err)
	}
	return cfg, nil
}

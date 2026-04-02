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
//	types.go     — All shared types, constants, and variables
//	config.go    — Operator-side YAML loading
//	toml.go      — Per-user TOML config generation (config.toml for the ConfigMap)
//	markdown.go  — Markdown template builders (SOUL.md, IDENTITY.md, TOOLS.md) and agent skill files
//	bootstrap.go — Init container logic: merge operator-managed keys into PVC live config
package zeroclaw

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
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
			// Derived from the tool catalog in tools.go — single source of truth.
			AutoApprove: DefaultAutoApproveList(),
		},
		Agents: map[string]ZeroClawAgent{
			"wally": {
				SystemPrompt: "You are Wally. Be helpful, direct, and brief. Answer in 1-3 sentences. Only elaborate when asked.",
				Agentic:      true,
				AllowedTools: []string{
					"web_search", "web_fetch", "file_read", "file_write",
					"memory_recall", "memory_store",
					"orchestrator__send_push_notification",
					"orchestrator__get_user_profile", "orchestrator__get_workspace_info",
					"orchestrator__list_conversations", "orchestrator__search_past_messages",
					"orchestrator__create_agent_history",
				},
			},
			"eve": {
				SystemPrompt: "You are Eve. Be creative, clear, and concise. Draft content efficiently. Don't over-explain.",
				Agentic:      true,
				AllowedTools: []string{
					"web_search", "web_fetch", "file_read", "file_write",
					"memory_recall", "memory_store",
					"orchestrator__send_push_notification",
					"orchestrator__get_user_profile", "orchestrator__get_workspace_info",
					"orchestrator__list_conversations", "orchestrator__search_past_messages",
					"orchestrator__create_agent_history",
				},
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

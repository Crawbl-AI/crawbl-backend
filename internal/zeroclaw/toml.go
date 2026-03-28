package zeroclaw

// This file generates the per-user config.toml that goes into the bootstrap ConfigMap.
//
// The flow:
//  1. Start with cluster-wide defaults from ZeroClawConfig.
//  2. Apply per-user overrides from the UserSwarm CR (provider, model, temperature).
//  3. Apply raw TOML overrides from spec.config.tomlOverrides (escape hatch).
//  4. Encode the result as TOML — this becomes the "config.toml" key in the ConfigMap.
//
// The init container then takes this config.toml and merges operator-managed keys
// into the PVC-backed live config (see bootstrap.go).

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/fileutil"
)

// ---------------------------------------------------------------------------
// TOML config types (what ZeroClaw actually reads)
// ---------------------------------------------------------------------------

// BootstrapConfig is the top-level TOML structure that ZeroClaw reads from config.toml.
// Field names use snake_case to match ZeroClaw's expected TOML keys.
type BootstrapConfig struct {
	APIKey             string            `toml:"api_key"`
	DefaultProvider    string            `toml:"default_provider"`
	DefaultModel       string            `toml:"default_model"`
	DefaultTemperature float64           `toml:"default_temperature"`
	Autonomy           AutonomyConfig    `toml:"autonomy"`
	HTTPRequest        HTTPRequestConfig `toml:"http_request"`
	WebFetch           WebFetchConfig    `toml:"web_fetch"`
	WebSearch          WebSearchConfig   `toml:"web_search"`
	Gateway            GatewayConfig     `toml:"gateway"`
	Reliability        Reliability       `toml:"reliability"`
}

// AutonomyConfig controls what the agent can do without asking the user.
type AutonomyConfig struct {
	Level           string   `toml:"level"`
	WorkspaceOnly   bool     `toml:"workspace_only"`
	AllowedCommands []string `toml:"allowed_commands"`
	ForbiddenPaths  []string `toml:"forbidden_paths"`
	AutoApprove     []string `toml:"auto_approve"`
	AlwaysAsk       []string `toml:"always_ask"`
}

// GatewayConfig controls ZeroClaw's HTTP gateway that the orchestrator talks to.
type GatewayConfig struct {
	Port            int32  `toml:"port"`
	Host            string `toml:"host"`
	AllowPublicBind bool   `toml:"allow_public_bind"`
	RequirePairing  bool   `toml:"require_pairing"`
}

// HTTPRequestConfig controls the agent's raw HTTP request tool.
type HTTPRequestConfig struct {
	Enabled           bool     `toml:"enabled"`
	AllowedDomains    []string `toml:"allowed_domains"`
	MaxResponseSize   int      `toml:"max_response_size"`
	TimeoutSecs       int      `toml:"timeout_secs"`
	AllowPrivateHosts bool     `toml:"allow_private_hosts"`
}

// WebFetchConfig controls the agent's web page fetching tool.
type WebFetchConfig struct {
	Enabled         bool     `toml:"enabled"`
	AllowedDomains  []string `toml:"allowed_domains"`
	BlockedDomains  []string `toml:"blocked_domains"`
	MaxResponseSize int      `toml:"max_response_size"`
	TimeoutSecs     int      `toml:"timeout_secs"`
}

// WebSearchConfig controls the agent's internet search tool.
type WebSearchConfig struct {
	Enabled            bool   `toml:"enabled"`
	Provider           string `toml:"provider"`
	MaxResults         int    `toml:"max_results"`
	TimeoutSecs        int    `toml:"timeout_secs"`
	SearxngInstanceURL string `toml:"searxng_instance_url,omitempty"`
}

// Reliability controls provider retry behavior and model fallbacks.
type Reliability struct {
	ProviderRetries uint32              `toml:"provider_retries"`
	ProviderBackoff uint64              `toml:"provider_backoff_ms"`
	ModelFallbacks  map[string][]string `toml:"model_fallbacks"`
}

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

// BuildConfigTOML generates the config.toml content for a user's bootstrap ConfigMap.
//
// Steps:
//  1. Start with cluster-wide defaults from ZeroClawConfig.
//  2. Apply per-user overrides (provider, model, temperature) from the UserSwarm spec.
//  3. Apply raw TOML overrides if spec.config.tomlOverrides is set.
//  4. Encode as TOML string.
//
// This function is pure and unit-testable: pass in a UserSwarm + ZeroClawConfig,
// assert on the returned TOML string.
func BuildConfigTOML(sw *crawblv1alpha1.UserSwarm, zc *ZeroClawConfig) (string, error) {
	if zc == nil {
		zc = DefaultConfig()
	}

	// Step 1: Build base config from cluster-wide defaults.
	cfg := BootstrapConfig{
		APIKey:             "",
		DefaultProvider:    "openai",
		DefaultModel:       "gpt-5-mini",
		DefaultTemperature: zc.Defaults.Temperature,
		Autonomy: AutonomyConfig{
			Level:           "supervised",
			WorkspaceOnly:   true,
			AllowedCommands: zc.Autonomy.AllowedCommands,
			ForbiddenPaths:  zc.Autonomy.ForbiddenPaths,
			AutoApprove:     zc.Autonomy.AutoApprove,
			AlwaysAsk:       []string{},
		},
		HTTPRequest: HTTPRequestConfig{
			Enabled:           true,
			AllowedDomains:    zc.HTTPRequest.AllowedDomains,
			MaxResponseSize:   zc.HTTPRequest.MaxResponseSize,
			TimeoutSecs:       zc.Defaults.Timeout,
			AllowPrivateHosts: false,
		},
		WebFetch: WebFetchConfig{
			Enabled:         true,
			AllowedDomains:  []string{"*"},
			BlockedDomains:  []string{},
			MaxResponseSize: zc.WebFetch.MaxResponseSize,
			TimeoutSecs:     zc.Defaults.Timeout,
		},
		WebSearch: WebSearchConfig{
			Enabled:     true,
			Provider:    zc.WebSearch.Provider,
			MaxResults:  zc.WebSearch.MaxResults,
			TimeoutSecs: zc.Defaults.ShortTimeout,
		},
		Gateway: GatewayConfig{
			Port:            gatewayPort(sw),
			Host:            "[::]",
			AllowPublicBind: true,  // Bound to pod network; access is controlled by K8s NetworkPolicy.
			RequirePairing:  true,
		},
		Reliability: Reliability{
			ProviderRetries: zc.Defaults.ProviderRetries,
			ProviderBackoff: zc.Defaults.ProviderBackoffMs,
			ModelFallbacks:  map[string][]string{},
		},
	}

	// Step 2: Apply per-user overrides from the CR spec.
	if sw.Spec.Config.DefaultProvider != "" {
		cfg.DefaultProvider = sw.Spec.Config.DefaultProvider
	}
	if sw.Spec.Config.DefaultModel != "" {
		cfg.DefaultModel = sw.Spec.Config.DefaultModel
	}
	if sw.Spec.Config.DefaultTemperature != nil {
		cfg.DefaultTemperature = *sw.Spec.Config.DefaultTemperature
	}

	// Step 3: Apply raw TOML overrides (escape hatch for anything not in the spec).
	if err := fileutil.ApplyTOMLOverrides(&cfg, sw.Spec.Config.TOMLOverrides); err != nil {
		return "", err
	}

	// Step 4: Encode as TOML.
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return "", fmt.Errorf("encode zeroclaw bootstrap config: %w", err)
	}
	return buf.String(), nil
}

// gatewayPort returns the ZeroClaw gateway port with default fallback.
func gatewayPort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

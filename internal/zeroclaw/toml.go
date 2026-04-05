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
)

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
func BuildConfigTOML(sw *crawblv1alpha1.UserSwarm, zc *ZeroClawConfig, mcpCfg ...*MCPBootstrapConfig) (string, error) {
	if zc == nil {
		zc = DefaultConfig()
	}

	// Step 1: Build base config from cluster-wide defaults.
	cfg := BootstrapConfig{
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

	// Step 3: Inject MCP client config if provided.
	if len(mcpCfg) > 0 && mcpCfg[0] != nil {
		cfg.MCP = *mcpCfg[0]
	}

	// Step 3b: Populate delegate agents from operator config.
	// Provider and model are required by ZeroClaw's Rust deserializer —
	// fill from workspace defaults if the operator YAML omits them.
	if len(zc.Agents) > 0 {
		cfg.Agents = make(map[string]DelegateAgentConfig, len(zc.Agents))
		for name, agent := range zc.Agents {
			if !isValidAgentName(name) {
				return "", fmt.Errorf("invalid agent name %q in operator config", name)
			}
			cfg.Agents[name] = DelegateAgentConfig{
				Provider:     cfg.DefaultProvider,
				Model:        cfg.DefaultModel,
				SystemPrompt: agent.SystemPrompt,
				Agentic:      agent.Agentic,
				AllowedTools: agent.AllowedTools,
				SkillsDir:    fmt.Sprintf("agents/%s", name),
			}
		}
	}

	// Step 3c: Apply per-user agent overrides from the CR spec.
	// These come from the orchestrator's agent_settings table and let users
	// customise model, allowed tools, etc. per agent without changing the
	// operator-level defaults. Only agents that already exist in the operator
	// config are overridden — we do not create new agents from user settings.
	if cfg.Agents != nil && sw.Spec.Config.Agents != nil {
		for slug, override := range sw.Spec.Config.Agents {
			agent, exists := cfg.Agents[slug]
			if !exists {
				continue
			}
			if override.Model != "" {
				agent.Model = override.Model
			}
			if len(override.AllowedTools) > 0 {
				agent.AllowedTools = override.AllowedTools
			}
			cfg.Agents[slug] = agent
		}
	}

	// Step 4: Encode as TOML. The TOMLOverrides escape hatch was removed
	// along with the CRD field in US-P2-006 — the whole zeroclaw TOML
	// path is scheduled for deletion in US-P2-008.
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

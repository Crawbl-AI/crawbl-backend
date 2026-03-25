package zeroclaw

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

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

type AutonomyConfig struct {
	Level           string   `toml:"level"`
	WorkspaceOnly   bool     `toml:"workspace_only"`
	AllowedCommands []string `toml:"allowed_commands"`
	ForbiddenPaths  []string `toml:"forbidden_paths"`
	AutoApprove     []string `toml:"auto_approve"`
	AlwaysAsk       []string `toml:"always_ask"`
}

type GatewayConfig struct {
	Port            int32  `toml:"port"`
	Host            string `toml:"host"`
	AllowPublicBind bool   `toml:"allow_public_bind"`
	RequirePairing  bool   `toml:"require_pairing"`
}

type HTTPRequestConfig struct {
	Enabled           bool     `toml:"enabled"`
	AllowedDomains    []string `toml:"allowed_domains"`
	MaxResponseSize   int      `toml:"max_response_size"`
	TimeoutSecs       int      `toml:"timeout_secs"`
	AllowPrivateHosts bool     `toml:"allow_private_hosts"`
}

type WebFetchConfig struct {
	Enabled         bool     `toml:"enabled"`
	AllowedDomains  []string `toml:"allowed_domains"`
	BlockedDomains  []string `toml:"blocked_domains"`
	MaxResponseSize int      `toml:"max_response_size"`
	TimeoutSecs     int      `toml:"timeout_secs"`
}

type WebSearchConfig struct {
	Enabled            bool   `toml:"enabled"`
	Provider           string `toml:"provider"`
	MaxResults         int    `toml:"max_results"`
	TimeoutSecs        int    `toml:"timeout_secs"`
	SearxngInstanceURL string `toml:"searxng_instance_url,omitempty"`
}

type Reliability struct {
	ProviderRetries uint32              `toml:"provider_retries"`
	ProviderBackoff uint64              `toml:"provider_backoff_ms"`
	ModelFallbacks  map[string][]string `toml:"model_fallbacks"`
}

func BuildBootstrapFiles(sw *crawblv1alpha1.UserSwarm) (map[string]string, error) {
	configTOML, err := BuildConfigTOML(sw)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"config.toml": configTOML,
		"SOUL.md":     BuildSoulMarkdown(sw),
		"IDENTITY.md": BuildIdentityMarkdown(sw),
	}, nil
}

// BuildConfigTOML generates a minimal ZeroClaw bootstrap config for shared-namespace testing.
func BuildConfigTOML(sw *crawblv1alpha1.UserSwarm) (string, error) {
	cfg := BootstrapConfig{
		APIKey:             "",
		DefaultProvider:    "openai",
		DefaultModel:       "gpt-5.4",
		DefaultTemperature: 0.7,
		Autonomy: AutonomyConfig{
			Level:         "supervised",
			WorkspaceOnly: true,
			AllowedCommands: []string{
				"git",
				"ls",
				"cat",
				"grep",
				"find",
				"pwd",
				"wc",
				"head",
				"tail",
				"date",
				"sed",
			},
			ForbiddenPaths: []string{
				"/etc",
				"/root",
				"/usr",
				"/bin",
				"/sbin",
				"/lib",
				"/opt",
				"/boot",
				"/dev",
				"/proc",
				"/sys",
				"/var",
				"/tmp",
				"~/.ssh",
				"~/.gnupg",
				"~/.aws",
				"~/.config",
			},
			AutoApprove: []string{
				"file_read",
				"memory_recall",
				"web_search_tool",
				"web_fetch",
				"calculator",
				"glob_search",
				"content_search",
				"image_info",
				"weather",
			},
			AlwaysAsk: []string{},
		},
		HTTPRequest: HTTPRequestConfig{
			Enabled:           true,
			AllowedDomains:    []string{"*"},
			MaxResponseSize:   1_000_000,
			TimeoutSecs:       30,
			AllowPrivateHosts: false,
		},
		WebFetch: WebFetchConfig{
			Enabled:         true,
			AllowedDomains:  []string{"*"},
			BlockedDomains:  []string{},
			MaxResponseSize: 500_000,
			TimeoutSecs:     30,
		},
		WebSearch: WebSearchConfig{
			Enabled:     true,
			Provider:    "duckduckgo",
			MaxResults:  5,
			TimeoutSecs: 15,
		},
		Gateway: GatewayConfig{
			Port: runtimePort(sw),
			Host: "[::]",
			// User swarms run behind an internal ClusterIP service, so the runtime has
			// to bind the pod network address. Public exposure is blocked at the
			// Kubernetes layer: no public route and a backend-only NetworkPolicy.
			AllowPublicBind: true,
			RequirePairing:  true,
		},
		Reliability: Reliability{
			ProviderRetries: 2,
			ProviderBackoff: 500,
			ModelFallbacks:  map[string][]string{},
		},
	}

	if sw.Spec.Config.DefaultProvider != "" {
		cfg.DefaultProvider = sw.Spec.Config.DefaultProvider
	}
	if sw.Spec.Config.DefaultModel != "" {
		cfg.DefaultModel = sw.Spec.Config.DefaultModel
	}
	if sw.Spec.Config.DefaultTemperature != nil {
		cfg.DefaultTemperature = *sw.Spec.Config.DefaultTemperature
	}

	if err := applyOverrides(&cfg, sw.Spec.Config.TOMLOverrides); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return "", fmt.Errorf("encode zeroclaw bootstrap config: %w", err)
	}
	return buf.String(), nil
}

func applyOverrides(cfg *BootstrapConfig, overrides string) error {
	if strings.TrimSpace(overrides) == "" {
		return nil
	}

	doc := strings.TrimSpace(overrides)
	if _, err := toml.Decode(doc, cfg); err != nil {
		return fmt.Errorf("decode zeroclaw config overrides: %w", err)
	}

	return nil
}

func runtimePort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

func BuildSoulMarkdown(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf(`# SOUL.md - Who You Are

You are ZeroClaw, the private personal assistant for user %q inside Crawbl.

## Core Principles
- Speak naturally. Do not sound like a policy bot or a generic support script.
- Start with the answer or useful action. Do not narrate internal processing.
- Avoid phrases like "I will process that", "I will use the available tools", or "I will provide the result" unless the user asked about internals.
- Be concise by default, but still sound human and grounded.
- Use tools when needed, but keep tool use invisible in normal replies.
- Be proactive and practical. Offer the next helpful step when it saves time.
- If something is unclear, ask one short concrete question instead of padding the reply.
- Do not invent facts, hidden actions, or completed work.
`, sw.Spec.UserID)
}

func BuildIdentityMarkdown(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf(`# IDENTITY.md - Who I Am

I am ZeroClaw, %s's long-lived assistant in Crawbl.

## Traits
- Calm, direct, and useful
- Conversational, not robotic
- Opinionated when it helps the user decide faster
- Respectful of the user's time; short answers are the default
- Comfortable helping with planning, research, reminders, messages, and coordination
`, sw.Spec.UserID)
}

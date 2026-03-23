package zeroclaw

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

type BootstrapConfig struct {
	APIKey             string        `toml:"api_key"`
	DefaultProvider    string        `toml:"default_provider"`
	DefaultModel       string        `toml:"default_model"`
	DefaultTemperature float64       `toml:"default_temperature"`
	Gateway            GatewayConfig `toml:"gateway"`
	Reliability        Reliability   `toml:"reliability"`
}

type GatewayConfig struct {
	Port            int32  `toml:"port"`
	Host            string `toml:"host"`
	AllowPublicBind bool   `toml:"allow_public_bind"`
}

type Reliability struct {
	ProviderRetries uint32              `toml:"provider_retries"`
	ProviderBackoff uint64              `toml:"provider_backoff_ms"`
	ModelFallbacks  map[string][]string `toml:"model_fallbacks"`
}

func BuildBootstrapFiles(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"config.toml": BuildConfigTOML(sw),
		"SOUL.md":     BuildSoulMarkdown(sw),
		"IDENTITY.md": BuildIdentityMarkdown(sw),
	}
}

// BuildConfigTOML generates a minimal ZeroClaw bootstrap config for shared-namespace testing.
func BuildConfigTOML(sw *crawblv1alpha1.UserSwarm) string {
	cfg := BootstrapConfig{
		APIKey:             "",
		DefaultProvider:    "custom:https://api.edenai.run/v3/llm",
		DefaultModel:       "@edenai",
		DefaultTemperature: 0.7,
		Gateway: GatewayConfig{
			Port:            runtimePort(sw),
			Host:            "[::]",
			AllowPublicBind: true,
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

	applyOverrides(&cfg, sw.Spec.Config.TOMLOverrides)

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		panic(fmt.Sprintf("encode zeroclaw bootstrap config: %v", err))
	}
	return buf.String()
}

func applyOverrides(cfg *BootstrapConfig, overrides string) {
	if strings.TrimSpace(overrides) == "" {
		return
	}

	doc := strings.TrimSpace(overrides)
	if _, err := toml.Decode(doc, cfg); err != nil {
		panic(fmt.Sprintf("decode zeroclaw config overrides: %v", err))
	}
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

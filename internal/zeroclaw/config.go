package zeroclaw

import (
	"bytes"
	"fmt"
	"sort"
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
}

type GatewayConfig struct {
	Port            int32  `toml:"port"`
	Host            string `toml:"host"`
	AllowPublicBind bool   `toml:"allow_public_bind"`
}

// BuildConfigTOML generates a minimal ZeroClaw bootstrap config for shared-namespace testing.
func BuildConfigTOML(sw *crawblv1alpha1.UserSwarm) string {
	cfg := BootstrapConfig{
		APIKey:             "",
		DefaultProvider:    "openrouter",
		DefaultModel:       "anthropic/claude-sonnet-4-20250514",
		DefaultTemperature: 0.7,
		Gateway: GatewayConfig{
			Port:            runtimePort(sw),
			Host:            "[::]",
			AllowPublicBind: true,
		},
	}

	applyOverrides(&cfg, sw.Spec.Config.Data)

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		panic(fmt.Sprintf("encode zeroclaw bootstrap config: %v", err))
	}
	return buf.String()
}

func applyOverrides(cfg *BootstrapConfig, overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}

	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var doc strings.Builder
	for _, key := range keys {
		value := strings.TrimSpace(overrides[key])
		if value == "" {
			continue
		}
		doc.WriteString(value)
		if !strings.HasSuffix(value, "\n") {
			doc.WriteByte('\n')
		}
	}

	if strings.TrimSpace(doc.String()) == "" {
		return
	}

	if _, err := toml.Decode(doc.String(), cfg); err != nil {
		panic(fmt.Sprintf("decode zeroclaw config overrides: %v", err))
	}
}

func runtimePort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

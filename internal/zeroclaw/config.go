package zeroclaw

import (
	"fmt"
	"sort"
	"strings"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// BuildConfigTOML generates a minimal ZeroClaw config that keeps the gateway reachable for shared-namespace testing.
func BuildConfigTOML(sw *crawblv1alpha1.UserSwarm) string {
	lines := []string{
		`api_key = ""`,
		`default_provider = "openrouter"`,
		`default_model = "anthropic/claude-sonnet-4-20250514"`,
		`default_temperature = 0.7`,
		"",
		"[gateway]",
		fmt.Sprintf("port = %d", runtimePort(sw)),
		`host = "[::]"`,
		`allow_public_bind = true`,
	}

	if len(sw.Spec.Config.Data) == 0 {
		return strings.Join(lines, "\n") + "\n"
	}

	keys := make([]string, 0, len(sw.Spec.Config.Data))
	for key := range sw.Spec.Config.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines = append(lines, "", "# Crawbl-managed overrides")
	for _, key := range keys {
		lines = append(lines, sw.Spec.Config.Data[key])
	}

	return strings.Join(lines, "\n") + "\n"
}

func runtimePort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

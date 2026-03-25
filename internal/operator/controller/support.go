// Package controller provides Kubernetes controller logic.
package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// Probe configuration constants (in seconds).
const (
	healthProbeInitialDelay     = 10
	healthProbePeriod           = 30
	healthProbeTimeout          = 10
	healthProbeFailureThreshold = 3

	startupProbePeriod           = 10
	startupProbeTimeout          = 10
	startupProbeFailureThreshold = 18
)

var requeueSlow = 30 * time.Second

const userSwarmFinalizer = "crawbl.ai/userswarm-protection"

const (
	zeroClawRuntimeUID         int64 = 65532
	zeroClawRuntimeGID         int64 = 65532
	zeroClawBootstrapMode      int32 = 0o444
	kubernetesNameMaxLen             = 63
	workloadResourceNameMaxLen       = 52
	resourceNameHashLen              = 10
)

func desiredRuntimeNamespace(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Placement.RuntimeNamespace != "" {
		return sw.Spec.Placement.RuntimeNamespace
	}
	return crawblv1alpha1.DefaultRuntimeNamespace
}

func runtimeMode(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Runtime.Mode != "" {
		return sw.Spec.Runtime.Mode
	}
	return crawblv1alpha1.DefaultRuntimeMode
}

func runtimePort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

func serviceAccountName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Runtime.ServiceAccountName != "" {
		return sw.Spec.Runtime.ServiceAccountName
	}
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s", sw.Name), workloadResourceNameMaxLen)
}

func workloadName(sw *crawblv1alpha1.UserSwarm) string {
	// StatefulSet names bleed into the generated controller-revision-hash pod label,
	// so they need extra headroom beyond the normal 63-character object-name limit.
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s", sw.Name), workloadResourceNameMaxLen)
}

func headlessServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-headless", sw.Name), kubernetesNameMaxLen)
}

func serviceName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s", sw.Name), workloadResourceNameMaxLen)
}

func configMapName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-config", sw.Name), kubernetesNameMaxLen)
}

func bootstrapConfigVolumeName() string {
	return "bootstrap-config"
}

func envSecretName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Config.EnvSecretRef == nil {
		return ""
	}
	return sw.Spec.Config.EnvSecretRef.Name
}

func usesManagedEnvSecret(sw *crawblv1alpha1.UserSwarm) bool {
	return len(sw.Spec.Config.SecretData) > 0
}

func managedEnvSecretName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-env", sw.Name), kubernetesNameMaxLen)
}

func pvcName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-data", sw.Name), kubernetesNameMaxLen)
}

func (r *UserSwarmReconciler) runtimeVaultEnabled() bool {
	return r.RuntimeVault.EnabledForRuntime()
}

func (r *UserSwarmReconciler) runtimeVaultSecretFilePath() string {
	return path.Join("/vault/secrets", r.RuntimeVault.FileName)
}

func (r *UserSwarmReconciler) runtimeVaultAnnotations() map[string]string {
	if !r.runtimeVaultEnabled() {
		return nil
	}

	annotations := map[string]string{
		"vault.hashicorp.com/agent-inject":                       "true",
		"vault.hashicorp.com/agent-init-first":                   "true",
		"vault.hashicorp.com/agent-pre-populate-only":            fmt.Sprintf("%t", r.RuntimeVault.PrePopulateOnly),
		"vault.hashicorp.com/auth-path":                          r.RuntimeVault.AuthPath,
		"vault.hashicorp.com/role":                               r.RuntimeVault.Role,
		"vault.hashicorp.com/agent-inject-secret-openai-api-key": r.RuntimeVault.SecretPath,
		"vault.hashicorp.com/agent-inject-file-openai-api-key":   r.RuntimeVault.FileName,
		// The bootstrap init container runs as a non-root user and reads the
		// rendered Vault file via OPENAI_API_KEY_FILE. Set an explicit readable
		// mode so the file can be merged into the live config before ZeroClaw starts.
		"vault.hashicorp.com/agent-inject-perms-openai-api-key":    "0444",
		"vault.hashicorp.com/agent-inject-template-openai-api-key": fmt.Sprintf("{{- with secret %q -}}{{ index .Data.data %q }}{{- end -}}", r.RuntimeVault.SecretPath, r.RuntimeVault.SecretKey),
	}

	// Vault injector defaults are fairly heavy for a tiny single-node dev cluster.
	// Set explicit requests/limits so one extra swarm does not get stuck Pending
	// purely because the init injector asked for more CPU than the node can spare.
	if r.RuntimeVault.AgentCPURequest != "" {
		annotations["vault.hashicorp.com/agent-requests-cpu"] = r.RuntimeVault.AgentCPURequest
	}
	if r.RuntimeVault.AgentMemoryRequest != "" {
		annotations["vault.hashicorp.com/agent-requests-mem"] = r.RuntimeVault.AgentMemoryRequest
	}
	if r.RuntimeVault.AgentCPULimit != "" {
		annotations["vault.hashicorp.com/agent-limits-cpu"] = r.RuntimeVault.AgentCPULimit
	}
	if r.RuntimeVault.AgentMemoryLimit != "" {
		annotations["vault.hashicorp.com/agent-limits-mem"] = r.RuntimeVault.AgentMemoryLimit
	}

	return annotations
}

func httpRouteName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-route", sw.Name), kubernetesNameMaxLen)
}

func networkPolicyName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-netpol", sw.Name), kubernetesNameMaxLen)
}

func labelsFor(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "zeroclaw",
		"app.kubernetes.io/component":  "userswarm-runtime",
		"app.kubernetes.io/managed-by": "userswarm-operator",
		"crawbl.ai/userswarm":          sw.Name,
		"crawbl.ai/user-id":            sw.Spec.UserID,
	}
}

func selectorLabelsFor(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "zeroclaw",
		"crawbl.ai/userswarm":    sw.Name,
	}
}

func replicasFor(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Suspend {
		return 0
	}
	return 1
}

func routePath(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.Path != "" {
		return sw.Spec.Exposure.HTTPRoute.Path
	}
	return "/"
}

func routeURL(sw *crawblv1alpha1.UserSwarm) string {
	if !sw.Spec.Exposure.HTTPRoute.Enabled || sw.Spec.Exposure.HTTPRoute.Host == "" {
		return ""
	}
	return fmt.Sprintf("https://%s%s", sw.Spec.Exposure.HTTPRoute.Host, routePath(sw))
}

func routeGatewayName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.GatewayName != "" {
		return sw.Spec.Exposure.HTTPRoute.GatewayName
	}
	return crawblv1alpha1.DefaultPublicGatewayName
}

func routeGatewayNamespace(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.GatewayNamespace != "" {
		return sw.Spec.Exposure.HTTPRoute.GatewayNamespace
	}
	return crawblv1alpha1.DefaultPublicGatewayNamespace
}

func routeSectionName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.ParentSectionName != "" {
		return sw.Spec.Exposure.HTTPRoute.ParentSectionName
	}
	return crawblv1alpha1.DefaultPublicGatewaySection
}

func resourceQuantity(raw string) (resource.Quantity, error) {
	return resource.ParseQuantity(raw)
}

func healthProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/usr/local/bin/zeroclaw", "status", "--format=exit-code"},
			},
		},
		InitialDelaySeconds: healthProbeInitialDelay,
		PeriodSeconds:       healthProbePeriod,
		TimeoutSeconds:      healthProbeTimeout,
		FailureThreshold:    healthProbeFailureThreshold,
	}
}

func startupProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/usr/local/bin/zeroclaw", "status", "--format=exit-code"},
			},
		},
		PeriodSeconds:    startupProbePeriod,
		TimeoutSeconds:   startupProbeTimeout,
		FailureThreshold: startupProbeFailureThreshold,
	}
}

func ptrTo[T any](value T) *T {
	return &value
}

func checksumString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func checksumStringMap(values map[string]string) string {
	if len(values) == 0 {
		return checksumString("")
	}

	// Sort keys first so pod-template checksums stay stable regardless of map iteration order.
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(values[key])
		b.WriteByte('\n')
	}

	return checksumString(b.String())
}

func smokeTestJobName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-smoke", sw.Name), kubernetesNameMaxLen)
}

func smokeTestURL(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/health", serviceName(sw), desiredRuntimeNamespace(sw), runtimePort(sw))
}

func smokeTestSpecChecksum(sw *crawblv1alpha1.UserSwarm) string {
	return checksumString(strings.Join([]string{
		sw.Spec.Runtime.Image,
		runtimeMode(sw),
		smokeTestURL(sw),
		routeURL(sw),
		fmt.Sprintf("%d", sw.Generation),
	}, "\n"))
}

func truncateKubernetesName(value string, maxLen int) string {
	normalized := strings.ToLower(strings.Trim(value, "-"))
	if normalized == "" {
		return "resource"
	}
	if len(normalized) <= maxLen {
		return normalized
	}

	hash := checksumString(normalized)
	if len(hash) > resourceNameHashLen {
		hash = hash[:resourceNameHashLen]
	}

	keepLen := maxLen - len(hash) - 1
	if keepLen < 1 {
		if len(hash) > maxLen {
			return hash[:maxLen]
		}
		return hash
	}

	prefix := strings.Trim(normalized[:keepLen], "-")
	if prefix == "" {
		return hash
	}

	return fmt.Sprintf("%s-%s", prefix, hash)
}

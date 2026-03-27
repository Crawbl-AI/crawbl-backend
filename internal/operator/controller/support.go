// Package controller provides Kubernetes controller logic for the UserSwarm operator.
package controller

// This file contains all the helper functions, naming conventions, constants, and
// utilities used across the reconciler. It's the "glue" — resource name generators,
// label builders, probe configs, checksum functions, and K8s name truncation logic.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// Probe timing constants (in seconds).
// Health probes run after the pod is up; startup probes run during initial boot.
// The startup probe gets 18 * 10s = 180s (3 min) for ZeroClaw to fully initialize,
// which includes downloading models and setting up the workspace.
const (
	healthProbeInitialDelay     = 10
	healthProbePeriod           = 30
	healthProbeTimeout          = 10
	healthProbeFailureThreshold = 3

	startupProbePeriod           = 10
	startupProbeTimeout          = 10
	startupProbeFailureThreshold = 18
)

// requeueSlow is the default requeue interval for non-critical retries.
// 30s keeps us responsive without hammering the API server.
var requeueSlow = 30 * time.Second

// userSwarmFinalizer prevents the CR from being deleted until we've cleaned up
// all child resources in the runtime namespace.
const userSwarmFinalizer = "crawbl.ai/userswarm-protection"

// ZeroClaw runtime security constants.
// UID/GID 65532 is the standard "nonroot" user in distroless images.
const (
	zeroClawRuntimeUID         int64 = 65532
	zeroClawRuntimeGID         int64 = 65532
	zeroClawBootstrapMode      int32 = 0o444 // world-readable, nobody can write
	kubernetesNameMaxLen             = 63     // K8s object names can't exceed 63 chars
	workloadResourceNameMaxLen       = 52     // StatefulSet names need extra headroom for the controller-revision-hash label
	resourceNameHashLen              = 10     // how many chars of the hash suffix to keep when truncating
)

// --- Resource name generators ---
// All child resources follow the naming pattern "zeroclaw-{swarm.Name}[-suffix]".
// Names are truncated with a hash suffix if they'd exceed K8s limits.

// desiredRuntimeNamespace returns the namespace where this swarm's runtime resources live.
// Defaults to the shared runtime namespace if not explicitly overridden on the CR.
func desiredRuntimeNamespace(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Placement.RuntimeNamespace != "" {
		return sw.Spec.Placement.RuntimeNamespace
	}
	return crawblv1alpha1.DefaultRuntimeNamespace
}

// runtimeMode returns the ZeroClaw runtime mode (e.g. "gateway", "worker").
func runtimeMode(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Runtime.Mode != "" {
		return sw.Spec.Runtime.Mode
	}
	return crawblv1alpha1.DefaultRuntimeMode
}

// runtimePort returns the port ZeroClaw's gateway listens on.
func runtimePort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

// serviceAccountName returns the name for this swarm's K8s ServiceAccount.
func serviceAccountName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Runtime.ServiceAccountName != "" {
		return sw.Spec.Runtime.ServiceAccountName
	}
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s", sw.Name), workloadResourceNameMaxLen)
}

// workloadName returns the StatefulSet name.
// Uses the shorter max length because StatefulSet names bleed into the
// auto-generated controller-revision-hash pod label, which also has a 63-char limit.
func workloadName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s", sw.Name), workloadResourceNameMaxLen)
}

// headlessServiceName returns the name for the headless Service (required by StatefulSet).
func headlessServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-headless", sw.Name), kubernetesNameMaxLen)
}

// serviceName returns the name for the ClusterIP Service (the main traffic entry point).
func serviceName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s", sw.Name), workloadResourceNameMaxLen)
}

// configMapName returns the name for the bootstrap ConfigMap.
func configMapName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-config", sw.Name), kubernetesNameMaxLen)
}

// bootstrapConfigVolumeName returns the volume name for the bootstrap config mount.
// This is a static name since it's only used within a single pod template.
func bootstrapConfigVolumeName() string {
	return "bootstrap-config"
}

// envSecretName returns the name of the externally-managed env secret (from spec.config.envSecretRef).
// Returns empty string if no external secret is configured.
func envSecretName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Config.EnvSecretRef == nil {
		return ""
	}
	return sw.Spec.Config.EnvSecretRef.Name
}

// usesManagedEnvSecret returns true if the CR has inline secretData that needs
// a managed K8s Secret. This is the deprecated path — prefer envSecretRef.
func usesManagedEnvSecret(sw *crawblv1alpha1.UserSwarm) bool {
	return len(sw.Spec.Config.SecretData) > 0
}

// managedEnvSecretName returns the name for the deprecated managed env Secret.
func managedEnvSecretName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-env", sw.Name), kubernetesNameMaxLen)
}

// pvcName returns the name for the user's PersistentVolumeClaim.
func pvcName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-data", sw.Name), kubernetesNameMaxLen)
}


// httpRouteName returns the name for the optional public HTTPRoute.
func httpRouteName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-route", sw.Name), kubernetesNameMaxLen)
}

// networkPolicyName returns the name for the ingress NetworkPolicy.
func networkPolicyName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-netpol", sw.Name), kubernetesNameMaxLen)
}

// --- Label helpers ---

// labelsFor returns the full label set applied to all child resources.
// Includes standard K8s recommended labels plus Crawbl-specific ones for
// filtering by swarm name and user ID.
func labelsFor(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "zeroclaw",
		"app.kubernetes.io/component":  "userswarm-runtime",
		"app.kubernetes.io/managed-by": "userswarm-operator",
		"crawbl.ai/userswarm":          sw.Name,
		"crawbl.ai/user-id":            sw.Spec.UserID,
	}
}

// selectorLabelsFor returns the minimal label set used for pod selectors.
// This is intentionally smaller than labelsFor — selectors are immutable on
// Services and StatefulSets, so we only include the stable identity labels.
func selectorLabelsFor(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "zeroclaw",
		"crawbl.ai/userswarm":    sw.Name,
	}
}

// replicasFor returns the desired replica count. Suspended swarms get 0 replicas,
// which scales down the StatefulSet without deleting the PVC or other resources.
func replicasFor(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Suspend {
		return 0
	}
	return 1
}

// --- Route helpers ---

// routePath returns the path match for the HTTPRoute. Defaults to "/" (match everything).
func routePath(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.Path != "" {
		return sw.Spec.Exposure.HTTPRoute.Path
	}
	return "/"
}

// routeURL builds the public URL for this swarm (empty if routing is disabled).
// Used to populate .status.url so the mobile app knows where to reach the swarm.
func routeURL(sw *crawblv1alpha1.UserSwarm) string {
	if !sw.Spec.Exposure.HTTPRoute.Enabled || sw.Spec.Exposure.HTTPRoute.Host == "" {
		return ""
	}
	return fmt.Sprintf("https://%s%s", sw.Spec.Exposure.HTTPRoute.Host, routePath(sw))
}

// routeGatewayName returns the name of the shared Gateway to attach the HTTPRoute to.
func routeGatewayName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.GatewayName != "" {
		return sw.Spec.Exposure.HTTPRoute.GatewayName
	}
	return crawblv1alpha1.DefaultPublicGatewayName
}

// routeGatewayNamespace returns the namespace of the shared Gateway.
func routeGatewayNamespace(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.GatewayNamespace != "" {
		return sw.Spec.Exposure.HTTPRoute.GatewayNamespace
	}
	return crawblv1alpha1.DefaultPublicGatewayNamespace
}

// routeSectionName returns the Gateway listener section to attach to.
func routeSectionName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.HTTPRoute.ParentSectionName != "" {
		return sw.Spec.Exposure.HTTPRoute.ParentSectionName
	}
	return crawblv1alpha1.DefaultPublicGatewaySection
}

// --- K8s utility helpers ---

// resourceQuantity parses a string like "1Gi" into a K8s resource.Quantity.
func resourceQuantity(raw string) (resource.Quantity, error) {
	return resource.ParseQuantity(raw)
}

// healthProbe returns the readiness/liveness probe config for the ZeroClaw container.
// Uses the zeroclaw CLI's status command instead of an HTTP probe because ZeroClaw's
// health endpoint might not be on the same port or might need special handling.
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

// startupProbe returns the startup probe config. This is more generous than the health
// probe — it gives ZeroClaw up to 3 minutes to finish initializing before K8s kills it.
// ZeroClaw needs time to download models, set up the workspace, etc. on first boot.
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

// --- Generic utilities ---

// ptrTo returns a pointer to the given value. Used everywhere K8s APIs need *int32, *bool, etc.
func ptrTo[T any](value T) *T {
	return &value
}

// checksumString returns a hex-encoded SHA-256 hash of a single string.
func checksumString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// checksumStringMap returns a deterministic checksum of a string map.
// Keys are sorted first so the checksum is stable regardless of Go's random map iteration order.
// Used for pod-template annotations that trigger rolling updates when config changes.
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

// --- Backup helpers ---

// backupJobName returns the name for the periodic backup K8s Job.
func backupJobName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-backup", sw.Name), kubernetesNameMaxLen)
}

// finalBackupJobName returns the name for the pre-deletion final backup K8s Job.
func finalBackupJobName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-final-backup", sw.Name), kubernetesNameMaxLen)
}

// --- Smoke test helpers ---

// smokeTestJobName returns the name for the smoke test K8s Job.
func smokeTestJobName(sw *crawblv1alpha1.UserSwarm) string {
	return truncateKubernetesName(fmt.Sprintf("zeroclaw-%s-smoke", sw.Name), kubernetesNameMaxLen)
}

// smokeTestURL returns the cluster-internal URL the smoke test hits.
// It goes through the ClusterIP service, not directly to the pod — this verifies
// the full service path works end-to-end.
func smokeTestURL(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/health", serviceName(sw), desiredRuntimeNamespace(sw), runtimePort(sw))
}

// smokeTestSpecChecksum computes a hash of all inputs that should trigger a new smoke test
// when they change. This includes the runtime image, mode, service URL, route URL, and
// the CR's generation (so any spec change reruns the test).
func smokeTestSpecChecksum(sw *crawblv1alpha1.UserSwarm) string {
	return checksumString(strings.Join([]string{
		sw.Spec.Runtime.Image,
		runtimeMode(sw),
		smokeTestURL(sw),
		routeURL(sw),
		fmt.Sprintf("%d", sw.Generation),
	}, "\n"))
}

// truncateKubernetesName safely shortens a resource name to fit within K8s limits.
// If the name is already short enough, it's returned as-is. Otherwise, we keep a
// prefix and append a hash suffix to ensure uniqueness. This prevents collisions
// when two long swarm names would otherwise truncate to the same prefix.
func truncateKubernetesName(value string, maxLen int) string {
	normalized := strings.ToLower(strings.Trim(value, "-"))
	if normalized == "" {
		return "resource"
	}
	if len(normalized) <= maxLen {
		return normalized
	}

	// Take a hash of the full name to use as a unique suffix.
	hash := checksumString(normalized)
	if len(hash) > resourceNameHashLen {
		hash = hash[:resourceNameHashLen]
	}

	// Keep as much of the original name as we can, then append the hash.
	keepLen := maxLen - len(hash) - 1 // -1 for the separator dash
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

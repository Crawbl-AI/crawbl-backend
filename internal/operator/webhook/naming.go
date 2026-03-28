package webhook

// This file defines how child resources are named, labeled, and configured.
//
// Naming convention:
//   All child resources follow the pattern "zeroclaw-{swarm.Name}[-suffix]".
//   Names are truncated with a hash suffix if they'd exceed K8s limits (63 chars
//   for most resources, 52 for StatefulSets which need headroom for pod suffixes).
//
// Labels:
//   allLabels()      — full set applied to every child resource.
//   selectorLabels() — minimal immutable set used in Service/StatefulSet selectors.
//
// Spec accessors:
//   Pure functions that read a field from the UserSwarm spec with a default fallback.
//   These are small but important — they centralize default logic so the child
//   builders don't each need to handle "what if this field is empty?".

import (
	"fmt"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
)

// ---------------------------------------------------------------------------
// Resource names
//
// Each function returns the K8s name for one child resource type.
// All are deterministic and safe to call repeatedly for the same swarm.
// ---------------------------------------------------------------------------

// ServiceAccountName returns the name for this swarm's per-user ServiceAccount.
func ServiceAccountName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s", sw.Name), kube.MaxWorkloadNameLen)
}

// ConfigMapName returns the name for the bootstrap ConfigMap.
func ConfigMapName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-config", sw.Name), kube.MaxNameLen)
}

// PVCName returns the name for the user's PersistentVolumeClaim.
func PVCName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-data", sw.Name), kube.MaxNameLen)
}

// HeadlessServiceName returns the name for the headless Service (required by StatefulSet).
func HeadlessServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-headless", sw.Name), kube.MaxNameLen)
}

// ServiceName returns the name for the ClusterIP Service (main traffic entry point).
func ServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s", sw.Name), kube.MaxWorkloadNameLen)
}

// StatefulSetName returns the name for the StatefulSet.
func StatefulSetName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s", sw.Name), kube.MaxWorkloadNameLen)
}

// BackupJobName returns the name for the periodic backup Job.
func BackupJobName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-backup", sw.Name), kube.MaxNameLen)
}

// ---------------------------------------------------------------------------
// Spec accessors with defaults
//
// These extract values from the UserSwarm spec, falling back to CRD-level
// defaults when the field is empty/zero. Unit-testable by constructing
// a UserSwarm with various field combinations.
// ---------------------------------------------------------------------------

// RuntimeNamespace returns the namespace where child resources are created.
// Defaults to "userswarms" if not specified on the CR.
func RuntimeNamespace(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Placement.RuntimeNamespace != "" {
		return sw.Spec.Placement.RuntimeNamespace
	}
	return crawblv1alpha1.DefaultRuntimeNamespace
}

// RuntimePort returns the port ZeroClaw's gateway listens on.
// Defaults to 42617 if not specified.
func RuntimePort(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

// RuntimeMode returns the ZeroClaw runtime mode (e.g. "daemon", "gateway").
// Defaults to "daemon" if not specified.
func RuntimeMode(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Runtime.Mode != "" {
		return sw.Spec.Runtime.Mode
	}
	return crawblv1alpha1.DefaultRuntimeMode
}

// EnvSecretRef returns the name of the externally-managed Secret containing
// provider API keys and other sensitive env vars. Returns "" if not configured.
func EnvSecretRef(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Config.EnvSecretRef == nil {
		return ""
	}
	return sw.Spec.Config.EnvSecretRef.Name
}

// ReplicaCount returns 0 if the swarm is suspended, 1 otherwise.
// Suspended swarms keep their PVC and Services but scale the StatefulSet to zero.
func ReplicaCount(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Suspend {
		return 0
	}
	return 1
}

// ---------------------------------------------------------------------------
// Labels
//
// Two label sets with different purposes:
//   - allLabels: full set for resource identification and filtering.
//   - selectorLabels: minimal immutable set for Service/StatefulSet selectors.
//     Must never change after creation (K8s enforces this).
// ---------------------------------------------------------------------------

// AllLabels returns the full label set applied to every child resource.
// Includes standard K8s recommended labels plus Crawbl-specific ones.
func AllLabels(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "zeroclaw",
		"app.kubernetes.io/component":  "userswarm-runtime",
		"app.kubernetes.io/managed-by": "metacontroller",
		"crawbl.ai/userswarm":          sw.Name,
		"crawbl.ai/user-id":            sw.Spec.UserID,
	}
}

// SelectorLabels returns the minimal label set used in Service and StatefulSet
// selectors. These are immutable — once set on a Service or StatefulSet, K8s
// won't let you change them. Only include the stable identity fields.
func SelectorLabels(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "zeroclaw",
		"crawbl.ai/userswarm":    sw.Name,
	}
}

// ---------------------------------------------------------------------------
// Internal aliases used by children.go for brevity.
// These keep the child builders readable without exporting every name helper.
// ---------------------------------------------------------------------------

func runtimeNS(sw *crawblv1alpha1.UserSwarm) string   { return RuntimeNamespace(sw) }
func runtimePort(sw *crawblv1alpha1.UserSwarm) int32   { return RuntimePort(sw) }
func runtimeMode(sw *crawblv1alpha1.UserSwarm) string  { return RuntimeMode(sw) }
func envSecretRef(sw *crawblv1alpha1.UserSwarm) string { return EnvSecretRef(sw) }

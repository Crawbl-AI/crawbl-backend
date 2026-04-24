package webhook

// This file contains reusable Kubernetes struct builders.
// These are small helpers that construct common K8s objects with sensible defaults.
// They eliminate repetitive boilerplate across webhook handlers and controllers.

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TypeMeta builds a Kubernetes TypeMeta from apiVersion and kind strings.
// Required by Metacontroller — it reads apiVersion + kind from each child
// to determine which resource type to create/update.
func TypeMeta(apiVersion, kind string) metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: apiVersion, Kind: kind}
}

// RestrictedSecurityContext returns a container SecurityContext that follows
// best practices for non-root workloads:
//   - No privilege escalation
//   - Runs as non-root with explicit UID/GID
//   - All Linux capabilities dropped
//
// The uid and gid parameters let callers specify the runtime user.
// For distroless images, this is typically 65532 (the "nonroot" user).
func RestrictedSecurityContext(uid, gid int64) *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		RunAsNonRoot:             ptr.To(true),
		RunAsUser:                ptr.To(uid),
		RunAsGroup:               ptr.To(gid),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

// StatusCondition builds a Kubernetes-style status condition as a generic map.
// This is the format Metacontroller expects when setting conditions on a CR's status.
// For typed conditions, use metav1.Condition directly.
func StatusCondition(typ, status, reason, message string) map[string]any {
	c := map[string]any{
		"type":   typ,
		"status": status,
		"reason": reason,
	}
	if message != "" {
		c["message"] = message
	}
	return c
}

// SecretEnvFrom builds an EnvFromSource that injects all keys from a Kubernetes
// Secret as environment variables. Used to mount provider API keys and other
// sensitive config into containers without putting them in ConfigMaps.
func SecretEnvFrom(secretName string) []corev1.EnvFromSource {
	return []corev1.EnvFromSource{{
		SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		},
	}}
}

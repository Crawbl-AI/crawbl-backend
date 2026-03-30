package webhook

import (
	"fmt"
	"strings"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
)

// This file centralizes everything that gives the runtime graph its stable shape:
// names, labels, selectors, and spec-default accessors.

func workspaceIDFromSwarmName(name string) string {
	return strings.TrimPrefix(name, "workspace-")
}

func runtimeServiceAccountName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s", sw.Name), kube.MaxWorkloadNameLen)
}

func runtimeConfigName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-config", sw.Name), kube.MaxNameLen)
}

func workspacePVCName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-data", sw.Name), kube.MaxNameLen)
}

func headlessNetworkName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-headless", sw.Name), kube.MaxNameLen)
}

func runtimeServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s", sw.Name), kube.MaxWorkloadNameLen)
}

func runtimeStatefulSetName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s", sw.Name), kube.MaxWorkloadNameLen)
}

func backupJobName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf("zeroclaw-%s-backup", sw.Name), kube.MaxNameLen)
}

func runtimeNamespaceFor(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Placement.RuntimeNamespace != "" {
		return sw.Spec.Placement.RuntimeNamespace
	}
	return crawblv1alpha1.DefaultRuntimeNamespace
}

func runtimePortFor(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Runtime.Port != 0 {
		return sw.Spec.Runtime.Port
	}
	return crawblv1alpha1.DefaultGatewayPort
}

func runtimeModeFor(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Runtime.Mode != "" {
		return sw.Spec.Runtime.Mode
	}
	return crawblv1alpha1.DefaultRuntimeMode
}

func runtimeEnvSecretName(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Config.EnvSecretRef == nil {
		return ""
	}
	return sw.Spec.Config.EnvSecretRef.Name
}

func runtimeReplicaCount(sw *crawblv1alpha1.UserSwarm) int32 {
	if sw.Spec.Suspend {
		return 0
	}
	return 1
}

func runtimeLabels(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "zeroclaw",
		"app.kubernetes.io/component":  "userswarm-runtime",
		"app.kubernetes.io/managed-by": "metacontroller",
		"crawbl.ai/userswarm":          sw.Name,
		"crawbl.ai/user-id":            sw.Spec.UserID,
	}
}

func selectorLabels(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "zeroclaw",
		"crawbl.ai/userswarm":    sw.Name,
	}
}

package webhook

import (
	"fmt"
	"strings"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
)

// This file centralizes everything that gives the runtime graph its stable
// shape: names, labels, selectors, and spec-default accessors. All name
// helpers use the "agent-runtime-" prefix so generated resources are easy
// to eyeball in kubectl output.

const (
	// runtimeAppName is the app.kubernetes.io/name label applied to every
	// child resource. Used by the selector on Service and Deployment.
	runtimeAppName = "crawbl-agent-runtime"

	// defaultAgentRuntimeImage is the fallback image used when the webhook
	// cannot derive the agent-runtime image from the userswarm spec. Points
	// at the dev tag so local clusters work without extra configuration.
	defaultAgentRuntimeImage = "registry.digitalocean.com/crawbl/crawbl-agent-runtime:dev"

	// agentRuntimeNameFormat is the fmt.Sprintf format for all agent-runtime
	// child resource names (ServiceAccount, Service, Deployment). The single
	// %s placeholder is replaced with the UserSwarm CR name.
	agentRuntimeNameFormat = "agent-runtime-%s"
)

// workspaceIDFromSwarmName strips the "workspace-" prefix off a CR name to
// recover the bare workspace ID. EnsureRuntime in the client package does
// the inverse.
func workspaceIDFromSwarmName(name string) string {
	return strings.TrimPrefix(name, "workspace-")
}

func runtimeServiceAccountName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf(agentRuntimeNameFormat, sw.Name), kube.MaxWorkloadNameLen)
}

func runtimeServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf(agentRuntimeNameFormat, sw.Name), kube.MaxWorkloadNameLen)
}

func runtimeDeploymentName(sw *crawblv1alpha1.UserSwarm) string {
	return kube.TruncateName(fmt.Sprintf(agentRuntimeNameFormat, sw.Name), kube.MaxWorkloadNameLen)
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
		"app.kubernetes.io/name":       runtimeAppName,
		"app.kubernetes.io/component":  "userswarm-runtime",
		"app.kubernetes.io/managed-by": "metacontroller",
		"crawbl.ai/userswarm":          sw.Name,
		"crawbl.ai/user-id":            sw.Spec.UserID,
	}
}

func selectorLabels(sw *crawblv1alpha1.UserSwarm) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": runtimeAppName,
		"crawbl.ai/userswarm":    sw.Name,
	}
}

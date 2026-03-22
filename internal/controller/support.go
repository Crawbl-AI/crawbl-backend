package controller

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

var requeueSlow = 30 * time.Second

const userSwarmFinalizer = "crawbl.ai/userswarm-protection"

const (
	zeroClawRuntimeUID int64 = 65532
	zeroClawRuntimeGID int64 = 65532
	zeroClawConfigMode int32 = 0o440
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
	return workloadName(sw)
}

func workloadName(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("zeroclaw-%s", sw.Name)
}

func headlessServiceName(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("%s-headless", workloadName(sw))
}

func serviceName(sw *crawblv1alpha1.UserSwarm) string {
	return workloadName(sw)
}

func configMapName(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("%s-config", workloadName(sw))
}

func secretName(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("%s-env", workloadName(sw))
}

func pvcName(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("%s-data", workloadName(sw))
}

func ingressName(sw *crawblv1alpha1.UserSwarm) string {
	return fmt.Sprintf("%s-ingress", workloadName(sw))
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

func ingressPath(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Exposure.Ingress.Path != "" {
		return sw.Spec.Exposure.Ingress.Path
	}
	return "/"
}

func ingressURL(sw *crawblv1alpha1.UserSwarm) string {
	if !sw.Spec.Exposure.Ingress.Enabled || sw.Spec.Exposure.Ingress.Host == "" {
		return ""
	}
	return fmt.Sprintf("https://%s%s", sw.Spec.Exposure.Ingress.Host, ingressPath(sw))
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
		InitialDelaySeconds: 10,
		PeriodSeconds:       30,
		TimeoutSeconds:      10,
		FailureThreshold:    3,
	}
}

func ptrTo[T any](value T) *T {
	return &value
}

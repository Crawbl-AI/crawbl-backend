package webhook

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// This file holds the non-pod parts of the desired runtime graph:
// identity and the single ClusterIP Service that fronts the runtime pod.
// There is no PVC, no backup Job, no bootstrap ConfigMap — the agent
// runtime binary takes all of its configuration from CLI flags plus the
// envSecretRef Secret projected onto the container.

func buildServiceAccount(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		TypeMeta:   TypeMeta("v1", "ServiceAccount"),
		ObjectMeta: objectMeta(runtimeServiceAccountName(sw), ns, sw),
	}
	if pullSecret := sw.Spec.Runtime.ImagePullSecretName; pullSecret != "" {
		sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
	}
	return sa
}

// buildRuntimeNetwork emits the ClusterIP Service that the orchestrator
// dials for gRPC (Converse + Memory). The Service exposes exactly one
// port — the gRPC listener — because the HTTP webhook path is gone.
func buildRuntimeNetwork(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.Service {
	port := runtimePortFor(sw)
	return &corev1.Service{
		TypeMeta:   TypeMeta("v1", "Service"),
		ObjectMeta: objectMeta(runtimeServiceName(sw), ns, sw),
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabels(sw),
			Ports: []corev1.ServicePort{{
				Name:        "grpc",
				Port:        port,
				TargetPort:  intstr.FromInt32(port),
				Protocol:    corev1.ProtocolTCP,
				AppProtocol: func() *string { s := "grpc"; return &s }(),
			}},
		},
	}
}

func objectMeta(name, ns string, sw *crawblv1alpha1.UserSwarm) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: runtimeLabels(sw)}
}

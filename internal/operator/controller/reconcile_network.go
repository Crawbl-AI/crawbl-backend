package controller

// This file manages NetworkPolicy for UserSwarm pods. The policy locks down
// ingress so only the backend namespace (where the orchestrator lives) and
// sibling pods in the same swarm can reach the runtime port. Everything else
// is blocked — no cross-user pod access, no random namespace traffic.

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// reconcileNetworkPolicy creates or updates the NetworkPolicy that restricts
// who can talk to this user's ZeroClaw pod.
//
// Ingress is allowed from exactly two sources:
// 1. The "backend" namespace — so the orchestrator API can proxy requests.
// 2. Pods with the same swarm selector — so pods within the same StatefulSet
//    can communicate (useful if we ever scale beyond 1 replica).
//
// Egress is not restricted — ZeroClaw needs outbound access for LLM APIs,
// tool execution, etc.
func (r *UserSwarmReconciler) reconcileNetworkPolicy(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkPolicyName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		// Only applies to pods matching this specific user's swarm labels.
		obj.Spec.PodSelector = metav1.LabelSelector{
			MatchLabels: selectorLabelsFor(swarm),
		}
		// We only define Ingress rules — absence of Egress in PolicyTypes means
		// all outbound traffic is allowed by default.
		obj.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		obj.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
			From: []networkingv1.NetworkPolicyPeer{
				{
					// Allow traffic from the backend namespace (orchestrator).
					// Uses the well-known metadata.name label that K8s auto-applies.
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "backend",
						},
					},
				},
				{
					// Allow traffic from sibling pods in the same swarm.
					PodSelector: &metav1.LabelSelector{
						MatchLabels: selectorLabelsFor(swarm),
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{{
				Port: ptrTo(intstr.FromInt32(runtimePort(swarm))),
			}},
		}}

		return nil
	})
	return err
}

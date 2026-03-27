package controller

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

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

		obj.Spec.PodSelector = metav1.LabelSelector{
			MatchLabels: selectorLabelsFor(swarm),
		}
		obj.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		obj.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
			From: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "backend",
						},
					},
				},
				{
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

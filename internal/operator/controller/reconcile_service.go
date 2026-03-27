package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

func (r *UserSwarmReconciler) reconcileHeadlessService(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessServiceName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Spec.ClusterIP = corev1.ClusterIPNone
		obj.Spec.PublishNotReadyAddresses = true
		obj.Spec.Selector = selectorLabelsFor(swarm)
		obj.Spec.Ports = []corev1.ServicePort{{
			Name:       "http",
			Port:       runtimePort(swarm),
			TargetPort: intstr.FromInt32(runtimePort(swarm)),
		}}
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileService(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Spec.Type = corev1.ServiceTypeClusterIP
		obj.Spec.Selector = selectorLabelsFor(swarm)
		obj.Spec.Ports = []corev1.ServicePort{{
			Name:       "http",
			Port:       runtimePort(swarm),
			TargetPort: intstr.FromInt32(runtimePort(swarm)),
		}}
		return nil
	})
	return err
}

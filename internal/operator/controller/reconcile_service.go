package controller

// This file manages the two Services each UserSwarm needs:
// 1. A headless service — required by the StatefulSet for stable pod DNS names.
// 2. A ClusterIP service — the main entry point the orchestrator and smoke tests use
//    to reach the ZeroClaw runtime over the internal network.

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// reconcileHeadlessService creates the headless service that the StatefulSet requires.
// Without a headless service, StatefulSet pods can't get stable DNS names like
// zeroclaw-<user>-0.zeroclaw-<user>-headless.<ns>.svc.cluster.local — and the
// StatefulSet controller itself won't create pods without one.
//
// PublishNotReadyAddresses is true so DNS entries exist even during startup,
// which helps the init container and probes resolve before the pod is fully ready.
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
		// ClusterIP=None makes it headless — no virtual IP, just DNS records for each pod.
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

// reconcileService creates the ClusterIP service that the orchestrator uses to
// reach this user's ZeroClaw runtime. This is the service the smoke test hits
// and the one the backend proxies chat requests through.
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

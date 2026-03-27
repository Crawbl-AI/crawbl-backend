package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

func (r *UserSwarmReconciler) cleanupManagedResources(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (bool, error) {
	runtimeNamespace := desiredRuntimeNamespace(swarm)

	var namespace corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: runtimeNamespace}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	objects := []client.Object{
		&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: httpRouteName(swarm), Namespace: runtimeNamespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeTestJobName(swarm), Namespace: runtimeNamespace}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: workloadName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: headlessServiceName(swarm), Namespace: runtimeNamespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName(swarm), Namespace: runtimeNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName(swarm), Namespace: runtimeNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName(swarm), Namespace: runtimeNamespace}},
	}
	if usesManagedEnvSecret(swarm) {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: managedEnvSecretName(swarm), Namespace: runtimeNamespace}})
	}

	// Delete explicitly so the finalizer can report progress even when child cleanup spans multiple reconciles.
	pending := false
	for _, obj := range objects {
		key := client.ObjectKeyFromObject(obj)
		if err := r.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}

		pending = true
		if obj.GetDeletionTimestamp().IsZero() {
			if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return pending, nil
}

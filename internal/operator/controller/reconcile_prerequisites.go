package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

func (r *UserSwarmReconciler) ensureRuntimeNamespaceExists(ctx context.Context, name string) error {
	var ns corev1.Namespace
	return r.Get(ctx, types.NamespacedName{Name: name}, &ns)
}

func (r *UserSwarmReconciler) ensureImagePullSecretExists(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if swarm.Spec.Runtime.ImagePullSecretName == "" {
		return nil
	}

	// Shared pull secrets are expected to be provisioned ahead of time in the runtime namespace.
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: desiredRuntimeNamespace(swarm),
		Name:      swarm.Spec.Runtime.ImagePullSecretName,
	}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("runtime namespace %q is missing image pull secret %q", desiredRuntimeNamespace(swarm), swarm.Spec.Runtime.ImagePullSecretName)
		}
		return err
	}
	return nil
}

func (r *UserSwarmReconciler) ensureRuntimeSecretExists(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	name := envSecretName(swarm)
	if name == "" && usesManagedEnvSecret(swarm) {
		name = managedEnvSecretName(swarm)
	}
	if name == "" {
		return nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: desiredRuntimeNamespace(swarm),
		Name:      name,
	}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("runtime namespace %q is missing env secret %q", desiredRuntimeNamespace(swarm), name)
		}
		return err
	}
	return nil
}

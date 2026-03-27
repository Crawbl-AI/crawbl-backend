package controller

// This file validates shared prerequisites that must exist BEFORE we create any
// per-user workload resources. These are things the operator doesn't own — the
// runtime namespace, image pull secrets, and env secrets are provisioned externally
// (by ArgoCD, ESO, or an admin). If any are missing, the reconcile loop bails
// early and requeues slowly until someone fixes it.

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// ensureRuntimeNamespaceExists checks that the shared runtime namespace already exists.
// We don't create it — that's ArgoCD's job. If it's missing, we can't do anything.
func (r *UserSwarmReconciler) ensureRuntimeNamespaceExists(ctx context.Context, name string) error {
	var ns corev1.Namespace
	return r.Get(ctx, types.NamespacedName{Name: name}, &ns)
}

// ensureImagePullSecretExists verifies the image pull secret is present in the runtime
// namespace. This secret is needed for pulling the ZeroClaw container image from a
// private registry. If no pull secret is configured on the CR, we skip the check.
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

// ensureRuntimeSecretExists verifies the env secret (provider keys, credentials, etc.)
// exists in the runtime namespace. This can come from either an explicit envSecretRef
// on the CR, or from the deprecated inline secretData field which creates a managed secret.
// If neither is configured, we skip the check — the runtime just won't have any secret env.
func (r *UserSwarmReconciler) ensureRuntimeSecretExists(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	// Prefer the explicit external secret ref; fall back to the deprecated managed secret name.
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

package controller

// This file handles the "config layer" of a UserSwarm runtime: service account,
// bootstrap ConfigMap, and the deprecated managed env secret. These are all
// lightweight resources that need to exist before the StatefulSet can reference them.

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

// reconcileServiceAccount ensures the per-user service account exists.
// It also wires up the image pull secret reference so pods automatically
// get the right credentials when pulling from private registries.
func (r *UserSwarmReconciler) reconcileServiceAccount(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		if pullSecret := swarm.Spec.Runtime.ImagePullSecretName; pullSecret != "" {
			obj.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
		} else {
			obj.ImagePullSecrets = nil
		}
		return nil
	})
	return err
}

// reconcileConfigMap creates or updates the bootstrap ConfigMap that contains
// ZeroClaw configuration files (config.toml, SOUL.md, IDENTITY.md, etc.).
// The init container copies these into the PVC on first boot, and subsequent
// reconciles only update operator-managed keys without clobbering ZeroClaw state.
func (r *UserSwarmReconciler) reconcileConfigMap(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		// Build the full set of bootstrap files from the CR spec + shared ZeroClaw config.
		bootstrapFiles, err := zeroclaw.BuildBootstrapFiles(swarm, r.ZeroClawConfig)
		if err != nil {
			return err
		}
		obj.Data = bootstrapFiles
		return nil
	})
	return err
}

// reconcileDeprecatedManagedSecret handles the legacy path where secret env vars are
// inlined directly in the CR's spec.config.secretData field. This creates a K8s Secret
// that gets mounted into the runtime containers via envFrom.
//
// This is deprecated — the preferred path is spec.config.envSecretRef pointing to an
// ESO-managed external secret. But we still support it for backward compatibility.
func (r *UserSwarmReconciler) reconcileDeprecatedManagedSecret(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if !usesManagedEnvSecret(swarm) {
		return nil
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedEnvSecretName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Type = corev1.SecretTypeOpaque
		obj.StringData = swarm.Spec.Config.SecretData
		return nil
	})
	return err
}

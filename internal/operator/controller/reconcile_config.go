package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

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
		bootstrapFiles, err := zeroclaw.BuildBootstrapFiles(swarm, r.ZeroClawConfig)
		if err != nil {
			return err
		}
		obj.Data = bootstrapFiles
		return nil
	})
	return err
}

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

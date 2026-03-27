package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

//nolint:cyclop
func (r *UserSwarmReconciler) reconcileStatefulSet(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		replicas := replicasFor(swarm)
		obj.Spec.Replicas = &replicas
		obj.Spec.ServiceName = headlessServiceName(swarm)
		obj.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabelsFor(swarm)}
		obj.Spec.Template.Labels = labelsFor(swarm)
		bootstrapFiles, err := zeroclaw.BuildBootstrapFiles(swarm, r.ZeroClawConfig)
		if err != nil {
			return err
		}
		envSecretRefName := envSecretName(swarm)
		if envSecretRefName == "" && usesManagedEnvSecret(swarm) {
			envSecretRefName = managedEnvSecretName(swarm)
		}
		// Roll the pod only when bootstrap inputs change; the live config itself stays on the PVC.
		obj.Spec.Template.Annotations = map[string]string{
			"crawbl.ai/config-checksum": checksumStringMap(bootstrapFiles),
			"crawbl.ai/env-secret-ref":  checksumString(envSecretRefName),
		}
		obj.Spec.Template.Spec.ServiceAccountName = serviceAccountName(swarm)
		obj.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot:        ptrTo(true),
			RunAsUser:           ptrTo(zeroClawRuntimeUID),
			RunAsGroup:          ptrTo(zeroClawRuntimeGID),
			FSGroup:             ptrTo(zeroClawRuntimeGID),
			FSGroupChangePolicy: ptrTo(corev1.FSGroupChangeOnRootMismatch),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}

		obj.Spec.Template.Spec.InitContainers = []corev1.Container{buildInitContainer(r.BootstrapImage, envSecretRefName)}
		obj.Spec.Template.Spec.Containers = []corev1.Container{buildRuntimeContainer(swarm, envSecretRefName)}

		obj.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName(swarm),
					},
				},
			},
			{
				Name: bootstrapConfigVolumeName(),
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: configMapName(swarm)},
						DefaultMode:          ptrTo(zeroClawBootstrapMode),
					},
				},
			},
		}

		return nil
	})
	return err
}

func buildInitContainer(bootstrapImage, envSecretRefName string) corev1.Container {
	c := corev1.Container{
		Name:            "bootstrap-config",
		Image:           bootstrapImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/userswarm-operator", "bootstrap"},
		Args: []string{
			"--bootstrap-config=/bootstrap/config.toml",
			"--live-config=/zeroclaw-data/.zeroclaw/config.toml",
			"--workspace=/zeroclaw-data/workspace",
		},
		// Merge only operator-managed config keys into the PVC-backed live config and preserve ZeroClaw state.
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptrTo(false),
			RunAsNonRoot:             ptrTo(true),
			RunAsUser:                ptrTo(zeroClawRuntimeUID),
			RunAsGroup:               ptrTo(zeroClawRuntimeGID),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "data",
				MountPath: "/zeroclaw-data",
			},
			{
				Name:      bootstrapConfigVolumeName(),
				MountPath: "/bootstrap",
				ReadOnly:  true,
			},
		},
	}
	if envSecretRefName != "" {
		c.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: envSecretRefName},
			},
		}}
	}
	return c
}

func buildRuntimeContainer(swarm *crawblv1alpha1.UserSwarm, envSecretRefName string) corev1.Container {
	c := corev1.Container{
		Name:            "zeroclaw",
		Image:           swarm.Spec.Runtime.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{runtimeMode(swarm)},
		Ports: []corev1.ContainerPort{{
			Name:          "http",
			ContainerPort: runtimePort(swarm),
		}},
		Resources: swarm.Spec.Runtime.Resources,
		Env: []corev1.EnvVar{{
			Name:  "ZEROCLAW_GATEWAY_PORT",
			Value: fmt.Sprintf("%d", runtimePort(swarm)),
		}, {
			Name:  "ZEROCLAW_GATEWAY_HOST",
			Value: "0.0.0.0",
		}, {
			// The runtime must listen on the pod network interface so the
			// orchestrator can reach it over the ClusterIP service. NetworkPolicy
			// and the lack of a public route keep it internal-only.
			Name:  "ZEROCLAW_ALLOW_PUBLIC_BIND",
			Value: "true",
		}, {
			Name:  "ZEROCLAW_WORKSPACE",
			Value: "/zeroclaw-data/workspace",
		}},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptrTo(false),
			RunAsNonRoot:             ptrTo(true),
			RunAsUser:                ptrTo(zeroClawRuntimeUID),
			RunAsGroup:               ptrTo(zeroClawRuntimeGID),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "data",
				MountPath: "/zeroclaw-data",
			},
			{
				Name:      bootstrapConfigVolumeName(),
				MountPath: "/zeroclaw-data/workspace/SOUL.md",
				SubPath:   "SOUL.md",
				ReadOnly:  true,
			},
			{
				Name:      bootstrapConfigVolumeName(),
				MountPath: "/zeroclaw-data/workspace/IDENTITY.md",
				SubPath:   "IDENTITY.md",
				ReadOnly:  true,
			},
		},
		ReadinessProbe: healthProbe(),
		LivenessProbe:  healthProbe(),
		StartupProbe:   startupProbe(),
	}
	if envSecretRefName != "" {
		// Keep provider credentials and other sensitive runtime env outside the bootstrap ConfigMap.
		c.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: envSecretRefName},
			},
		}}
	}
	return c
}

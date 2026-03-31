package webhook

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
)

// This file holds the workload shape: the StatefulSet, pod security context,
// init container, runtime container, and the two mounted data sources that
// become the live ZeroClaw workspace.

func buildRuntimeStatefulSet(sw *crawblv1alpha1.UserSwarm, ns, bootstrapImage string, bootstrapFiles map[string]string) *appsv1.StatefulSet {
	port := runtimePortFor(sw)
	secretName := runtimeEnvSecretName(sw)
	replicas := runtimeReplicaCount(sw)

	return &appsv1.StatefulSet{
		TypeMeta:   kube.TypeMeta("apps/v1", "StatefulSet"),
		ObjectMeta: objectMeta(runtimeStatefulSetName(sw), ns, sw),
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: headlessNetworkName(sw),
			Selector:    &metav1.LabelSelector{MatchLabels: selectorLabels(sw)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: runtimeLabels(sw),
					Annotations: map[string]string{
						"crawbl.ai/config-checksum": kube.ChecksumMap(bootstrapFiles),
						"crawbl.ai/env-secret-ref":  kube.ChecksumString(secretName),
						"crawbl.ai/bootstrap-image": kube.ChecksumString(bootstrapImage),
					},
				},
				Spec: buildRuntimePodSpec(sw, port, bootstrapImage, secretName),
			},
		},
	}
}

func buildRuntimePodSpec(sw *crawblv1alpha1.UserSwarm, port int32, bootstrapImage, secretName string) corev1.PodSpec {
	return corev1.PodSpec{
		ServiceAccountName: runtimeServiceAccountName(sw),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:        kube.Ptr(true),
			RunAsUser:           kube.Ptr(runtimeUID),
			RunAsGroup:          kube.Ptr(runtimeGID),
			FSGroup:             kube.Ptr(runtimeGID),
			FSGroupChangePolicy: kube.Ptr(corev1.FSGroupChangeOnRootMismatch),
			SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		InitContainers: []corev1.Container{buildBootstrapContainer(bootstrapImage, secretName)},
		Containers:     []corev1.Container{buildZeroClawContainer(sw, port, secretName)},
		Volumes: []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: workspacePVCName(sw)},
				},
			},
			{
				Name: "bootstrap-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: runtimeConfigName(sw)},
						DefaultMode:          kube.Ptr(bootstrapConfigMode),
					},
				},
			},
		},
	}
}

func buildBootstrapContainer(bootstrapImage, secretName string) corev1.Container {
	container := corev1.Container{
		Name:            "bootstrap-config",
		Image:           bootstrapImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/crawbl", "platform", "userswarm", "bootstrap"},
		Args: []string{
			"--bootstrap-config=/bootstrap/config.toml",
			"--live-config=/zeroclaw-data/.zeroclaw/config.toml",
			"--workspace=/zeroclaw-data/workspace",
		},
		SecurityContext: kube.RestrictedSecurityContext(runtimeUID, runtimeGID),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/zeroclaw-data"},
			{Name: "bootstrap-config", MountPath: "/bootstrap", ReadOnly: true},
		},
	}
	if secretName != "" {
		container.EnvFrom = kube.SecretEnvFrom(secretName)
	}
	return container
}

func buildZeroClawContainer(sw *crawblv1alpha1.UserSwarm, port int32, secretName string) corev1.Container {
	healthCommand := []string{"/usr/local/bin/zeroclaw", "status", "--format=exit-code"}

	healthProbe := &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: healthCommand}},
		InitialDelaySeconds: 10,
		PeriodSeconds:       30,
		TimeoutSeconds:      10,
		FailureThreshold:    3,
	}
	startupProbe := &corev1.Probe{
		ProbeHandler:     corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: healthCommand}},
		PeriodSeconds:    10,
		TimeoutSeconds:   10,
		FailureThreshold: 18,
	}

	image := sw.Spec.Runtime.Image
	if image == "" {
		image = "registry.digitalocean.com/crawbl/zeroclaw:latest"
	}

	container := corev1.Container{
		Name:            "zeroclaw",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{runtimeModeFor(sw)},
		Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: port}},
		Resources:       sw.Spec.Runtime.Resources,
		Env:             runtimeGatewayEnv(port),
		SecurityContext: kube.RestrictedSecurityContext(runtimeUID, runtimeGID),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/zeroclaw-data"},
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/SOUL.md", SubPath: "SOUL.md", ReadOnly: true},
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/IDENTITY.md", SubPath: "IDENTITY.md", ReadOnly: true},
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/TOOLS.md", SubPath: "TOOLS.md", ReadOnly: true},
		},
		ReadinessProbe: healthProbe,
		LivenessProbe:  healthProbe,
		StartupProbe:   startupProbe,
	}
	if secretName != "" {
		container.EnvFrom = kube.SecretEnvFrom(secretName)
	}
	return container
}

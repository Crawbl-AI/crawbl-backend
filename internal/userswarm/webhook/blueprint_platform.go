package webhook

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
)

// This file holds the non-pod parts of the desired runtime graph:
// identity, config, storage, network entrypoints, and the optional backup job.

func buildServiceAccount(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		TypeMeta:   kube.TypeMeta("v1", "ServiceAccount"),
		ObjectMeta: objectMeta(runtimeServiceAccountName(sw), ns, sw),
	}
	if pullSecret := sw.Spec.Runtime.ImagePullSecretName; pullSecret != "" {
		sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
	}
	return sa
}

func buildBootstrapConfigMap(sw *crawblv1alpha1.UserSwarm, ns string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   kube.TypeMeta("v1", "ConfigMap"),
		ObjectMeta: objectMeta(runtimeConfigName(sw), ns, sw),
		Data:       data,
	}
}

func buildWorkspacePVC(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.PersistentVolumeClaim {
	size := sw.Spec.Storage.Size
	if size == "" {
		size = "2Gi"
	}

	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta:   kube.TypeMeta("v1", "PersistentVolumeClaim"),
		ObjectMeta: objectMeta(workspacePVCName(sw), ns, sw),
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(size),
				},
			},
		},
	}

	if storageClass := sw.Spec.Storage.StorageClassName; storageClass != "" {
		pvc.Spec.StorageClassName = kube.Ptr(storageClass)
	}

	return pvc
}

func buildHeadlessNetwork(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.Service {
	port := runtimePortFor(sw)
	return &corev1.Service{
		TypeMeta:   kube.TypeMeta("v1", "Service"),
		ObjectMeta: objectMeta(headlessNetworkName(sw), ns, sw),
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector:                 selectorLabels(sw),
			Ports:                    []corev1.ServicePort{{Name: "http", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}
}

func buildRuntimeNetwork(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.Service {
	port := runtimePortFor(sw)
	return &corev1.Service{
		TypeMeta:   kube.TypeMeta("v1", "Service"),
		ObjectMeta: objectMeta(runtimeServiceName(sw), ns, sw),
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabels(sw),
			Ports:    []corev1.ServicePort{{Name: "http", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}
}

func buildBackupJob(sw *crawblv1alpha1.UserSwarm, ns string, cfg *runtimeConfig) *batchv1.Job {
	return &batchv1.Job{
		TypeMeta:   kube.TypeMeta("batch/v1", "Job"),
		ObjectMeta: objectMeta(backupJobName(sw), ns, sw),
		Spec: batchv1.JobSpec{
			BackoffLimit:            kube.Ptr(int32(1)),
			TTLSecondsAfterFinished: kube.Ptr(int32(3600)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: runtimeServiceAccountName(sw),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: kube.Ptr(true),
						RunAsUser:    kube.Ptr(runtimeUID),
						RunAsGroup:   kube.Ptr(runtimeGID),
					},
					Containers: []corev1.Container{{
						Name:    "backup",
						Image:   cfg.BootstrapImage,
						Command: []string{"/crawbl", "platform", "userswarm", "backup"},
						Args: []string{
							"--workspace=/zeroclaw-data/workspace",
							"--bucket=" + cfg.BackupBucket,
							"--region=" + cfg.BackupRegion,
							"--swarm=" + sw.Name,
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/zeroclaw-data", ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{{
						Name:         "data",
						VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: workspacePVCName(sw)}},
					}},
				},
			},
		},
	}
}

func objectMeta(name, ns string, sw *crawblv1alpha1.UserSwarm) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: runtimeLabels(sw)}
}

func runtimeGatewayEnv(port int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "ZEROCLAW_GATEWAY_PORT", Value: fmt.Sprintf("%d", port)},
		{Name: "ZEROCLAW_GATEWAY_HOST", Value: "0.0.0.0"},
		{Name: "ZEROCLAW_ALLOW_PUBLIC_BIND", Value: "true"},
		{Name: "ZEROCLAW_WORKSPACE", Value: "/zeroclaw-data/workspace"},
		// Multi-agent delegation + tool calls can exceed the default 30s timeout.
		{Name: "ZEROCLAW_GATEWAY_TIMEOUT_SECS", Value: "300"},
	}
}

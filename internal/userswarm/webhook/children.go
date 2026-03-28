package webhook

// This file contains pure functions that build the desired K8s child resources
// for a UserSwarm. Each function takes a UserSwarm spec (+ namespace + config)
// and returns a typed K8s struct. No side effects, no API calls, no state.
//
// These are the 7 resources Metacontroller manages for each UserSwarm:
//
//  1. ServiceAccount  — per-user SA with image pull secrets for private registry.
//  2. ConfigMap       — ZeroClaw bootstrap files (config.toml, SOUL.md, IDENTITY.md, TOOLS.md, AGENTS.md).
//  3. PVC             — persistent storage for ZeroClaw workspace and config state.
//  4. Headless Service — required by StatefulSet for stable pod DNS names.
//  5. ClusterIP Service — main traffic entry point; orchestrator proxies requests through this.
//  6. StatefulSet     — the actual ZeroClaw runtime pod (init container + main container).
//  7. Backup Job      — periodic S3 backup of workspace data (optional, skipped for e2e swarms).
//
// Every function is exported and takes explicit parameters, making them
// directly unit-testable: construct a UserSwarm, call the function, assert
// on the returned struct fields.

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"

)

// Security constants for ZeroClaw runtime containers.
// UID/GID 65532 is the standard "nonroot" user in distroless images.
const (
	runtimeUID          int64 = 65532
	runtimeGID          int64 = 65532
	bootstrapConfigMode int32 = 0o444 // world-readable, nobody can write
)

// ---------------------------------------------------------------------------
// 1. ServiceAccount
// ---------------------------------------------------------------------------

// DesiredServiceAccount builds the per-user ServiceAccount.
// If the CR specifies an imagePullSecretName, the SA references it so pods
// automatically get credentials when pulling from private registries (DOCR).
func DesiredServiceAccount(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		TypeMeta:   kube.TypeMeta("v1", "ServiceAccount"),
		ObjectMeta: objectMeta(ServiceAccountName(sw), ns, sw),
	}
	if ps := sw.Spec.Runtime.ImagePullSecretName; ps != "" {
		sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: ps}}
	}
	return sa
}

// ---------------------------------------------------------------------------
// 2. ConfigMap
// ---------------------------------------------------------------------------

// DesiredConfigMap builds the bootstrap ConfigMap containing ZeroClaw config files.
// The init container reads these and merges operator-managed keys into the
// PVC-backed live config without clobbering ZeroClaw runtime state.
//
// The data map typically contains: config.toml, SOUL.md, IDENTITY.md, TOOLS.md, AGENTS.md.
func DesiredConfigMap(sw *crawblv1alpha1.UserSwarm, ns string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   kube.TypeMeta("v1", "ConfigMap"),
		ObjectMeta: objectMeta(ConfigMapName(sw), ns, sw),
		Data:       data,
	}
}

// ---------------------------------------------------------------------------
// 3. PVC
// ---------------------------------------------------------------------------

// DesiredPVC builds the PersistentVolumeClaim for this user's ZeroClaw data.
// Holds /zeroclaw-data — config, workspace files, and any state ZeroClaw accumulates.
// RWO because each user has a single-replica StatefulSet.
func DesiredPVC(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta:   kube.TypeMeta("v1", "PersistentVolumeClaim"),
		ObjectMeta: objectMeta(PVCName(sw), ns, sw),
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(sw.Spec.Storage.Size),
				},
			},
		},
	}
	if sw.Spec.Storage.StorageClassName != "" {
		pvc.Spec.StorageClassName = kube.Ptr(sw.Spec.Storage.StorageClassName)
	}
	return pvc
}

// ---------------------------------------------------------------------------
// 4. Headless Service
// ---------------------------------------------------------------------------

// DesiredHeadlessService builds the headless Service required by the StatefulSet.
// Without it, pods can't get stable DNS names and the StatefulSet controller
// won't create pods. PublishNotReadyAddresses is true so DNS entries exist
// even during startup (helps init container and probes resolve).
func DesiredHeadlessService(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.Service {
	port := runtimePort(sw)
	return &corev1.Service{
		TypeMeta:   kube.TypeMeta("v1", "Service"),
		ObjectMeta: objectMeta(HeadlessServiceName(sw), ns, sw),
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector:                 SelectorLabels(sw),
			Ports:                    []corev1.ServicePort{{Name: "http", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}
}

// ---------------------------------------------------------------------------
// 5. ClusterIP Service
// ---------------------------------------------------------------------------

// DesiredService builds the ClusterIP Service that the orchestrator uses to
// reach this user's ZeroClaw runtime. The backend proxies chat requests through
// this service: http://{name}.{ns}.svc.cluster.local:{port}/webhook
func DesiredService(sw *crawblv1alpha1.UserSwarm, ns string) *corev1.Service {
	port := runtimePort(sw)
	return &corev1.Service{
		TypeMeta:   kube.TypeMeta("v1", "Service"),
		ObjectMeta: objectMeta(ServiceName(sw), ns, sw),
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: SelectorLabels(sw),
			Ports:    []corev1.ServicePort{{Name: "http", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}
}

// ---------------------------------------------------------------------------
// 6. StatefulSet
// ---------------------------------------------------------------------------

// DesiredStatefulSet builds the StatefulSet that runs the ZeroClaw runtime pod.
//
// The pod has two containers:
//   - Init container (bootstrap-config): runs "crawbl platform bootstrap"
//     to merge webhook-managed config keys into the PVC-backed live config.
//   - Main container (zeroclaw): the actual ZeroClaw runtime process.
//
// Rolling updates are triggered by checksum annotations on the pod template:
// when bootstrap files or the env secret change, the checksum changes, K8s sees
// a spec diff, and Metacontroller's RollingRecreate strategy restarts the pod.
//
// When suspended (spec.suspend=true), replicas is set to 0. The PVC and Services
// are preserved so the user can resume without data loss.
func DesiredStatefulSet(sw *crawblv1alpha1.UserSwarm, ns, bootstrapImage string, bootstrapFiles map[string]string) *appsv1.StatefulSet {
	port := runtimePort(sw)
	secretRef := envSecretRef(sw)
	replicas := ReplicaCount(sw)

	return &appsv1.StatefulSet{
		TypeMeta:   kube.TypeMeta("apps/v1", "StatefulSet"),
		ObjectMeta: objectMeta(StatefulSetName(sw), ns, sw),
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: HeadlessServiceName(sw),
			Selector:    &metav1.LabelSelector{MatchLabels: SelectorLabels(sw)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: AllLabels(sw),
					Annotations: map[string]string{
						// These checksums trigger rolling updates when config changes.
						"crawbl.ai/config-checksum": kube.ChecksumMap(bootstrapFiles),
						"crawbl.ai/env-secret-ref":  kube.ChecksumString(secretRef),
					},
				},
				Spec: buildPodSpec(sw, port, bootstrapImage, secretRef),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// 7. Backup Job
// ---------------------------------------------------------------------------

// DesiredBackupJob builds a Job that backs up the user's workspace to S3.
// Uses the same crawbl-platform image with "crawbl platform backup".
// TTL is set to 1 hour so completed Jobs auto-cleanup.
func DesiredBackupJob(sw *crawblv1alpha1.UserSwarm, ns string, cfg *Config) *batchv1.Job {
	return &batchv1.Job{
		TypeMeta:   kube.TypeMeta("batch/v1", "Job"),
		ObjectMeta: objectMeta(BackupJobName(sw), ns, sw),
		Spec: batchv1.JobSpec{
			BackoffLimit:            kube.Ptr(int32(1)),
			TTLSecondsAfterFinished: kube.Ptr(int32(3600)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: ServiceAccountName(sw),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: kube.Ptr(true),
						RunAsUser:    kube.Ptr(runtimeUID),
						RunAsGroup:   kube.Ptr(runtimeGID),
					},
					Containers: []corev1.Container{{
						Name:    "backup",
						Image:   cfg.BootstrapImage,
						Command: []string{"/crawbl", "platform", "backup"},
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
						VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: PVCName(sw)}},
					}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Pod spec builder (internal)
// ---------------------------------------------------------------------------

// buildPodSpec assembles the full pod spec with security context, init container,
// main container, and volume mounts. Extracted for readability.
func buildPodSpec(sw *crawblv1alpha1.UserSwarm, port int32, bootstrapImage, secretRef string) corev1.PodSpec {
	return corev1.PodSpec{
		ServiceAccountName: ServiceAccountName(sw),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:        kube.Ptr(true),
			RunAsUser:           kube.Ptr(runtimeUID),
			RunAsGroup:          kube.Ptr(runtimeGID),
			FSGroup:             kube.Ptr(runtimeGID),
			FSGroupChangePolicy: kube.Ptr(corev1.FSGroupChangeOnRootMismatch),
			SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		InitContainers: []corev1.Container{buildInitContainer(bootstrapImage, secretRef)},
		Containers:     []corev1.Container{buildRuntimeContainer(sw, port, secretRef)},
		Volumes: []corev1.Volume{
			{
				// Main data volume backed by the user's PVC.
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: PVCName(sw)},
				},
			},
			{
				// Bootstrap config from the ConfigMap, mounted read-only.
				Name: "bootstrap-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName(sw)},
						DefaultMode:          kube.Ptr(bootstrapConfigMode),
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Container builders (internal)
// ---------------------------------------------------------------------------

// buildInitContainer creates the bootstrap-config init container.
// Runs "crawbl platform bootstrap" to merge webhook-managed config
// keys into the PVC-backed live config without clobbering ZeroClaw state.
func buildInitContainer(bootstrapImage, secretRef string) corev1.Container {
	c := corev1.Container{
		Name:            "bootstrap-config",
		Image:           bootstrapImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/crawbl", "platform", "bootstrap"},
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
	if secretRef != "" {
		c.EnvFrom = kube.SecretEnvFrom(secretRef)
	}
	return c
}

// buildRuntimeContainer creates the main zeroclaw container.
// This is the actual ZeroClaw process that serves the user's AI swarm.
//
// Probes:
//   - Readiness + Liveness: exec "zeroclaw status --format=exit-code" every 30s.
//   - Startup: same check every 10s, with 18 failures allowed = 3 min budget
//     for ZeroClaw to download models and initialize the workspace.
func buildRuntimeContainer(sw *crawblv1alpha1.UserSwarm, port int32, secretRef string) corev1.Container {
	healthCmd := []string{"/usr/local/bin/zeroclaw", "status", "--format=exit-code"}

	healthProbe := &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: healthCmd}},
		InitialDelaySeconds: 10,
		PeriodSeconds:       30,
		TimeoutSeconds:      10,
		FailureThreshold:    3,
	}
	startupProbe := &corev1.Probe{
		ProbeHandler:     corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: healthCmd}},
		PeriodSeconds:    10,
		TimeoutSeconds:   10,
		FailureThreshold: 18, // 18 * 10s = 3 min startup budget
	}

	c := corev1.Container{
		Name:            "zeroclaw",
		Image:           sw.Spec.Runtime.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{runtimeMode(sw)},
		Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: port}},
		Resources:       sw.Spec.Runtime.Resources,
		Env: []corev1.EnvVar{
			{Name: "ZEROCLAW_GATEWAY_PORT", Value: fmt.Sprintf("%d", port)},
			{Name: "ZEROCLAW_GATEWAY_HOST", Value: "0.0.0.0"},
			{Name: "ZEROCLAW_ALLOW_PUBLIC_BIND", Value: "true"},
			{Name: "ZEROCLAW_WORKSPACE", Value: "/zeroclaw-data/workspace"},
		},
		SecurityContext: kube.RestrictedSecurityContext(runtimeUID, runtimeGID),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/zeroclaw-data"},
			// Operator-managed markdown files mounted read-only from the ConfigMap.
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/SOUL.md", SubPath: "SOUL.md", ReadOnly: true},
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/IDENTITY.md", SubPath: "IDENTITY.md", ReadOnly: true},
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/TOOLS.md", SubPath: "TOOLS.md", ReadOnly: true},
			{Name: "bootstrap-config", MountPath: "/zeroclaw-data/workspace/AGENTS.md", SubPath: "AGENTS.md", ReadOnly: true},
		},
		ReadinessProbe: healthProbe,
		LivenessProbe:  healthProbe,
		StartupProbe:   startupProbe,
	}
	if secretRef != "" {
		c.EnvFrom = kube.SecretEnvFrom(secretRef)
	}
	return c
}

// objectMeta builds a standard ObjectMeta with name, namespace, and the full label set.
// This stays here (not in kube/) because it depends on UserSwarm-specific AllLabels().
func objectMeta(name, ns string, sw *crawblv1alpha1.UserSwarm) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: AllLabels(sw)}
}

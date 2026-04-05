package webhook

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
)

// This file holds the workload shape: a single Deployment (not a
// StatefulSet — there is no durable per-pod state anymore, all persistence
// flows through the orchestrator over gRPC) that runs the
// crawbl-agent-runtime binary. No init containers, no PVCs, no TOML config,
// no SOUL/IDENTITY/TOOLS markdown files — the pod boots from CLI flags
// and the envSecretRef Secret and nothing else.

// buildRuntimeDeployment constructs the desired Deployment for one
// UserSwarm. Replicas is 0 when the CR is suspended and 1 otherwise.
func buildRuntimeDeployment(sw *crawblv1alpha1.UserSwarm, ns string, cfg *runtimeConfig) *appsv1.Deployment {
	port := runtimePortFor(sw)
	secretName := runtimeEnvSecretName(sw)
	replicas := runtimeReplicaCount(sw)
	image := resolveRuntimeImage(sw, cfg)

	return &appsv1.Deployment{
		TypeMeta:   kube.TypeMeta("apps/v1", "Deployment"),
		ObjectMeta: objectMeta(runtimeDeploymentName(sw), ns, sw),
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(sw)},
			Strategy: appsv1.DeploymentStrategy{
				// Recreate instead of RollingUpdate because a workspace
				// always has exactly one active runtime pod — we do not
				// want two instances racing on the same gRPC service.
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: runtimeLabels(sw),
					Annotations: map[string]string{
						"crawbl.ai/runtime-image": kube.ChecksumString(image),
						"crawbl.ai/env-secret":    kube.ChecksumString(secretName),
					},
				},
				Spec: buildRuntimePodSpec(sw, port, image, secretName, cfg),
			},
		},
	}
}

// buildRuntimePodSpec assembles the pod spec: one container, two emptyDir
// volumes (cache + tmp), restricted security context, and no init work.
func buildRuntimePodSpec(sw *crawblv1alpha1.UserSwarm, port int32, image, secretName string, cfg *runtimeConfig) corev1.PodSpec {
	return corev1.PodSpec{
		ServiceAccountName: runtimeServiceAccountName(sw),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   kube.Ptr(true),
			RunAsUser:      kube.Ptr(runtimeUID),
			RunAsGroup:     kube.Ptr(runtimeGID),
			FSGroup:        kube.Ptr(runtimeGID),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Containers: []corev1.Container{buildAgentRuntimeContainer(sw, port, image, secretName, cfg)},
		Volumes: []corev1.Volume{
			{
				// /cache holds transient artifacts the runtime may materialize
				// while a turn is in flight (MCP cache, model scratch space).
				// Backed by an emptyDir capped at 512Mi so a runaway cache
				// can never fill the node.
				Name: "cache",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						SizeLimit: kube.Ptr(resource.MustParse("512Mi")),
					},
				},
			},
			{
				// /tmp is a tmpfs so the container filesystem can stay
				// read-only. 128Mi is enough for ADK temporary buffers and
				// Go's os.TempDir scratch files.
				Name: "tmp",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium:    corev1.StorageMediumMemory,
						SizeLimit: kube.Ptr(resource.MustParse("128Mi")),
					},
				},
			},
		},
	}
}

// buildAgentRuntimeContainer is the single container in the runtime pod.
// It runs /crawbl-agent-runtime with CLI flags for identity and endpoints,
// pulls secrets (OPENAI_API_KEY, CRAWBL_MCP_SIGNING_KEY, ...) from the
// envSecretRef Secret, and reports health via the gRPC Health service.
func buildAgentRuntimeContainer(sw *crawblv1alpha1.UserSwarm, port int32, image, secretName string, cfg *runtimeConfig) corev1.Container {
	workspaceID := workspaceIDFromSwarmName(sw.Name)

	args := []string{
		fmt.Sprintf("--grpc-listen=:%d", port),
		"--workspace-id=" + workspaceID,
		"--user-id=" + sw.Spec.UserID,
	}
	if cfg.OrchestratorGRPCEndpoint != "" {
		args = append(args, "--orchestrator-endpoint="+cfg.OrchestratorGRPCEndpoint)
	}
	if cfg.MCPEndpoint != "" {
		args = append(args, "--mcp-endpoint="+cfg.MCPEndpoint)
	}
	if model := sw.Spec.Config.DefaultModel; model != "" {
		args = append(args, "--openai-model="+model)
	}

	// Native gRPC probe against grpc.health.v1.Health/Check. Kubernetes
	// v1.24+ ships this built-in, so there's no sidecar and no exec shell.
	healthProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			GRPC: &corev1.GRPCAction{Port: port},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
	startupProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			GRPC: &corev1.GRPCAction{Port: port},
		},
		PeriodSeconds:    5,
		TimeoutSeconds:   5,
		FailureThreshold: 24, // 2 minutes to boot
	}

	container := corev1.Container{
		Name:            "agent-runtime",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/crawbl-agent-runtime"},
		Args:            args,
		Ports: []corev1.ContainerPort{{
			Name:          "grpc",
			ContainerPort: port,
			Protocol:      corev1.ProtocolTCP,
		}},
		Resources: sw.Spec.Runtime.Resources,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             kube.Ptr(true),
			RunAsUser:                kube.Ptr(runtimeUID),
			RunAsGroup:               kube.Ptr(runtimeGID),
			AllowPrivilegeEscalation: kube.Ptr(false),
			ReadOnlyRootFilesystem:   kube.Ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "cache", MountPath: "/cache"},
			{Name: "tmp", MountPath: "/tmp"},
		},
		ReadinessProbe: healthProbe,
		LivenessProbe:  healthProbe,
		StartupProbe:   startupProbe,
	}

	if secretName != "" {
		container.EnvFrom = kube.SecretEnvFrom(secretName)
	}

	// Literal env vars for Postgres + Redis connection settings. The
	// runtime container reads these through internal/pkg/database and
	// internal/pkg/redisclient. Passwords live in the envSecretRef
	// Secret (projected via EnvFrom above); the non-secret fields are
	// injected here so a single webhook rollout updates every runtime
	// pod's backend targets without touching the per-workspace Secret.
	container.Env = append(container.Env, runtimeBackendEnv(cfg)...)

	return container
}

// runtimeBackendEnv produces the list of literal env vars the runtime
// container needs to reach the orchestrator-shared Postgres + Redis
// backends. Values are sourced from the webhook process env (see
// runtimeConfigFromEnv) so cluster operators change backends by
// rolling the webhook, not by editing every per-workspace Secret.
func runtimeBackendEnv(cfg *runtimeConfig) []corev1.EnvVar {
	if cfg == nil {
		return nil
	}
	out := make([]corev1.EnvVar, 0, 8)
	if cfg.PostgresHost != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_DATABASE_HOST", Value: cfg.PostgresHost})
	}
	if cfg.PostgresPort != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_DATABASE_PORT", Value: cfg.PostgresPort})
	}
	if cfg.PostgresUser != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_DATABASE_USER", Value: cfg.PostgresUser})
	}
	if cfg.PostgresName != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_DATABASE_NAME", Value: cfg.PostgresName})
	}
	if cfg.PostgresSchema != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_DATABASE_SCHEMA", Value: cfg.PostgresSchema})
	}
	if cfg.PostgresSSLMode != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_DATABASE_SSLMODE", Value: cfg.PostgresSSLMode})
	}
	if cfg.RedisAddr != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_REDIS_ADDR", Value: cfg.RedisAddr})
	}
	if cfg.OTelEnabled != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_OTEL_ENABLED", Value: cfg.OTelEnabled})
	}
	if cfg.OTelMetricsEndpoint != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_OTEL_METRICS_ENDPOINT", Value: cfg.OTelMetricsEndpoint})
	}
	if cfg.OTelEnvironment != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_OTEL_ENVIRONMENT", Value: cfg.OTelEnvironment})
	}
	if cfg.OTelNamespace != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_OTEL_NAMESPACE", Value: cfg.OTelNamespace})
	}
	if cfg.OTelExportInterval != "" {
		out = append(out, corev1.EnvVar{Name: "CRAWBL_OTEL_EXPORT_INTERVAL", Value: cfg.OTelExportInterval})
	}
	return out
}

// resolveRuntimeImage picks the image in this order:
//  1. Webhook env var CRAWBL_AGENT_RUNTIME_IMAGE (rolls every swarm forward
//     on webhook redeploy — used by the CI pipeline).
//  2. Spec.Runtime.Image on the CR (per-workspace override).
//  3. A hardcoded fallback so a misconfigured webhook still produces a pod
//     instead of an invalid PodSpec error.
func resolveRuntimeImage(sw *crawblv1alpha1.UserSwarm, cfg *runtimeConfig) string {
	if cfg != nil && cfg.AgentRuntimeImage != "" {
		return cfg.AgentRuntimeImage
	}
	if sw.Spec.Runtime.Image != "" {
		return sw.Spec.Runtime.Image
	}
	return "registry.digitalocean.com/crawbl/crawbl-agent-runtime:dev"
}

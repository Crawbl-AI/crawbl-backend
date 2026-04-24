// Package client holds the Kubernetes CR lifecycle half of the production
// Client implementation: EnsureRuntime, DeleteRuntime, getRuntimeState,
// desiredUserSwarm, and the NewUserSwarmClient constructor. The gRPC
// wire half (SendText, SendTextStream, Memory RPCs, connection cache)
// lives in grpc_client.go, grpc_converse.go, and grpc_memory.go.
//
// There is no HTTP wire code anywhere in this package anymore. The
// legacy /webhook, /webhook/stream, and /api/memory endpoints that the
// Rust agent runtime exposed are gone (US-P2-004).
package client

import (
	"context"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/middleware"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
)

// NewUserSwarmClient constructs the production Kubernetes-backed Client.
//
// Setup steps:
//  1. Register Crawbl CRD types with a fresh controller-runtime Scheme.
//  2. Load the in-cluster or kubeconfig REST config.
//  3. Build a typed controller-runtime client.
//  4. Fill in UserSwarmConfig defaults for any blank fields.
//
// Returns error (not *merrors.Error) because this is called at start-up
// — the caller should treat a non-nil error as fatal.
func NewUserSwarmClient(cfg Config) (Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(crawblv1alpha1.AddToScheme(scheme))

	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	// Override the default K8s client rate limiter. The default QPS=5/Burst=10
	// throttles at ~100 concurrent users calling EnsureRuntime. 50/100 provides
	// headroom up to ~20K users without overwhelming the API server.
	restConfig.QPS = 50
	restConfig.Burst = 100

	k8sClient, err := k8sclient.New(restConfig, k8sclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	userswarmCfg := cfg.UserSwarm
	if strings.TrimSpace(userswarmCfg.RuntimeNamespace) == "" {
		userswarmCfg.RuntimeNamespace = DefaultRuntimeNamespace
	}
	if userswarmCfg.PollTimeout <= 0 {
		userswarmCfg.PollTimeout = DefaultPollTimeout
	}
	if userswarmCfg.PollInterval <= 0 {
		userswarmCfg.PollInterval = DefaultPollInterval
	}
	if userswarmCfg.Port <= 0 {
		userswarmCfg.Port = DefaultRuntimePort
	}

	cacheCtx, cacheCancel := context.WithCancel(context.Background())
	return &userSwarmClient{
		client:      k8sClient,
		config:      userswarmCfg,
		grpcPool:    crawblgrpc.NewInsecureHMACPool(userswarmCfg.MCPSigningKey),
		cache:       newRuntimeCache(cacheCtx),
		cacheCancel: cacheCancel,
	}, nil
}

// Close releases every cached gRPC connection. Safe to call multiple
// times. The orchestrator's shutdown path invokes it once on SIGTERM.
func (c *userSwarmClient) Close() error {
	if c == nil {
		return nil
	}
	if c.cacheCancel != nil {
		c.cacheCancel()
	}
	if c.grpcPool == nil {
		return nil
	}
	return c.grpcPool.Close()
}

// EnsureRuntime is the canonical "upsert the workspace's UserSwarm CR
// and optionally wait for it to be Ready" entry point. Called by the
// orchestrator during sign-up, sign-in, and before every message send
// that needs a live runtime.
//
// The returned RuntimeStatus carries the identity fields (UserID,
// WorkspaceID) populated from opts so downstream SendText/Memory calls
// can sign HMAC bearer tokens without another lookup.
func (c *userSwarmClient) EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error) {
	if opts == nil || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	// Fast path: return cached status if the runtime is already verified
	// and the cache entry hasn't expired. Reduces K8s API calls by ~95%.
	if cached, ok := c.cache.get(opts.WorkspaceID); ok {
		// Shallow copy so concurrent callers don't race on identity fields.
		cp := *cached
		cp.UserID = opts.UserID
		cp.WorkspaceID = opts.WorkspaceID
		return &cp, nil
	}

	desired := c.desiredUserSwarm(ctx, opts)
	if mErr := c.upsertUserSwarm(ctx, desired); mErr != nil {
		return nil, mErr
	}

	// Fetch the current state (reads the status subresource populated
	// by the UserSwarm webhook).
	status, mErr := c.getRuntimeState(ctx, desired.Name)
	if mErr != nil {
		return nil, mErr
	}
	// Stamp identity into the returned RuntimeStatus so the gRPC client
	// half of this package can sign HMAC bearer tokens on every call.
	status.UserID = opts.UserID
	status.WorkspaceID = opts.WorkspaceID

	if !opts.WaitForVerified || status.Verified {
		c.cache.set(opts.WorkspaceID, status)
		return status, nil
	}

	return c.pollUntilVerified(ctx, opts, desired.Name, status)
}

// upsertUserSwarm creates or updates the UserSwarm CR to match the desired state.
func (c *userSwarmClient) upsertUserSwarm(ctx context.Context, desired *crawblv1alpha1.UserSwarm) *merrors.Error {
	var existing crawblv1alpha1.UserSwarm
	err := c.client.Get(ctx, k8sclient.ObjectKey{Name: desired.Name}, &existing)
	switch {
	case err == nil:
		// Update the spec in place when it has drifted.
		if !reflect.DeepEqual(existing.Spec, desired.Spec) || !reflect.DeepEqual(existing.Labels, desired.Labels) {
			existing.Spec = desired.Spec
			existing.Labels = desired.Labels
			if err := c.client.Update(ctx, &existing); err != nil {
				return merrors.WrapStdServerError(err, "update userswarm")
			}
		}
	case k8sclient.IgnoreNotFound(err) == nil:
		// CR does not exist yet — create it.
		if err := c.client.Create(ctx, desired); err != nil {
			return merrors.WrapStdServerError(err, "create userswarm")
		}
	default:
		return merrors.WrapStdServerError(err, "get userswarm")
	}
	return nil
}

// pollUntilVerified polls the UserSwarm CR until Verified=true, PollTimeout, or ctx cancellation.
func (c *userSwarmClient) pollUntilVerified(ctx context.Context, opts *EnsureRuntimeOpts, swarmName string, status *orchestrator.RuntimeStatus) (*orchestrator.RuntimeStatus, *merrors.Error) {
	timeout := c.config.PollTimeout
	if timeout <= 0 {
		timeout = DefaultPollTimeout
	}
	interval := c.config.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, merrors.WrapStdServerError(ctx.Err(), "ensure runtime canceled")
		case <-ticker.C:
		}
		var mErr *merrors.Error
		status, mErr = c.getRuntimeState(ctx, swarmName)
		if mErr != nil {
			return nil, mErr
		}
		status.UserID = opts.UserID
		status.WorkspaceID = opts.WorkspaceID
		if status.Verified {
			c.cache.set(opts.WorkspaceID, status)
			return status, nil
		}
	}
	return status, merrors.ErrRuntimeNotReady
}

// getRuntimeState reads the UserSwarm CR and converts its status into
// the orchestrator.RuntimeStatus domain type. The returned value does
// NOT have UserID/WorkspaceID stamped — callers (EnsureRuntime, the
// gRPC helpers) are responsible for setting those fields from the
// calling opts.
func (c *userSwarmClient) getRuntimeState(ctx context.Context, swarmName string) (*orchestrator.RuntimeStatus, *merrors.Error) {
	var swarm crawblv1alpha1.UserSwarm
	if err := c.client.Get(ctx, k8sclient.ObjectKey{Name: swarmName}, &swarm); err != nil {
		return nil, merrors.WrapStdServerError(err, "get userswarm status")
	}
	return &orchestrator.RuntimeStatus{
		SwarmName:        swarm.Name,
		RuntimeNamespace: swarm.Status.RuntimeNamespace,
		ServiceName:      swarm.Status.ServiceName,
		Phase:            swarm.Status.Phase,
		Verified:         isConditionTrue(swarm.Status.Conditions, readyConditionType),
		Status: orchestrator.ResolveRuntimeState(
			swarm.Status.Phase,
			isConditionTrue(swarm.Status.Conditions, readyConditionType),
		),
	}, nil
}

// desiredUserSwarm constructs the CR the cluster should hold for this
// workspace. EnsureRuntime diffs this against the actual CR to decide
// whether to create or update.
//
// The agent-runtime pod reads all of its configuration from CLI flags
// injected by the webhook plus the envSecretRef Secret — there is no
// TOML merge step, no ConfigMap, and no Raw escape hatch populated here.
func (c *userSwarmClient) desiredUserSwarm(ctx context.Context, opts *EnsureRuntimeOpts) *crawblv1alpha1.UserSwarm {
	name := userswarmName(opts.WorkspaceID)

	labels := map[string]string{}
	if principal, ok := middleware.PrincipalFromContext(ctx); ok {
		if strings.HasPrefix(principal.Subject, "e2e-") {
			labels["crawbl.ai/e2e"] = "true"
		}
	}

	sw := &crawblv1alpha1.UserSwarm{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: crawblv1alpha1.UserSwarmSpec{
			UserID:  opts.UserID,
			Suspend: false, // Explicit: unsuspend the runtime when EnsureRuntime is called.
			Placement: crawblv1alpha1.UserSwarmPlacementSpec{
				RuntimeNamespace: c.config.RuntimeNamespace,
			},
			Runtime: crawblv1alpha1.UserSwarmRuntimeSpec{
				Image:               c.config.Image,
				Port:                c.config.Port,
				ImagePullSecretName: c.config.ImagePullSecretName,
			},
			Config: crawblv1alpha1.UserSwarmConfigSpec{
				DefaultProvider: c.config.DefaultProvider,
				DefaultModel:    c.config.DefaultModel,
			},
		},
	}

	// Set default resource limits for agent runtime pods when none are specified.
	if len(sw.Spec.Runtime.Resources.Requests) == 0 && len(sw.Spec.Runtime.Resources.Limits) == 0 {
		sw.Spec.Runtime.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		}
	}

	if secretName := strings.TrimSpace(c.config.EnvSecretName); secretName != "" {
		sw.Spec.Config.EnvSecretRef = &crawblv1alpha1.UserSwarmSecretRef{Name: secretName}
	}

	if len(opts.AgentSettings) > 0 {
		sw.Spec.Config.Agents = make(map[string]crawblv1alpha1.UserSwarmAgentConfigSpec, len(opts.AgentSettings))
		for slug, settings := range opts.AgentSettings {
			sw.Spec.Config.Agents[slug] = crawblv1alpha1.UserSwarmAgentConfigSpec{
				Model:          settings.Model,
				ResponseLength: settings.ResponseLength,
				AllowedTools:   settings.AllowedTools,
			}
		}
	}

	return sw
}

// DeleteRuntime removes the UserSwarm CR for the given workspace.
// Idempotent: if the CR is already gone we return nil.
func (c *userSwarmClient) DeleteRuntime(ctx context.Context, workspaceID string) *merrors.Error {
	if strings.TrimSpace(workspaceID) == "" {
		return merrors.ErrInvalidInput
	}

	name := userswarmName(workspaceID)
	var swarm crawblv1alpha1.UserSwarm
	err := c.client.Get(ctx, k8sclient.ObjectKey{Name: name}, &swarm)
	if err != nil {
		if k8sclient.IgnoreNotFound(err) == nil {
			return nil
		}
		return merrors.WrapStdServerError(err, "get userswarm for deletion")
	}
	if err := c.client.Delete(ctx, &swarm); err != nil {
		if k8sclient.IgnoreNotFound(err) == nil {
			return nil
		}
		return merrors.WrapStdServerError(err, "delete userswarm")
	}

	// Best-effort drop of any cached gRPC connection for this workspace
	// so a recreated pod does not inherit a dead connection from the
	// pool. The service name is derived from the CR we just deleted.
	if swarm.Status.ServiceName != "" && swarm.Status.RuntimeNamespace != "" {
		target := crawblgrpc.ClusterTarget(swarm.Status.ServiceName, swarm.Status.RuntimeNamespace, c.config.Port)
		c.grpcPool.Drop(target)
	}

	c.cache.invalidate(workspaceID)

	return nil
}

// userswarmName derives the stable Kubernetes CR name from a workspace
// ID. The "workspace-" prefix namespaces the name and lowercasing
// makes it a valid DNS subdomain label.
func userswarmName(workspaceID string) string {
	return "workspace-" + strings.ToLower(strings.TrimSpace(workspaceID))
}

// isConditionTrue returns true if the named condition exists and its
// Status is ConditionTrue. Used to read Verified/Ready flags off the
// CR's status.conditions slice.
func isConditionTrue(conditions []metav1.Condition, conditionType string) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

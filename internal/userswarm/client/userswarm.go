// Package client provides HTTP client for user swarm runtime.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// NewUserSwarmClient constructs the production Kubernetes-backed Client.
//
// It performs two one-time setup steps:
//  1. Registers the Crawbl CRD types (UserSwarm, etc.) with a fresh runtime
//     Scheme so controller-runtime knows how to serialise/deserialise them when
//     talking to the API server.
//  2. Loads the in-cluster or kubeconfig REST config (same logic as kubectl)
//     and builds a typed controller-runtime client from it.
//
// Sensible defaults are applied for any UserSwarmConfig fields that are zero or
// blank, so callers only need to supply the values that differ from the defaults.
//
// Returns an error (not *merrors.Error) because this is called at start-up, not
// inside a request handler — the caller should treat a non-nil error as fatal.
func NewUserSwarmClient(cfg Config) (Client, error) {
	// Create a new Scheme and register our custom resource types into it.
	// controller-runtime uses the Scheme to map Kubernetes GVK (Group/Version/Kind)
	// tuples to Go structs such as crawblv1alpha1.UserSwarm.
	scheme := runtime.NewScheme()
	utilruntime.Must(crawblv1alpha1.AddToScheme(scheme))

	// GetConfig reads the REST configuration from, in order:
	// 1. The KUBECONFIG env var or ~/.kube/config (for local development)
	// 2. The in-cluster service-account token at /var/run/secrets/kubernetes.io/
	//    (when running inside a pod in the cluster)
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	// Build the typed Kubernetes client.  The Scheme we pass in is what allows
	// k8sClient.Get / Create / Update to work with our custom UserSwarm type.
	k8sClient, err := k8sclient.New(restConfig, k8sclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	// Normalise the UserSwarm-specific config: fill in any missing fields with
	// package-level defaults so the rest of the code can assume they are set.
	userswarmCfg := cfg.UserSwarm
	if strings.TrimSpace(userswarmCfg.RuntimeNamespace) == "" {
		userswarmCfg.RuntimeNamespace = DefaultRuntimeNamespace
	}
	if strings.TrimSpace(userswarmCfg.StorageSize) == "" {
		userswarmCfg.StorageSize = DefaultRuntimeStorageSize
	}
	if userswarmCfg.PollTimeout <= 0 {
		userswarmCfg.PollTimeout = DefaultPollTimeout
	}
	if userswarmCfg.PollInterval <= 0 {
		userswarmCfg.PollInterval = DefaultPollInterval
	}
	if userswarmCfg.Port == 0 {
		userswarmCfg.Port = DefaultRuntimePort
	}

	return &userSwarmClient{
		client: k8sClient,
		config: userswarmCfg,
		// A single shared http.Client with a timeout is enough here; the runtime
		// pods are within the same cluster so there is no connection overhead.
		httpClient:       &http.Client{Timeout: defaultHTTPTimeout},
		httpStreamClient: &http.Client{Timeout: defaultStreamHTTPTimeout},
	}, nil
}

// EnsureRuntime is the main provisioning entry-point called by the orchestrator.
//
// It implements an upsert pattern against the UserSwarm CRD:
//   - If no CR exists for the workspace yet, it creates one.
//   - If a CR exists but its spec has drifted from what we want, it updates it.
//   - If a CR exists and the spec is already correct, it does nothing.
//
// After the upsert, if WaitForVerified is false the function returns immediately
// with whatever state the CR is currently in (useful for non-blocking sign-up /
// sign-in flows where the mobile client will poll for readiness separately).
//
// If WaitForVerified is true, the function enters a polling loop that re-reads
// the CR every PollInterval until the operator sets the Ready condition to true,
// or until PollTimeout is exceeded, in which case ErrRuntimeNotReady is returned.
//
// The nolint directive suppresses the cyclomatic-complexity linter warning;
// the branching here is inherently required by the upsert + optional-poll logic.
//
//nolint:cyclop
func (c *userSwarmClient) EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error) {
	// Guard: both IDs are mandatory — a missing UserID would produce a CR without
	// owner attribution, and a missing WorkspaceID makes the CR name undefined.
	if opts == nil || strings.TrimSpace(opts.UserID) == "" || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	// Build the UserSwarm CR we want to exist in the cluster.  This is the
	// "desired state" that we will reconcile against.
	desired := c.desiredUserSwarm(ctx, opts)

	// Attempt to fetch the current CR from the cluster.
	var actual crawblv1alpha1.UserSwarm
	err := c.client.Get(ctx, k8sclient.ObjectKey{Name: desired.Name}, &actual)
	switch {
	case k8sclient.IgnoreNotFound(err) != nil:
		// A real API error (e.g. network partition, RBAC denial) — surface it.
		return nil, merrors.WrapStdServerError(err, "get userswarm")

	case err != nil:
		// err is a Not Found error: the CR does not exist yet, so create it.
		// This is the first-time provisioning path taken at sign-up.
		if createErr := c.client.Create(ctx, desired); createErr != nil {
			return nil, merrors.WrapStdServerError(createErr, "create userswarm")
		}
		actual = *desired

	case !reflect.DeepEqual(actual.Spec, desired.Spec):
		// The CR already exists but its spec no longer matches what we want
		// (e.g. the operator updated the image tag or TOML config changed).
		// Overwrite only the Spec; preserve Status and metadata managed by k8s.
		actual.Spec = desired.Spec
		if updateErr := c.client.Update(ctx, &actual); updateErr != nil {
			return nil, merrors.WrapStdServerError(updateErr, "update userswarm")
		}
	}

	// If the caller does not need to wait for the pod to be healthy, return the
	// current (possibly not-yet-ready) state right away.  The mobile client is
	// expected to poll GET /v1/workspaces/{id} for runtime.verified.
	if !opts.WaitForVerified {
		refreshed, mErr := c.getRuntimeState(ctx, desired.Name)
		if mErr != nil {
			return nil, mErr
		}
		return refreshed, nil
	}

	// Polling loop: keep re-reading the CR until the operator marks it Verified.
	// The operator sets the "Ready" condition to true once the StatefulSet pod
	// is healthy and the ZeroClaw gateway has responded to a health probe.
	deadline := time.Now().Add(c.config.PollTimeout)
	for {
		runtimeState, mErr := c.getRuntimeState(ctx, desired.Name)
		if mErr != nil {
			return nil, mErr
		}
		if runtimeState.Verified {
			return runtimeState, nil
		}
		if time.Now().After(deadline) {
			// We've waited long enough; the pod never became ready in time.
			return nil, merrors.ErrRuntimeNotReady
		}

		// Wait for either the context to be cancelled (e.g. the HTTP request was
		// dropped by the client) or for the next poll tick.
		select {
		case <-ctx.Done():
			return nil, merrors.WrapStdServerError(ctx.Err(), "wait for userswarm verification")
		case <-time.After(c.config.PollInterval):
			// Next iteration: re-read the CR status.
		}
	}
}

// SendText forwards a user's chat message to the ZeroClaw pod's /webhook
// endpoint and returns the agent's text response.
//
// The function:
//  1. Validates that the runtime is marked Verified (the pod is healthy).
//  2. Encodes the message as JSON, optionally including AgentID and SystemPrompt.
//  3. Constructs the Kubernetes in-cluster DNS URL for the pod's Service.
//  4. Sends an HTTP POST with the JSON body and reads/decodes the response.
//
// The in-cluster URL form is:
//
//	http://<service-name>.<namespace>.svc.cluster.local:<port>/webhook
//
// This form is resolvable from any pod in the cluster, including the orchestrator.
//
//nolint:cyclop
func (c *userSwarmClient) SendText(ctx context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error) {
	// Basic nil/empty guards.
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return nil, merrors.ErrInvalidInput
	}
	// The Runtime must be Verified (Ready condition = true) and must have the
	// service coordinates we need to build the URL.  If either is missing the
	// pod is not ready to accept traffic yet.
	if !opts.Runtime.Verified || strings.TrimSpace(opts.Runtime.RuntimeNamespace) == "" || strings.TrimSpace(opts.Runtime.ServiceName) == "" {
		return nil, merrors.ErrRuntimeNotReady
	}

	// Build the request body.  AgentID and SystemPrompt use pointer fields so
	// they are omitted from the JSON entirely when empty (omitempty semantics).
	webhookReq := webhookRequest{Message: opts.Message}
	if opts.AgentID != "" {
		webhookReq.AgentID = &opts.AgentID
	}
	if opts.SystemPrompt != "" {
		webhookReq.SystemPrompt = &opts.SystemPrompt
	}
	payload, err := json.Marshal(&webhookReq)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "encode runtime webhook request")
	}

	// Build the in-cluster webhook URL using the Service name and namespace
	// from the UserSwarm status.  The .svc.cluster.local suffix is the standard
	// Kubernetes DNS search domain that makes service discovery work within the
	// cluster without needing external DNS.
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/webhook", opts.Runtime.ServiceName, opts.Runtime.RuntimeNamespace, c.config.Port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "build runtime webhook request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Forward the session ID so ZeroClaw can maintain conversation context across
	// multiple turns of the same session.  The header is optional; ZeroClaw
	// creates a new session if it is absent.
	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		httpReq.Header.Set("X-Session-Id", sessionID)
	}

	// Execute the HTTP call.  The http.Client has a 90-second timeout set at
	// construction time (defaultHTTPTimeout), so this will not block forever.
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "send runtime webhook request")
	}
	// Always drain and close the response body to prevent connection leaks.
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "read runtime webhook response")
	}

	// A non-200 status means ZeroClaw rejected the request or encountered an
	// internal error.  We include the body in the error so operators can see
	// what ZeroClaw said without needing to tail pod logs.
	if resp.StatusCode != http.StatusOK {
		return nil, merrors.WrapStdServerError(fmt.Errorf("runtime webhook returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))), "runtime webhook failed")
	}

	// Decode the JSON response envelope and return the agent turns.
	var webhookResp webhookResponse
	if err := json.Unmarshal(body, &webhookResp); err != nil {
		return nil, merrors.WrapStdServerError(err, "decode runtime webhook response")
	}

	// Filter out empty turns.
	var turns []AgentTurn
	for _, t := range webhookResp.Turns {
		if strings.TrimSpace(t.Text) != "" {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return nil, merrors.WrapStdServerError(fmt.Errorf("empty response from runtime"), "runtime returned no turns")
	}

	return turns, nil
}

// SendTextStream forwards a chat message to the ZeroClaw pod and returns
// a channel of streaming chunks. The channel is closed when the stream
// completes or the context is canceled.
func (c *userSwarmClient) SendTextStream(ctx context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error) {
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return nil, merrors.ErrInvalidInput
	}
	if !opts.Runtime.Verified {
		return nil, merrors.ErrRuntimeNotReady
	}
	if opts.Runtime.RuntimeNamespace == "" || opts.Runtime.ServiceName == "" {
		return nil, merrors.ErrInvalidInput
	}

	port := c.config.Port
	if port == 0 {
		port = DefaultRuntimePort
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/webhook/stream",
		opts.Runtime.ServiceName,
		opts.Runtime.RuntimeNamespace,
		port,
	)

	body := webhookRequest{Message: opts.Message}
	if opts.AgentID != "" {
		body.AgentID = &opts.AgentID
	}
	if opts.SystemPrompt != "" {
		body.SystemPrompt = &opts.SystemPrompt
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, merrors.NewServerError(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, merrors.NewServerError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.SessionID != "" {
		req.Header.Set("X-Session-Id", opts.SessionID)
	}

	resp, err := c.httpStreamClient.Do(req)
	if err != nil {
		return nil, merrors.NewServerError(err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, merrors.NewServerErrorText(fmt.Sprintf("webhook/stream returned %d", resp.StatusCode))
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue // skip malformed lines
			}

			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
		}
		// If the scanner encountered an I/O error (connection drop, buffer overflow),
		// the channel closes without a "done" chunk — caller treats as stream failure.
		if err := scanner.Err(); err != nil {
			slog.Warn("webhook/stream: scanner error",
				"error", err.Error(),
				"service", opts.Runtime.ServiceName,
			)
		}
	}()

	return ch, nil
}

// getRuntimeState reads the current UserSwarm CR from the cluster and converts
// its status fields into the orchestrator.RuntimeStatus domain type.
//
// This is a thin translation layer: the CRD status is Kubernetes-specific while
// RuntimeStatus is the shape consumed by the rest of the orchestrator (service
// layer, HTTP handlers, JSON responses to the mobile client).
//
// Called both from EnsureRuntime (during the polling loop and the fast-return
// path) and anywhere else that needs a fresh snapshot of runtime state.
func (c *userSwarmClient) getRuntimeState(ctx context.Context, swarmName string) (*orchestrator.RuntimeStatus, *merrors.Error) {
	var swarm crawblv1alpha1.UserSwarm
	if err := c.client.Get(ctx, k8sclient.ObjectKey{Name: swarmName}, &swarm); err != nil {
		return nil, merrors.WrapStdServerError(err, "get userswarm status")
	}

	// Translate the Kubernetes-native status into the orchestrator domain type.
	// ResolveRuntimeState maps (phase, verified) -> a RuntimeState enum value
	// that the mobile client displays as a human-readable status string.
	return &orchestrator.RuntimeStatus{
		SwarmName:        swarm.Name,
		RuntimeNamespace: swarm.Status.RuntimeNamespace,
		ServiceName:      swarm.Status.ServiceName,
		Phase:            swarm.Status.Phase,
		Verified:         isConditionTrue(swarm.Status.Conditions, readyConditionType),
		Status:           orchestrator.ResolveRuntimeState(swarm.Status.Phase, isConditionTrue(swarm.Status.Conditions, readyConditionType)),
	}, nil
}

// desiredUserSwarm constructs the UserSwarm CR that should exist in the cluster
// for the given workspace.  This is the "desired state" that EnsureRuntime
// compares against the actual CR in the cluster to decide whether to create or
// update.
//
// Notable design decisions encoded here:
//
//  1. TOML gateway override — the orchestrator always ensures the ZeroClaw
//     gateway binds 0.0.0.0 and disables pairing.  This is intentional: the pod
//     is not directly reachable from outside the cluster (Kubernetes NetworkPolicy
//     ensures that), so there is no security risk in binding all interfaces.
//     Binding localhost inside the pod would break the orchestrator's ability to
//     call the webhook over the cluster-internal Service IP.
//
//  2. e2e label — if the HTTP request was made by a user whose subject starts
//     with "e2e-" (injected by the auth middleware), the CR is tagged
//     crawbl.ai/e2e=true.  This lets the operator or CI pipelines apply
//     different scheduling, quotas, or cleanup policies for test runtimes without
//     touching production ones.
func (c *userSwarmClient) desiredUserSwarm(ctx context.Context, opts *EnsureRuntimeOpts) *crawblv1alpha1.UserSwarm {
	name := userswarmName(opts.WorkspaceID)

	// Use any operator-level TOML overrides from config, or fall back to the
	// minimal gateway section that keeps the pod reachable from inside the cluster.
	tomlOverrides := strings.TrimSpace(c.config.TOMLOverrides)
	if tomlOverrides == "" {
		// The runtime is internal-only because it stays behind the orchestrator and
		// a backend-only NetworkPolicy, not because ZeroClaw binds localhost. Keep
		// the gateway reachable on the pod network so the orchestrator can proxy it.
		tomlOverrides = "[gateway]\nhost = \"0.0.0.0\"\nrequire_pairing = false\nallow_public_bind = true"
	}

	labels := map[string]string{}
	// Read principal from context to detect e2e users. The auth middleware
	// stores it, so it's available throughout the request lifecycle.
	if principal, ok := httpserver.PrincipalFromContext(ctx); ok {
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
			// UserID is stored in the spec so the operator can label the pod for
			// per-user audit, routing, and cost attribution.
			UserID: opts.UserID,
			Placement: crawblv1alpha1.UserSwarmPlacementSpec{
				// All runtimes currently share a single namespace.  The namespace
				// name comes from config so it can be overridden per environment
				// without a code change.
				RuntimeNamespace: c.config.RuntimeNamespace,
			},
			Runtime: crawblv1alpha1.UserSwarmRuntimeSpec{
				Image:               c.config.Image,
				Mode:                crawblv1alpha1.DefaultRuntimeMode,
				Port:                c.config.Port,
				ImagePullSecretName: c.config.ImagePullSecretName,
			},
			Storage: crawblv1alpha1.UserSwarmStorageSpec{
				Size:             c.config.StorageSize,
				StorageClassName: c.config.StorageClassName,
			},
			Config: crawblv1alpha1.UserSwarmConfigSpec{
				DefaultProvider: c.config.DefaultProvider,
				DefaultModel:    c.config.DefaultModel,
				TOMLOverrides:   tomlOverrides,
			},
		},
	}

	// Only set EnvSecretRef when an env secret name is configured.  The secret
	// is typically managed by ESO and contains LLM provider API keys.  Omitting
	// the field when the name is blank avoids creating a broken reference.
	if secretName := strings.TrimSpace(c.config.EnvSecretName); secretName != "" {
		sw.Spec.Config.EnvSecretRef = &crawblv1alpha1.UserSwarmSecretRef{Name: secretName}
	}

	// Apply per-agent settings overrides from the orchestrator DB.
	// These flow through to the webhook's BuildConfigTOML where they override
	// the operator-level agent defaults before the final TOML is generated.
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

// DeleteRuntime deletes the UserSwarm CR for the given workspace ID.
// The operator's finalizer handles cleanup of all child resources.
//
// The function is idempotent: if the CR is already gone (either because it was
// never created or was deleted by a concurrent request) the function returns nil
// rather than an error.  This makes it safe to call on account deletion
// regardless of the current provisioning state.
func (c *userSwarmClient) DeleteRuntime(ctx context.Context, workspaceID string) *merrors.Error {
	if strings.TrimSpace(workspaceID) == "" {
		return merrors.ErrInvalidInput
	}

	name := userswarmName(workspaceID)
	var swarm crawblv1alpha1.UserSwarm
	err := c.client.Get(ctx, k8sclient.ObjectKey{Name: name}, &swarm)
	if err != nil {
		if k8sclient.IgnoreNotFound(err) == nil {
			// CR is already gone — nothing to do.
			return nil
		}
		return merrors.WrapStdServerError(err, "get userswarm for deletion")
	}

	if err := c.client.Delete(ctx, &swarm); err != nil {
		if k8sclient.IgnoreNotFound(err) == nil {
			// Deleted by someone else between our Get and Delete — that's fine.
			return nil
		}
		return merrors.WrapStdServerError(err, "delete userswarm")
	}

	return nil
}

// userswarmName derives the stable Kubernetes CR name for a workspace's swarm.
//
// The "workspace-" prefix namespaces the name in the cluster so it cannot clash
// with other CR types.  Lower-casing the workspace ID is required because
// Kubernetes resource names must be valid DNS subdomain labels (RFC 1123), which
// are case-insensitive and conventionally lowercase.
func userswarmName(workspaceID string) string {
	return "workspace-" + strings.ToLower(strings.TrimSpace(workspaceID))
}

// isConditionTrue scans a slice of Kubernetes meta conditions and returns true
// if the condition with the given type exists and its Status is "True".
//
// Kubernetes conditions are a standard extensibility mechanism: controllers
// append named conditions to a resource's status to communicate sub-states
// (e.g. "Ready", "PodScheduled").  We use this helper rather than accessing
// conditions by index because the slice order is not guaranteed.
func isConditionTrue(conditions []metav1.Condition, conditionType string) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == metav1.ConditionTrue
		}
	}
	// Condition not found at all — treat as false (not yet set by the operator).
	return false
}

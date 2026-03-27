// Package runtimeclient provides HTTP client for user swarm runtime.
package runtimeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Default HTTP client timeout for runtime API calls.
const defaultHTTPTimeout = 90 * time.Second

const verifiedConditionType = "Verified"

type userSwarmClient struct {
	client     client.Client
	config     UserSwarmConfig
	httpClient *http.Client
}

type webhookRequest struct {
	Message      string  `json:"message"`
	AgentID      *string `json:"agent_id,omitempty"`
	SystemPrompt *string `json:"system_prompt,omitempty"`
}

type webhookResponse struct {
	Response string `json:"response"`
}

func NewUserSwarmClient(cfg Config) (Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(crawblv1alpha1.AddToScheme(scheme))

	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

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
		client:     k8sClient,
		config:     userswarmCfg,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

//nolint:cyclop
func (c *userSwarmClient) EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error) {
	if opts == nil || strings.TrimSpace(opts.UserID) == "" || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	desired := c.desiredUserSwarm(opts)

	var actual crawblv1alpha1.UserSwarm
	err := c.client.Get(ctx, client.ObjectKey{Name: desired.Name}, &actual)
	switch {
	case client.IgnoreNotFound(err) != nil:
		return nil, merrors.WrapStdServerError(err, "get userswarm")
	case err != nil:
		if createErr := c.client.Create(ctx, desired); createErr != nil {
			return nil, merrors.WrapStdServerError(createErr, "create userswarm")
		}
		actual = *desired
	case !reflect.DeepEqual(actual.Spec, desired.Spec):
		actual.Spec = desired.Spec
		if updateErr := c.client.Update(ctx, &actual); updateErr != nil {
			return nil, merrors.WrapStdServerError(updateErr, "update userswarm")
		}
	}

	if !opts.WaitForVerified {
		refreshed, mErr := c.getRuntimeState(ctx, desired.Name)
		if mErr != nil {
			return nil, mErr
		}
		return refreshed, nil
	}

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
			return nil, merrors.ErrRuntimeNotReady
		}

		select {
		case <-ctx.Done():
			return nil, merrors.WrapStdServerError(ctx.Err(), "wait for userswarm verification")
		case <-time.After(c.config.PollInterval):
		}
	}
}

//nolint:cyclop
func (c *userSwarmClient) SendText(ctx context.Context, opts *SendTextOpts) (string, *merrors.Error) {
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return "", merrors.ErrInvalidInput
	}
	if !opts.Runtime.Verified || strings.TrimSpace(opts.Runtime.RuntimeNamespace) == "" || strings.TrimSpace(opts.Runtime.ServiceName) == "" {
		return "", merrors.ErrRuntimeNotReady
	}

	webhookReq := webhookRequest{Message: opts.Message}
	if opts.AgentID != "" {
		webhookReq.AgentID = &opts.AgentID
	}
	if opts.SystemPrompt != "" {
		webhookReq.SystemPrompt = &opts.SystemPrompt
	}
	payload, err := json.Marshal(&webhookReq)
	if err != nil {
		return "", merrors.WrapStdServerError(err, "encode runtime webhook request")
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/webhook", opts.Runtime.ServiceName, opts.Runtime.RuntimeNamespace, c.config.Port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", merrors.WrapStdServerError(err, "build runtime webhook request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		httpReq.Header.Set("X-Session-Id", sessionID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", merrors.WrapStdServerError(err, "send runtime webhook request")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", merrors.WrapStdServerError(err, "read runtime webhook response")
	}

	if resp.StatusCode != http.StatusOK {
		return "", merrors.WrapStdServerError(fmt.Errorf("runtime webhook returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))), "runtime webhook failed")
	}

	var webhookResp webhookResponse
	if err := json.Unmarshal(body, &webhookResp); err != nil {
		return "", merrors.WrapStdServerError(err, "decode runtime webhook response")
	}

	return webhookResp.Response, nil
}

func (c *userSwarmClient) getRuntimeState(ctx context.Context, swarmName string) (*orchestrator.RuntimeStatus, *merrors.Error) {
	var swarm crawblv1alpha1.UserSwarm
	if err := c.client.Get(ctx, client.ObjectKey{Name: swarmName}, &swarm); err != nil {
		return nil, merrors.WrapStdServerError(err, "get userswarm status")
	}

	return &orchestrator.RuntimeStatus{
		SwarmName:        swarm.Name,
		RuntimeNamespace: swarm.Status.RuntimeNamespace,
		ServiceName:      swarm.Status.ServiceName,
		Phase:            swarm.Status.Phase,
		Verified:         isConditionTrue(swarm.Status.Conditions, verifiedConditionType),
		Status:           orchestrator.ResolveRuntimeState(swarm.Status.Phase, isConditionTrue(swarm.Status.Conditions, verifiedConditionType)),
	}, nil
}

func (c *userSwarmClient) desiredUserSwarm(opts *EnsureRuntimeOpts) *crawblv1alpha1.UserSwarm {
	name := userswarmName(opts.WorkspaceID)
	tomlOverrides := strings.TrimSpace(c.config.TOMLOverrides)
	if tomlOverrides == "" {
		// The runtime is internal-only because it stays behind the orchestrator and
		// a backend-only NetworkPolicy, not because ZeroClaw binds localhost. Keep
		// the gateway reachable on the pod network so the orchestrator can proxy it.
		tomlOverrides = "[gateway]\nhost = \"0.0.0.0\"\nrequire_pairing = false\nallow_public_bind = true"
	}

	sw := &crawblv1alpha1.UserSwarm{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: crawblv1alpha1.UserSwarmSpec{
			UserID: opts.UserID,
			Placement: crawblv1alpha1.UserSwarmPlacementSpec{
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
			Exposure: crawblv1alpha1.UserSwarmExposureSpec{
				HTTPRoute: crawblv1alpha1.UserSwarmHTTPRouteSpec{
					Enabled: false,
				},
			},
		},
	}

	if secretName := strings.TrimSpace(c.config.EnvSecretName); secretName != "" {
		sw.Spec.Config.EnvSecretRef = &crawblv1alpha1.UserSwarmSecretRef{Name: secretName}
	}

	return sw
}

func userswarmName(workspaceID string) string {
	return "workspace-" + strings.ToLower(strings.TrimSpace(workspaceID))
}

func isConditionTrue(conditions []metav1.Condition, conditionType string) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

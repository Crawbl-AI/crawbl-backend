package client

import (
	"context"
	"net/http"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Default HTTP client timeout for runtime API calls.
const defaultHTTPTimeout = 90 * time.Second

const readyConditionType = "Ready"

type userSwarmClient struct {
	client     k8sclient.Client
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

type fakeClient struct {
	replyPrefix string
}

const (
	DriverFake      = "fake"
	DriverUserSwarm = "userswarm"

	DefaultFakeReplyPrefix          = "Fake runtime reply"
	DefaultRuntimeNamespace         = "userswarms"
	DefaultRuntimeStorageSize       = "2Gi"
	DefaultRuntimePort        int32 = 42617
	DefaultPollTimeout              = 60 * time.Second
	DefaultPollInterval             = 2 * time.Second
)

type Config struct {
	Driver          string
	FakeReplyPrefix string
	UserSwarm       UserSwarmConfig
}

type UserSwarmConfig struct {
	RuntimeNamespace    string
	Image               string
	ImagePullSecretName string
	StorageSize         string
	StorageClassName    string
	DefaultProvider     string
	DefaultModel        string
	EnvSecretName       string
	TOMLOverrides       string
	PollTimeout         time.Duration
	PollInterval        time.Duration
	Port                int32
}

type EnsureRuntimeOpts struct {
	UserID          string
	WorkspaceID     string
	WaitForVerified bool
}

type SendTextOpts struct {
	Runtime      *orchestrator.RuntimeStatus
	Message      string
	SessionID    string
	AgentID      string
	SystemPrompt string
}

type Client interface {
	EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error)
	SendText(ctx context.Context, opts *SendTextOpts) (string, *merrors.Error)
	// DeleteRuntime removes the UserSwarm CR for a workspace, triggering
	// the operator to clean up all child resources (StatefulSet, PVC, etc.).
	DeleteRuntime(ctx context.Context, workspaceID string) *merrors.Error
}

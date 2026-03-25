package runtimeclient

import (
	"context"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

const (
	DriverFake      = "fake"
	DriverUserSwarm = "userswarm"

	DefaultFakeReplyPrefix          = "Fake runtime reply"
	DefaultRuntimeNamespace         = "swarms-dev"
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
	Runtime   *orchestrator.RuntimeStatus
	Message   string
	SessionID string
}

type Client interface {
	EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error)
	SendText(ctx context.Context, opts *SendTextOpts) (string, *merrors.Error)
}

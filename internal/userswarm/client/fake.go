package client

import (
	"context"
	"fmt"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type fakeClient struct {
	replyPrefix string
}

func NewFakeClient(config Config) Client {
	replyPrefix := strings.TrimSpace(config.FakeReplyPrefix)
	if replyPrefix == "" {
		replyPrefix = DefaultFakeReplyPrefix
	}

	return &fakeClient{replyPrefix: replyPrefix}
}

func (c *fakeClient) EnsureRuntime(_ context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error) {
	if opts == nil || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	return &orchestrator.RuntimeStatus{
		SwarmName:        "fake-" + opts.WorkspaceID,
		RuntimeNamespace: "local",
		ServiceName:      "fake-runtime",
		Phase:            "Ready",
		Verified:         true,
		Status:           orchestrator.ResolveRuntimeState("Ready", true),
	}, nil
}

func (c *fakeClient) DeleteRuntime(_ context.Context, _ string) *merrors.Error {
	return nil // no-op for fake client
}

func (c *fakeClient) SendText(_ context.Context, opts *SendTextOpts) (string, *merrors.Error) {
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return "", merrors.ErrInvalidInput
	}

	return fmt.Sprintf("%s: %s", c.replyPrefix, opts.Message), nil
}

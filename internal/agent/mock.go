package agent

import (
	"context"
	"fmt"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// mockClient is the test/local-dev implementation of Client. It never touches
// any external service — it just echoes messages back with a configurable
// prefix. This makes it safe to run the orchestrator locally without a live
// agent runtime.
type mockClient struct {
	replyPrefix string
}

// NewMockClient constructs the test/local-dev Client implementation.
//
// mockClient satisfies the Client interface without touching any external
// service. It is used in two scenarios:
//
//  1. Unit tests — the orchestrator service layer can be exercised without a
//     live runtime by setting Driver = DriverFake in Config.
//  2. Local development — developers can run the orchestrator and exercise the
//     chat flow end-to-end; the mock simply echoes messages back.
//
// The FakeReplyPrefix field in Config lets callers customise the echo text so
// that test assertions can match a specific string without hard-coding the
// package-level default.
func NewMockClient(cfg Config) Client {
	replyPrefix := strings.TrimSpace(cfg.FakeReplyPrefix)
	if replyPrefix == "" {
		replyPrefix = DefaultFakeReplyPrefix
	}

	return &mockClient{replyPrefix: replyPrefix}
}

// EnsureRuntime immediately returns a synthetic RuntimeStatus that looks like a
// healthy, verified runtime. No external calls are made.
//
// The returned SwarmName is prefixed with "mock-" so log lines make it obvious
// that a real runtime is not involved. Phase is set to "Running" and Verified
// to true so callers that check those fields pass their guards.
func (c *mockClient) EnsureRuntime(_ context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error) {
	if opts == nil || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	return &orchestrator.RuntimeStatus{
		SwarmName:        "mock-" + opts.WorkspaceID,
		RuntimeNamespace: "local",
		ServiceName:      "mock-agent",
		Phase:            "Running",
		Verified:         true,
		Status:           orchestrator.ResolveRuntimeState("Running", true),
	}, nil
}

// DeleteRuntime is a no-op for the mock client.
//
// There is nothing to clean up because EnsureRuntime never created any real
// resources. Returning nil satisfies the idempotency contract of the Client
// interface.
func (c *mockClient) DeleteRuntime(_ context.Context, _ string) *merrors.Error {
	return nil
}

// SendText echoes the input message back to the caller with the configured
// reply prefix. No network call is made.
//
// The format "<prefix>: <message>" lets test assertions verify both that the
// message was received and that the correct mock client instance was used.
//
// Returns ErrInvalidInput if the required fields are missing, matching the
// validation behaviour of a real implementation so tests that exercise the
// validation path work against either implementation.
func (c *mockClient) SendText(_ context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error) {
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return nil, merrors.ErrInvalidInput
	}

	agentID := opts.AgentID
	if agentID == "" {
		agentID = "default"
	}

	return []AgentTurn{
		{AgentID: agentID, Text: fmt.Sprintf("%s: %s", c.replyPrefix, opts.Message)},
	}, nil
}

// SendTextStream returns a channel with a fake streaming response.
// It simulates streaming by splitting the reply into word-level chunks,
// followed by a done event.
func (c *mockClient) SendTextStream(_ context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}

	ch := make(chan StreamChunk, 4)
	go func() {
		defer close(ch)
		agentID := opts.AgentID
		if agentID == "" {
			agentID = "default"
		}
		text := c.replyPrefix + ": " + opts.Message
		words := strings.Fields(text)
		for _, word := range words {
			ch <- StreamChunk{
				Type:    StreamEventChunk,
				AgentID: agentID,
				Delta:   word + " ",
			}
		}
		ch <- StreamChunk{
			Type:    StreamEventDone,
			AgentID: agentID,
			Model:   "mock",
		}
	}()
	return ch, nil
}

// ListMemories returns an empty slice for the mock client.
func (c *mockClient) ListMemories(_ context.Context, _ *ListMemoriesOpts) ([]MemoryEntry, *merrors.Error) {
	return []MemoryEntry{}, nil
}

// DeleteMemory is a no-op for the mock client.
func (c *mockClient) DeleteMemory(_ context.Context, _ *DeleteMemoryOpts) *merrors.Error {
	return nil
}

// CreateMemory is a no-op for the mock client.
func (c *mockClient) CreateMemory(_ context.Context, _ *CreateMemoryOpts) *merrors.Error {
	return nil
}

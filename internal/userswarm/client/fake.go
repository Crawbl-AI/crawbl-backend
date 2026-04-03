package client

import (
	"context"
	"fmt"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// NewFakeClient constructs the test/local-dev Client implementation.
//
// fakeClient satisfies the Client interface without touching Kubernetes or any
// real ZeroClaw pod.  It is used in two scenarios:
//
//  1. Unit tests — the orchestrator service layer can be exercised without a
//     live cluster by setting Driver = DriverFake in Config.
//  2. Local development — developers who do not have a local k8s cluster can
//     still run the orchestrator and exercise the chat flow end-to-end; the fake
//     simply echoes messages back.
//
// The FakeReplyPrefix field in Config lets callers customise the echo text so
// that test assertions can match a specific string without hard-coding the
// package-level default.
func NewFakeClient(config Config) Client {
	replyPrefix := strings.TrimSpace(config.FakeReplyPrefix)
	if replyPrefix == "" {
		replyPrefix = DefaultFakeReplyPrefix
	}

	return &fakeClient{replyPrefix: replyPrefix}
}

// EnsureRuntime immediately returns a synthetic RuntimeStatus that looks like a
// healthy, verified runtime.  No Kubernetes API calls are made.
//
// The returned SwarmName is prefixed with "fake-" so log lines make it obvious
// that a real pod is not involved.  Phase is set to "Ready" and Verified to
// true so callers that check those fields (e.g. SendText) pass their guards.
//
// The context parameter is intentionally unused (_) because there is nothing
// asynchronous to cancel.
func (c *fakeClient) EnsureRuntime(_ context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error) {
	// WorkspaceID is the minimum required field — without it we cannot derive
	// a stable SwarmName, which downstream code may use for logging/tracing.
	if opts == nil || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	return &orchestrator.RuntimeStatus{
		SwarmName: "fake-" + opts.WorkspaceID,
		// "local" signals in logs that this is a fake namespace, not a real one.
		RuntimeNamespace: "local",
		ServiceName:      "fake-runtime",
		Phase:            "Ready",
		// Always report Verified=true so callers that gate on readiness (like
		// SendText and the workspace handler) can proceed without a real pod.
		Verified: true,
		Status:   orchestrator.ResolveRuntimeState("Ready", true),
	}, nil
}

// DeleteRuntime is a no-op for the fake client.
//
// There is nothing to clean up because EnsureRuntime never created any real
// Kubernetes resources.  Returning nil satisfies the idempotency contract of
// the Client interface.
func (c *fakeClient) DeleteRuntime(_ context.Context, _ string) *merrors.Error {
	return nil // no-op for fake client
}

// SendTextStream returns a channel with a single fake streaming response.
func (f *fakeClient) SendTextStream(_ context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}

	ch := make(chan StreamChunk, 4)
	go func() {
		defer close(ch)
		agentID := opts.AgentID
		if agentID == "" {
			agentID = "manager"
		}
		text := f.replyPrefix + ": " + opts.Message
		// Simulate streaming by splitting into chunks.
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
			Model:   "fake",
		}
	}()
	return ch, nil
}

// SendText echoes the input message back to the caller with the configured
// reply prefix.  No network call is made.
//
// The format "<prefix>: <message>" lets test assertions verify both that the
// message was received and that the correct fake client instance was used.
//
// Returns ErrInvalidInput if the required fields are missing, matching the
// validation behaviour of the real userSwarmClient so tests that exercise the
// validation path work against either implementation.
func (c *fakeClient) SendText(_ context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error) {
	// Mirror the same nil/empty guards as the real implementation so the fake
	// is a faithful stand-in for validation testing.
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return nil, merrors.ErrInvalidInput
	}

	// Echo the message back as a single manager turn so the caller can see it
	// was processed.  The real implementation returns turns from ZeroClaw agents.
	return []AgentTurn{
		{AgentID: "manager", Text: fmt.Sprintf("%s: %s", c.replyPrefix, opts.Message)},
	}, nil
}

// ListMemories returns an empty slice for the fake client.
func (f *fakeClient) ListMemories(_ context.Context, _ *ListMemoriesOpts) ([]MemoryEntry, *merrors.Error) {
	return []MemoryEntry{}, nil
}

// DeleteMemory is a no-op for the fake client.
func (f *fakeClient) DeleteMemory(_ context.Context, _ *DeleteMemoryOpts) *merrors.Error {
	return nil
}

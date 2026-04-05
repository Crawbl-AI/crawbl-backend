package client

import (
	"context"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"

	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
)

// ListMemories calls the runtime pod's Memory.ListMemories gRPC RPC.
// Auth metadata rides in via crawblgrpc.WithIdentity stamped onto the
// request ctx — no manual header plumbing. The HMACCredentials bound
// to the pool's ClientConn at dial time handles the rest.
func (c *userSwarmClient) ListMemories(ctx context.Context, opts *ListMemoriesOpts) ([]MemoryEntry, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	if err := validateMemoryRuntime(opts.Runtime); err != nil {
		return nil, err
	}

	conn, cerr := c.conn(ctx, &runtimeCoord{
		serviceName: opts.Runtime.ServiceName,
		namespace:   opts.Runtime.RuntimeNamespace,
	})
	if cerr != nil {
		return nil, wrapGRPCError(cerr, "dial runtime")
	}

	authedCtx := crawblgrpc.WithIdentity(ctx, crawblgrpc.Identity{
		Subject: opts.Runtime.UserID,
		Object:  opts.Runtime.WorkspaceID,
	})

	client := runtimev1.NewMemoryClient(conn)
	resp, err := client.ListMemories(authedCtx, &runtimev1.ListMemoriesRequest{
		WorkspaceId: opts.Runtime.WorkspaceID,
		Category:    opts.Category,
		Limit:       int32(opts.Limit),
		Offset:      int32(opts.Offset),
	})
	if err != nil {
		return nil, wrapGRPCError(err, "list memories")
	}

	entries := make([]MemoryEntry, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		if e == nil {
			continue
		}
		entries = append(entries, MemoryEntry{
			Key:       e.GetKey(),
			Content:   e.GetContent(),
			Category:  e.GetCategory(),
			CreatedAt: formatProtoTimestamp(e.GetCreatedAt()),
			UpdatedAt: formatProtoTimestamp(e.GetUpdatedAt()),
		})
	}
	return entries, nil
}

// CreateMemory calls Memory.CreateMemory with the given key + content.
func (c *userSwarmClient) CreateMemory(ctx context.Context, opts *CreateMemoryOpts) *merrors.Error {
	if opts == nil || opts.Key == "" || opts.Content == "" {
		return merrors.ErrInvalidInput
	}
	if err := validateMemoryRuntime(opts.Runtime); err != nil {
		return err
	}

	conn, cerr := c.conn(ctx, &runtimeCoord{
		serviceName: opts.Runtime.ServiceName,
		namespace:   opts.Runtime.RuntimeNamespace,
	})
	if cerr != nil {
		return wrapGRPCError(cerr, "dial runtime")
	}

	authedCtx := crawblgrpc.WithIdentity(ctx, crawblgrpc.Identity{
		Subject: opts.Runtime.UserID,
		Object:  opts.Runtime.WorkspaceID,
	})

	client := runtimev1.NewMemoryClient(conn)
	if _, err := client.CreateMemory(authedCtx, &runtimev1.CreateMemoryRequest{
		WorkspaceId: opts.Runtime.WorkspaceID,
		Key:         opts.Key,
		Content:     opts.Content,
		Category:    opts.Category,
	}); err != nil {
		return wrapGRPCError(err, "create memory")
	}
	return nil
}

// DeleteMemory calls Memory.DeleteMemory for the given key.
func (c *userSwarmClient) DeleteMemory(ctx context.Context, opts *DeleteMemoryOpts) *merrors.Error {
	if opts == nil || opts.Key == "" {
		return merrors.ErrInvalidInput
	}
	if err := validateMemoryRuntime(opts.Runtime); err != nil {
		return err
	}

	conn, cerr := c.conn(ctx, &runtimeCoord{
		serviceName: opts.Runtime.ServiceName,
		namespace:   opts.Runtime.RuntimeNamespace,
	})
	if cerr != nil {
		return wrapGRPCError(cerr, "dial runtime")
	}

	authedCtx := crawblgrpc.WithIdentity(ctx, crawblgrpc.Identity{
		Subject: opts.Runtime.UserID,
		Object:  opts.Runtime.WorkspaceID,
	})

	client := runtimev1.NewMemoryClient(conn)
	if _, err := client.DeleteMemory(authedCtx, &runtimev1.DeleteMemoryRequest{
		WorkspaceId: opts.Runtime.WorkspaceID,
		Key:         opts.Key,
	}); err != nil {
		return wrapGRPCError(err, "delete memory")
	}
	return nil
}

// validateMemoryRuntime is the shared precondition check for every
// Memory RPC: runtime must be non-nil and Verified, service + namespace
// must be populated (so the pool can build a target), and the identity
// fields (UserID, WorkspaceID) must be set so HMAC signing works.
//
// Shared across List/Create/Delete because they all require the same
// state before dialing.
func validateMemoryRuntime(runtime *orchestrator.RuntimeStatus) *merrors.Error {
	if runtime == nil {
		return merrors.ErrInvalidInput
	}
	if !runtime.Verified {
		return merrors.ErrRuntimeNotReady
	}
	if strings.TrimSpace(runtime.RuntimeNamespace) == "" || strings.TrimSpace(runtime.ServiceName) == "" {
		return merrors.ErrInvalidInput
	}
	if strings.TrimSpace(runtime.UserID) == "" || strings.TrimSpace(runtime.WorkspaceID) == "" {
		return merrors.NewServerErrorText("runtime missing identity (EnsureRuntime must stamp UserID + WorkspaceID)")
	}
	return nil
}

// formatProtoTimestamp renders a google.protobuf.Timestamp into an
// RFC3339 UTC string. Returns "" for nil / zero timestamps so the
// orchestrator's JSON layer omits the field.
func formatProtoTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	t := ts.AsTime()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

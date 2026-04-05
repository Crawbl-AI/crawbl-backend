package server

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/memory"
	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
)

// memoryServer is the gRPC handler for runtimev1.MemoryServer. It is a
// thin translation layer between proto messages and memory.Store — every
// real storage concern (persistence, concurrency, ordering, pagination)
// lives in the Store implementation behind the interface, so swapping
// the concrete backend does not touch this file.
type memoryServer struct {
	runtimev1.UnimplementedMemoryServer
	logger *slog.Logger
	store  memory.Store
}

// newMemoryServer is the server package's private constructor for the
// memory gRPC handler. Exposed to grpc_server.go via New() wiring.
func newMemoryServer(logger *slog.Logger, store memory.Store) *memoryServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &memoryServer{logger: logger, store: store}
}

// ListMemories implements runtimev1.MemoryServer.
func (s *memoryServer) ListMemories(ctx context.Context, req *runtimev1.ListMemoriesRequest) (*runtimev1.ListMemoriesResponse, error) {
	workspaceID, err := resolveWorkspaceID(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	entries, err := s.store.List(ctx, workspaceID, memory.ListFilter{
		Category: req.GetCategory(),
		Limit:    int(req.GetLimit()),
		Offset:   int(req.GetOffset()),
	})
	if err != nil {
		return nil, memoryErrToStatus(err)
	}
	out := make([]*runtimev1.MemoryEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, toProtoEntry(e))
	}
	return &runtimev1.ListMemoriesResponse{Entries: out}, nil
}

// CreateMemory implements runtimev1.MemoryServer.
func (s *memoryServer) CreateMemory(ctx context.Context, req *runtimev1.CreateMemoryRequest) (*runtimev1.CreateMemoryResponse, error) {
	workspaceID, err := resolveWorkspaceID(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "memory: key is required")
	}
	entry, err := s.store.Create(ctx, workspaceID, memory.Entry{
		Key:      req.GetKey(),
		Content:  req.GetContent(),
		Category: req.GetCategory(),
	})
	if err != nil {
		return nil, memoryErrToStatus(err)
	}
	return &runtimev1.CreateMemoryResponse{Entry: toProtoEntry(entry)}, nil
}

// DeleteMemory implements runtimev1.MemoryServer.
func (s *memoryServer) DeleteMemory(ctx context.Context, req *runtimev1.DeleteMemoryRequest) (*runtimev1.DeleteMemoryResponse, error) {
	workspaceID, err := resolveWorkspaceID(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "memory: key is required")
	}
	if err := s.store.Delete(ctx, workspaceID, req.GetKey()); err != nil {
		return nil, memoryErrToStatus(err)
	}
	return &runtimev1.DeleteMemoryResponse{}, nil
}

// resolveWorkspaceID prefers the value carried in the authenticated
// Identity (crawblgrpc.IdentityFromContext) over any caller-supplied
// workspace_id, so an agent can't read or mutate memories outside its
// own workspace even if the request body lies. If the request carries
// a workspace_id that disagrees with the identity's workspace, the
// identity wins silently — token identity is authoritative.
//
// The Identity.Object field carries the workspace ID in crawbl's HMAC
// scheme (Subject = userID, Object = workspaceID).
func resolveWorkspaceID(ctx context.Context, requested string) (string, error) {
	if id, ok := crawblgrpc.IdentityFromContext(ctx); ok && id.Object != "" {
		return id.Object, nil
	}
	if requested == "" {
		return "", status.Error(codes.InvalidArgument, "memory: workspace_id is required")
	}
	return requested, nil
}

// memoryErrToStatus translates memory.Store errors into gRPC status
// codes. Unknown errors are mapped to codes.Internal so callers see
// structured failures even when the store invents a new error class.
func memoryErrToStatus(err error) error {
	switch {
	case errors.Is(err, memory.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, memory.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// toProtoEntry marshals a memory.Entry into its proto counterpart.
// Timestamps are converted via timestamppb; zero times map to zero
// timestamp (nil is acceptable per proto conventions).
func toProtoEntry(e memory.Entry) *runtimev1.MemoryEntry {
	out := &runtimev1.MemoryEntry{
		Key:      e.Key,
		Content:  e.Content,
		Category: e.Category,
	}
	if !e.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(e.CreatedAt)
	}
	if !e.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(e.UpdatedAt)
	}
	return out
}

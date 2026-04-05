package server

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
)

// agentRuntimeStub is a placeholder implementation of
// runtimev1.AgentRuntimeServer for US-AR-003. Every RPC returns
// codes.Unimplemented; US-AR-009 replaces this file with the real Converse
// bidi stream wired to the ADK runner.
type agentRuntimeStub struct {
	runtimev1.UnimplementedAgentRuntimeServer
	logger *slog.Logger
}

func (s *agentRuntimeStub) Converse(stream runtimev1.AgentRuntime_ConverseServer) error {
	p, _ := PrincipalFromContext(stream.Context())
	s.logger.Info("Converse called (stub)", "user_id", p.UserID, "workspace_id", p.WorkspaceID)
	return status.Error(codes.Unimplemented, "Converse is not yet implemented (US-AR-009)")
}

// memoryStub is a placeholder implementation of runtimev1.MemoryServer for
// US-AR-003. US-AR-007 replaces the methods with the orchestrator-backed
// memory facade.
type memoryStub struct {
	runtimev1.UnimplementedMemoryServer
	logger *slog.Logger
}

func (s *memoryStub) ListMemories(ctx context.Context, req *runtimev1.ListMemoriesRequest) (*runtimev1.ListMemoriesResponse, error) {
	p, _ := PrincipalFromContext(ctx)
	s.logger.Info("ListMemories called (stub)", "user_id", p.UserID, "workspace_id", p.WorkspaceID, "category", req.GetCategory())
	return nil, status.Error(codes.Unimplemented, "ListMemories is not yet implemented (US-AR-007)")
}

func (s *memoryStub) CreateMemory(ctx context.Context, req *runtimev1.CreateMemoryRequest) (*runtimev1.CreateMemoryResponse, error) {
	p, _ := PrincipalFromContext(ctx)
	s.logger.Info("CreateMemory called (stub)", "user_id", p.UserID, "workspace_id", p.WorkspaceID, "key", req.GetKey())
	return nil, status.Error(codes.Unimplemented, "CreateMemory is not yet implemented (US-AR-007)")
}

func (s *memoryStub) DeleteMemory(ctx context.Context, req *runtimev1.DeleteMemoryRequest) (*runtimev1.DeleteMemoryResponse, error) {
	p, _ := PrincipalFromContext(ctx)
	s.logger.Info("DeleteMemory called (stub)", "user_id", p.UserID, "workspace_id", p.WorkspaceID, "key", req.GetKey())
	return nil, status.Error(codes.Unimplemented, "DeleteMemory is not yet implemented (US-AR-007)")
}

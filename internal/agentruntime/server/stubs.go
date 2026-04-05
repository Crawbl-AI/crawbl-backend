package server

import (
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
)

// agentRuntimeStub is a placeholder implementation of
// runtimev1.AgentRuntimeServer. Converse returns codes.Unimplemented until
// US-AR-009 wires the ADK runner through to the gRPC bidi stream.
//
// The Memory service was previously stubbed here too; US-AR-007 replaced
// it with the real memoryServer in memory.go.
type agentRuntimeStub struct {
	runtimev1.UnimplementedAgentRuntimeServer
	logger *slog.Logger
}

func (s *agentRuntimeStub) Converse(stream runtimev1.AgentRuntime_ConverseServer) error {
	p, _ := PrincipalFromContext(stream.Context())
	s.logger.Info("Converse called (stub)", "user_id", p.UserID, "workspace_id", p.WorkspaceID)
	return status.Error(codes.Unimplemented, "Converse is not yet implemented (US-AR-009)")
}

package server

import (
	"google.golang.org/grpc/health"
)

// grpcHealthServer aliases the upstream health server type so the rest of
// server/ can refer to it without importing the grpc/health package.
type grpcHealthServer = health.Server

// newGRPCHealthServer constructs an empty health server in the default
// state (no services registered; callers flip statuses via SetServingStatus).
func newGRPCHealthServer() *grpcHealthServer {
	return health.NewServer()
}

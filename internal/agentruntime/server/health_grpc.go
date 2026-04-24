package server

import (
	"google.golang.org/grpc/health"
)

// newGRPCHealthServer constructs an empty health server in the default
// state (no services registered; callers flip statuses via SetServingStatus).
func newGRPCHealthServer() *grpcHealthServer {
	return health.NewServer()
}

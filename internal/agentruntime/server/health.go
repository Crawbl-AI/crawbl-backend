package server

import (
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// NewHealthServer returns a HealthServer in the NOT_SERVING state.
func NewHealthServer() *HealthServer {
	s := newGRPCHealthServer()
	// The empty service name is the conventional "overall" status key.
	s.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	return &HealthServer{inner: s}
}

// SetServing flips the overall status to SERVING. Called by the runtime once
// the agent graph is constructed and ready to handle Converse streams.
func (h *HealthServer) SetServing() {
	h.inner.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
}

// SetNotServing flips the overall status back to NOT_SERVING. Used during
// graceful shutdown so load balancers stop routing new traffic.
func (h *HealthServer) SetNotServing() {
	h.inner.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
}

// Inner exposes the underlying *grpc/health.Server for registration with a
// grpc.Server. Kept as a method rather than a field so the inner type can
// evolve without breaking callers.
func (h *HealthServer) Inner() healthpb.HealthServer {
	return h.inner
}

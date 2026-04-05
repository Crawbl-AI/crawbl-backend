package server

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/memory"
	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
)

// Server is the top-level gRPC server wrapper for crawbl-agent-runtime. It
// owns the net.Listener, the *grpc.Server, the HealthServer, the in-memory
// Memory store, and the runner.Runner that drives Converse turns.
// main.go constructs one Server via New(), calls Start() in a goroutine,
// and Shutdown() on SIGTERM.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	listener net.Listener
	grpcSrv  *grpc.Server
	health   *HealthServer
	memStore memory.Store
	runner   *runner.Runner
}

// New wires a Server ready to Start(). It registers the HMAC auth
// interceptor, the health service, the real AgentRuntime handler (wired
// to the provided runner), and the Memory service. It does NOT open
// the listener — Start() does that.
//
// The caller owns the runner — pass nil only for tests that never
// exercise Converse. The health server flips to SERVING inside main.go
// immediately after New returns, on the assumption that the runner was
// constructed successfully (because a nil runner produces a handler
// that returns codes.Unavailable, which is the correct unhealthy
// state).
func New(cfg config.Config, logger *slog.Logger, r *runner.Runner) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	unary, stream := HMACAuth(cfg.MCPSigningKey)
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unary),
		grpc.ChainStreamInterceptor(stream),
	)

	healthSrv := NewHealthServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv.Inner())

	memStore := memory.NewInMemoryStore(nil)

	runtimev1.RegisterAgentRuntimeServer(grpcSrv, newConverseHandler(logger, r))
	runtimev1.RegisterMemoryServer(grpcSrv, newMemoryServer(logger, memStore))

	return &Server{
		cfg:      cfg,
		logger:   logger,
		grpcSrv:  grpcSrv,
		health:   healthSrv,
		memStore: memStore,
		runner:   r,
	}, nil
}

// Start binds the listener and begins serving. Blocks until the server
// exits. main.go is expected to call Start() in its own goroutine.
func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.cfg.GRPCListen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.GRPCListen, err)
	}
	s.listener = l
	s.logger.Info("agent runtime gRPC listening", "addr", s.cfg.GRPCListen)
	return s.grpcSrv.Serve(l)
}

// Health exposes the HealthServer so callers (e.g. the runner in US-AR-009)
// can flip the status to SERVING once the agent graph is loaded.
func (s *Server) Health() *HealthServer {
	return s.health
}

// Shutdown initiates a graceful stop of the gRPC server. In-flight RPCs are
// allowed to finish up to the graceful shutdown timeout configured in
// cfg.Startup; after that, the server force-closes. The in-memory memory
// store is closed last (no-op today, but US-AR-007's facade will evolve
// in Phase 2 to a network-backed store that needs explicit cleanup).
func (s *Server) Shutdown() {
	if s == nil || s.grpcSrv == nil {
		return
	}
	s.health.SetNotServing()
	done := make(chan struct{})
	go func() {
		s.grpcSrv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		s.logger.Info("agent runtime gRPC stopped gracefully")
	case <-timerAfter(s.cfg.Startup.GracefulShutdownTimeout):
		s.logger.Warn("agent runtime gRPC graceful shutdown timed out, forcing stop")
		s.grpcSrv.Stop()
	}
	if s.memStore != nil {
		if err := s.memStore.Close(); err != nil {
			s.logger.Warn("memory store close returned error", "error", err)
		}
	}
}

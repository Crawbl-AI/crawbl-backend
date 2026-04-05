package server

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
)

// Server is the top-level gRPC server wrapper for crawbl-agent-runtime. It
// owns the net.Listener, the *grpc.Server, and the HealthServer. main.go
// constructs one Server via New(), calls Start() in a goroutine, and
// Shutdown() on SIGTERM.
//
// Concrete AgentRuntime and Memory service implementations are stubs in
// this iteration (US-AR-003). US-AR-009 fills in the Converse bidi stream
// against the ADK runner.
type Server struct {
	cfg       config.Config
	logger    *slog.Logger
	listener  net.Listener
	grpcSrv   *grpc.Server
	health    *HealthServer
}

// New wires a Server ready to Start(). It registers the HMAC auth
// interceptor, the health service, and the stub AgentRuntime / Memory
// services. It does NOT open the listener — Start() does that.
func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
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

	runtimev1.RegisterAgentRuntimeServer(grpcSrv, &agentRuntimeStub{logger: logger})
	runtimev1.RegisterMemoryServer(grpcSrv, &memoryStub{logger: logger})

	return &Server{
		cfg:     cfg,
		logger:  logger,
		grpcSrv: grpcSrv,
		health:  healthSrv,
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
// cfg.Startup; after that, the server force-closes.
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
}

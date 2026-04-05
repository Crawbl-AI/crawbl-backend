package server

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/memory"
	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
)

// Server is the top-level gRPC server wrapper for crawbl-agent-runtime.
// It owns the net.Listener, the *grpc.Server, the HealthServer, and
// holds references to the injected runner + memory store so Shutdown
// can tear them down in the correct order. main.go constructs one
// Server via New(), calls Start() in a goroutine, and Shutdown() on
// SIGTERM.
//
// Every piece of generic gRPC infrastructure (HMAC auth interceptor,
// graceful shutdown, PerRPC credentials symmetry with the client)
// lives in internal/pkg/grpc. This package only contains
// agentruntime-specific wiring: the Server struct, the Converse +
// Memory service handlers, and the HealthServer lifecycle.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	listener net.Listener
	grpcSrv  *grpc.Server
	health   *HealthServer
	memStore memory.Store
	runner   *runner.Runner
}

// Deps bundles the dependencies main.go constructs before calling New.
// Passing them through a single struct keeps the server package free
// of direct Postgres / Redis / model imports and makes the dependency
// graph obvious at the wiring site.
type Deps struct {
	// Runner drives Converse turns. Required.
	Runner *runner.Runner
	// MemStore backs the gRPC Memory service. Required.
	MemStore memory.Store
	// Logger for the server. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// New wires a Server ready to Start(). It installs the shared
// internal/pkg/grpc HMAC auth interceptor, registers the health +
// reflection services, and registers the AgentRuntime + Memory
// handlers against the injected dependencies. It does NOT open the
// listener — Start() does that.
//
// main.go calls srv.Health().SetServing() right after New returns once
// every dependency is constructed so Kubernetes probes see the pod as
// Ready.
func New(cfg config.Config, deps Deps) (*Server, error) {
	if deps.Runner == nil {
		return nil, fmt.Errorf("server: Deps.Runner is required")
	}
	if deps.MemStore == nil {
		return nil, fmt.Errorf("server: Deps.MemStore is required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	unary, stream := crawblgrpc.NewHMACServerAuth(cfg.MCPSigningKey, nil)
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unary),
		grpc.ChainStreamInterceptor(stream),
	)

	healthSrv := NewHealthServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv.Inner())

	runtimev1.RegisterAgentRuntimeServer(grpcSrv, newConverseHandler(logger, deps.Runner))
	runtimev1.RegisterMemoryServer(grpcSrv, newMemoryServer(logger, deps.MemStore))

	// Register gRPC server reflection so local debugging tools
	// (grpcurl, evans) can enumerate services without a .proto file.
	// Reflection paths are in crawblgrpc.DefaultAuthExemptMethods so
	// they bypass HMAC auth, and reflection.Register is idempotent.
	// Safe to leave on in production — it exposes service/method
	// names (which are not secret) but does NOT expose any RPCs
	// beyond what is already registered.
	reflection.Register(grpcSrv)

	return &Server{
		cfg:      cfg,
		logger:   logger,
		grpcSrv:  grpcSrv,
		health:   healthSrv,
		memStore: deps.MemStore,
		runner:   deps.Runner,
	}, nil
}

// Start binds the listener and begins serving. Blocks until the
// server exits. main.go calls Start() in its own goroutine.
func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.cfg.GRPCListen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.GRPCListen, err)
	}
	s.listener = l
	s.logger.Info("agent runtime gRPC listening", "addr", s.cfg.GRPCListen)
	return s.grpcSrv.Serve(l)
}

// Health exposes the HealthServer so main.go can flip the status to
// SERVING once the agent graph is loaded.
func (s *Server) Health() *HealthServer {
	return s.health
}

// Shutdown initiates a graceful stop of the gRPC server. In-flight
// RPCs are allowed to finish up to cfg.Startup.GracefulShutdownTimeout;
// after that, the server force-closes. The runner and memory store
// are torn down after the gRPC server has drained so active turns
// can complete against still-live backends.
//
// The graceful-stop dance itself lives in internal/pkg/grpc so every
// gRPC server in crawbl-backend shares the same implementation.
func (s *Server) Shutdown() {
	if s == nil || s.grpcSrv == nil {
		return
	}
	s.health.SetNotServing()
	crawblgrpc.GracefulShutdown(s.grpcSrv, s.cfg.Startup.GracefulShutdownTimeout, s.logger)
	if s.runner != nil {
		if err := s.runner.Close(); err != nil {
			s.logger.Warn("runner close returned error", "error", err)
		}
	}
	if s.memStore != nil {
		if err := s.memStore.Close(); err != nil {
			s.logger.Warn("memory store close returned error", "error", err)
		}
	}
}

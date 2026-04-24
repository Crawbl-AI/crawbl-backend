package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/agentruntime/v1"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
)

// New wires a Server ready to Start(). It installs the shared
// internal/pkg/grpc HMAC auth interceptor, registers the health +
// reflection services, and registers the AgentRuntime handler against
// the injected dependencies. It does NOT open the listener — Start()
// does that.
//
// main.go calls srv.Health().SetServing() right after New returns once
// every dependency is constructed so Kubernetes probes see the pod as
// Ready.
func New(cfg config.Config, deps Deps) (*Server, error) {
	if deps.Runner == nil {
		return nil, fmt.Errorf("server: Deps.Runner is required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	unary, stream := crawblgrpc.NewHMACServerAuth(cfg.MCPSigningKey, nil)
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unary),
		grpc.ChainStreamInterceptor(stream),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     serverMaxConnectionIdle,
			MaxConnectionAge:      serverMaxConnectionAge,
			MaxConnectionAgeGrace: serverMaxConnectionAgeGrace,
			Time:                  serverKeepaliveTime,
			Timeout:               serverKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             serverMinKeepaliveTime,
			PermitWithoutStream: true,
		}),
		grpc.MaxConcurrentStreams(serverMaxConcurrentStreams),
	)

	healthSrv := NewHealthServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv.Inner())

	runtimev1.RegisterAgentRuntimeServer(grpcSrv, newConverseHandler(logger, deps.Runner))

	// Register gRPC server reflection so local debugging tools
	// (grpcurl, evans) can enumerate services without a .proto file.
	// Reflection paths are in crawblgrpc.DefaultAuthExemptMethods so
	// they bypass HMAC auth, and reflection.Register is idempotent.
	// Safe to leave on in production — it exposes service/method
	// names (which are not secret) but does NOT expose any RPCs
	// beyond what is already registered.
	reflection.Register(grpcSrv)

	return &Server{
		cfg:     cfg,
		logger:  logger,
		grpcSrv: grpcSrv,
		health:  healthSrv,
		runner:  deps.Runner,
	}, nil
}

// Start binds the listener and begins serving. Blocks until the
// server exits. main.go calls Start() in its own goroutine.
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.lifecycleCancel = cancel
	lc := &net.ListenConfig{}
	l, err := lc.Listen(ctx, "tcp", s.cfg.GRPCListen)
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
// after that, the server force-closes. The runner is torn down after
// the gRPC server has drained so active turns can complete against
// still-live backends.
//
// The graceful-stop dance itself lives in internal/pkg/grpc so every
// gRPC server in crawbl-backend shares the same implementation.
func (s *Server) Shutdown() {
	if s == nil || s.grpcSrv == nil {
		return
	}
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	s.health.SetNotServing()
	crawblgrpc.GracefulShutdown(s.grpcSrv, s.cfg.Startup.GracefulShutdownTimeout, s.logger)
	if s.runner != nil {
		if err := s.runner.Close(); err != nil {
			s.logger.Warn("runner close returned error", "error", err)
		}
	}
}

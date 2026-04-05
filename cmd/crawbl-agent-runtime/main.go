// Command crawbl-agent-runtime is the second binary in the crawbl-backend
// module. It runs as the per-workspace agent runtime pod — one instance per
// user's swarm — and replaces the Rust ZeroClaw runtime in Phase 2.
//
// Runtime responsibilities:
//   - Host a multi-agent swarm (Manager + Wally + Eve) built on ADK-Go
//     (google.golang.org/adk). Agents and composition land in US-AR-008.
//   - Serve a gRPC bidi stream for conversational turns on port 42618. The
//     Converse implementation lands in US-AR-009.
//   - Talk back to the orchestrator's MCP server at /mcp/v1 over HMAC-signed
//     HTTP for orchestrator-mediated tools (user profile, agent history,
//     etc.). The MCP bridge lands in US-AR-006.
//   - Persist all durable state in Postgres via the orchestrator; keep
//     ephemeral caches in /cache (emptyDir) and /tmp (tmpfs); artifacts go
//     to DigitalOcean Spaces via the S3 protocol client.
//   - No per-user PVC anywhere in this process.
//
// This file handles process lifecycle only: parse config, construct the
// gRPC server, start serving, wait for SIGINT/SIGTERM, shut down gracefully.
// All business logic lives under internal/agentruntime/*.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/server"
)

// version is set by the Makefile build target via -ldflags at link time.
// It remains "dev" for local builds that skip the linker flag.
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.Args[1:], os.Stderr)
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(2)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(2)
	}

	logger.Info("crawbl-agent-runtime starting",
		"version", version,
		"workspace_id", cfg.WorkspaceID,
		"user_id", cfg.UserID,
		"grpc_listen", cfg.GRPCListen,
		"orchestrator_endpoint", cfg.OrchestratorGRPCEndpoint,
		"mcp_endpoint", cfg.MCPEndpoint,
		"openai_model", cfg.OpenAI.ModelName,
	)

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("init gRPC server", "error", err)
		os.Exit(1)
	}

	// Serve in a goroutine so the main goroutine can watch for signals.
	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serveErr <- err
		}
		close(serveErr)
	}()

	// Wait for shutdown signal or a fatal serve error.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case s := <-sig:
		logger.Info("shutdown signal received", "signal", s.String())
	case err := <-serveErr:
		if err != nil {
			logger.Error("gRPC server exited with error", "error", err)
			os.Exit(1)
		}
	}

	// Graceful stop, bounded by the configured shutdown timeout.
	_, cancel := context.WithTimeout(context.Background(), cfg.Startup.GracefulShutdownTimeout)
	defer cancel()
	srv.Shutdown()
	logger.Info("crawbl-agent-runtime stopped")
}

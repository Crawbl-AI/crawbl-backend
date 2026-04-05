// Command crawbl-agent-runtime is the second binary in the crawbl-backend
// module. It runs as the per-workspace agent runtime pod — one instance
// per user's swarm — and replaces the Rust agent runtime in Phase 2.
//
// Lifecycle:
//
//  1. Parse config from CLI flags + environment variables.
//  2. Construct the LLM adapter (OpenAI via adk-utils-go in Phase 1).
//  3. Construct the orchestrator MCP toolset (HMAC-authed, auto-reconnect).
//  4. Build the agent graph (Manager + Wally + Eve) via runner.New.
//  5. Construct the gRPC server with the HMAC interceptor chain and
//     register the Converse + Memory handlers.
//  6. Flip the health server to SERVING.
//  7. Serve until SIGINT/SIGTERM, then graceful-stop.
//
// All business logic lives under internal/agentruntime/*. This file is
// wiring only.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/model"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/server"
	agentmcp "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools/mcp"
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

	// Step 2a: fetch workspace blueprint (Phase 1: hardcoded default,
	// real HTTP fetch lands in Phase 2). Done before any heavy init so
	// a blueprint fetch failure fails fast before we burn time on the
	// LLM adapter + MCP dial.
	bpCtx, bpCancel := context.WithTimeout(context.Background(), cfg.Startup.BlueprintFetchTimeout)
	blueprint, err := runner.FetchBlueprint(bpCtx, cfg, logger)
	bpCancel()
	if err != nil {
		logger.Error("fetch workspace blueprint", "error", err)
		os.Exit(1)
	}
	logger.Info("workspace blueprint loaded", "workspace_id", blueprint.WorkspaceID, "agent_count", len(blueprint.Agents))
	for _, a := range blueprint.Agents {
		logger.Info("blueprint agent", "slug", a.Slug, "role", a.Role, "allowed_tools", a.AllowedTools)
	}

	// Step 2b: LLM adapter.
	llm, err := model.NewFromConfig(cfg)
	if err != nil {
		logger.Error("init model adapter", "error", err)
		os.Exit(1)
	}

	// Step 3: orchestrator MCP toolset. The returned Closer is a no-op
	// in Phase 1 but still captured so US-AR-006's Closer contract is
	// honored at the lifecycle boundary.
	mcpToolset, mcpCloser, err := agentmcp.Toolset(cfg)
	if err != nil {
		logger.Error("init mcp toolset", "error", err)
		os.Exit(1)
	}
	defer func() {
		if cerr := mcpCloser.Close(); cerr != nil {
			logger.Warn("mcp toolset close error", "error", cerr)
		}
	}()

	// Step 4: agent graph + ADK runner.
	r, err := runner.New(runner.BuildOptions{
		Model:      llm,
		MCPToolset: mcpToolset,
		Logger:     logger,
	})
	if err != nil {
		logger.Error("init runner", "error", err)
		os.Exit(1)
	}
	logger.Info("agent graph constructed", "root_agent", r.RootAgentName())

	// Step 5: gRPC server.
	srv, err := server.New(cfg, logger, r)
	if err != nil {
		logger.Error("init gRPC server", "error", err)
		os.Exit(1)
	}

	// Step 6: flip health to SERVING so Kubernetes probes mark the pod
	// Ready as soon as the listener is up. The runner is already
	// constructed above, so from this point forward Converse calls
	// will succeed (subject to auth).
	srv.Health().SetServing()

	// Step 7: serve + signal loop.
	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serveErr <- err
		}
		close(serveErr)
	}()

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

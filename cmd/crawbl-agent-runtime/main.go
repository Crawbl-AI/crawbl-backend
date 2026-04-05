// Command crawbl-agent-runtime is the second binary in the crawbl-backend
// module. It runs as the per-workspace agent runtime pod — one instance
// per user's swarm — and hosts the full Crawbl agent swarm behind a gRPC
// interface that the orchestrator talks to.
//
// Lifecycle:
//
//  1. Parse config from CLI flags + environment variables.
//  2. Open Postgres + Redis connections (shared with the orchestrator).
//  3. Fetch the workspace blueprint from the orchestrator (HMAC-authed).
//  4. Construct the LLM adapter, the orchestrator MCP toolset, and the
//     Redis-backed session service.
//  5. Build the agent graph (Manager + Wally + Eve) via runner.New.
//  6. Construct the gRPC server with the HMAC interceptor chain and
//     register the Converse + Memory handlers.
//  7. Flip the health server to SERVING.
//  8. Serve until SIGINT/SIGTERM, then graceful-stop.
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

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/agents"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/model"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/server"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/session"
	agentmcp "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools/mcp"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
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
		"postgres_host", cfg.Postgres.Host,
		"redis_addr", cfg.Redis.Addr,
	)

	// Step 2a: Postgres.
	dbConn, err := database.New(cfg.Postgres)
	if err != nil {
		logger.Error("init postgres", "error", err)
		os.Exit(1)
	}
	memStore := memory.NewPostgresStore(dbConn, nil)
	defer func() {
		if cerr := memStore.Close(); cerr != nil {
			logger.Warn("memory store close error", "error", cerr)
		}
	}()

	// Step 2b: Redis (shared client + ADK session service).
	redisCli, err := redisclient.New(cfg.Redis)
	if err != nil {
		logger.Error("init redis", "error", err)
		os.Exit(1)
	}
	rawRedis := redisclient.Unwrap(redisCli)
	if rawRedis == nil {
		logger.Error("init redis: unwrap returned nil client")
		os.Exit(1)
	}
	sessionSvc := session.NewRedisService(rawRedis, cfg.RedisSessionTTL)

	// Step 3: fetch workspace blueprint from the orchestrator. This is
	// done before any heavy init so a blueprint fetch failure fails
	// fast before we burn time on the LLM adapter or MCP dial. A
	// failure here is fatal — see runner/blueprint.go for the
	// rationale.
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

	// Step 4a: LLM adapter.
	llm, err := model.NewFromConfig(cfg)
	if err != nil {
		logger.Error("init model adapter", "error", err)
		os.Exit(1)
	}

	// Step 4b: orchestrator MCP toolset.
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

	// Step 4c: shared local tool slice (web_fetch, web_search_tool,
	// memory_store, memory_recall, memory_forget) built once per pod
	// and bound onto every agent in the graph.
	localTools, err := agents.BuildCommonTools(agents.CommonToolDeps{
		MemStore:        memStore,
		WorkspaceID:     cfg.WorkspaceID,
		SearXNGEndpoint: cfg.SearXNGEndpoint,
	})
	if err != nil {
		logger.Error("init local tools", "error", err)
		os.Exit(1)
	}

	// Step 5: agent graph + ADK runner. The runner owns the session
	// service from this point forward; server.Shutdown calls
	// runner.Close to tear it down on SIGTERM.
	r, err := runner.New(runner.BuildOptions{
		Model:          llm,
		MCPToolset:     mcpToolset,
		SessionService: sessionSvc,
		Blueprint:      blueprint,
		LocalTools:     localTools,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("init runner", "error", err)
		os.Exit(1)
	}
	logger.Info("agent graph constructed", "root_agent", r.RootAgentName())

	// Step 6: gRPC server.
	srv, err := server.New(cfg, server.Deps{
		Runner:   r,
		MemStore: memStore,
		Logger:   logger,
	})
	if err != nil {
		logger.Error("init gRPC server", "error", err)
		os.Exit(1)
	}

	// Step 7: flip health to SERVING so Kubernetes probes mark the pod
	// Ready as soon as the listener is up. The runner is already
	// constructed above, so from this point forward Converse calls
	// will succeed (subject to auth).
	srv.Health().SetServing()

	// Step 8: serve + signal loop.
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
	if cerr := redisCli.Close(); cerr != nil {
		logger.Warn("redis client close error", "error", cerr)
	}
	logger.Info("crawbl-agent-runtime stopped")
}

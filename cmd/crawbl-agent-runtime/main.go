// Command crawbl-agent-runtime is the second binary in the crawbl-backend
// module. It runs as the per-workspace agent runtime pod — one instance
// per user's swarm — and hosts the full Crawbl agent swarm behind a gRPC
// interface that the orchestrator talks to.
//
// Lifecycle:
//
//  1. Parse config from CLI flags + environment variables.
//  2. Open the Redis connection used by the ADK session service.
//  3. Fetch the workspace blueprint from the orchestrator (HMAC-authed).
//  4. Construct the LLM adapter, the orchestrator MCP toolset, and the
//     Redis-backed session service.
//  5. Build the agent graph (Manager + Wally + Eve) via runner.New.
//  6. Construct the gRPC server with the HMAC interceptor chain and
//     register the Converse handler.
//  7. Flip the health server to SERVING.
//  8. Serve until SIGINT/SIGTERM, then graceful-stop.
//
// All business logic lives under internal/agentruntime/*. This file is
// wiring only.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/model"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/server"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/session"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/storage"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	agentmcp "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools/mcp"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/telemetry"
)

// version is set via ko ldflags (.ko.yaml) or crawbl ci build at link time.
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

	if err := run(cfg, logger); err != nil {
		logger.Error("agent runtime exited with error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	logger.Info("crawbl-agent-runtime starting",
		"version", version,
		"workspace_id", cfg.WorkspaceID,
		"user_id", cfg.UserID,
		"grpc_listen", cfg.GRPCListen,
		"orchestrator_endpoint", cfg.OrchestratorGRPCEndpoint,
		"mcp_endpoint", cfg.MCPEndpoint,
		"openai_model", cfg.OpenAI.ModelName,
		"redis_addr", cfg.Redis.Addr,
	)

	defer initTelemetry(cfg, logger, version)()

	redisCli, sessionSvc, err := initRedisAndSession(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := redisCli.Close(); cerr != nil {
			logger.Warn("redis client close error", "error", cerr)
		}
	}()

	r, srv, mcpCloser, err := initRuntimeAndServer(cfg, logger, sessionSvc)
	if err != nil {
		return err
	}
	_ = r
	defer func() {
		if cerr := mcpCloser.Close(); cerr != nil {
			logger.Warn("mcp toolset close error", "error", cerr)
		}
	}()

	// Step 7: flip health to SERVING so Kubernetes probes mark the pod
	// Ready as soon as the listener is up. The runner is already
	// constructed above, so from this point forward Converse calls
	// will succeed (subject to auth).
	srv.Health().SetServing()

	return serveUntilSignal(cfg, logger, srv)
}

// initTelemetry wires OpenTelemetry metrics export and returns a shutdown
// function to be deferred by the caller.
func initTelemetry(_ config.Config, logger *slog.Logger, ver string) func() {
	telCtx, telCancel := context.WithTimeout(context.Background(), 10*time.Second)
	telemetryShutdown, tErr := telemetry.Init(telCtx, telemetry.ConfigFromEnv("crawbl-agent-runtime", ver), logger)
	telCancel()
	if tErr != nil {
		logger.Warn("telemetry init failed, continuing without metrics export", "error", tErr)
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetryShutdown(shutdownCtx); err != nil {
			logger.Warn("telemetry shutdown returned error", "error", err)
		}
	}
}

// initRedisAndSession creates the Redis client and ADK session service.
func initRedisAndSession(cfg config.Config) (redisclient.Client, *session.RedisService, error) {
	redisCli, err := redisclient.New(cfg.Redis)
	if err != nil {
		return nil, nil, err
	}
	rawRedis := redisclient.Unwrap(redisCli)
	if rawRedis == nil {
		return nil, nil, fmt.Errorf("init redis: unwrap returned nil client")
	}
	sessionSvc := session.NewRedisService(rawRedis, cfg.RedisSessionTTL)
	return redisCli, sessionSvc, nil
}

// initRuntimeAndServer builds the agent runner and gRPC server through all
// initialization steps (blueprint fetch, LLM adapter, MCP toolset, Spaces
// client, local tools, runner, server).
// The returned mcpCloser must be closed by the caller when the server stops.
func initRuntimeAndServer(cfg config.Config, logger *slog.Logger, sessionSvc *session.RedisService) (*runner.Runner, *server.Server, agentmcp.Closer, error) {
	// Step 3: fetch workspace blueprint from the orchestrator.
	bpCtx, bpCancel := context.WithTimeout(context.Background(), cfg.Startup.BlueprintFetchTimeout)
	blueprint, err := runner.FetchBlueprint(bpCtx, cfg, logger)
	bpCancel()
	if err != nil {
		return nil, nil, nil, err
	}
	logger.Info("workspace blueprint loaded", "workspace_id", blueprint.WorkspaceID, "agent_count", len(blueprint.Agents))
	for _, a := range blueprint.Agents {
		logger.Info("blueprint agent", "slug", a.Slug, "role", a.Role, "allowed_tools", a.AllowedTools)
	}

	// Step 4a: LLM adapter.
	llm, err := model.NewFromConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	// Step 4b: orchestrator MCP toolset.
	mcpToolset, mcpCloser, err := agentmcp.Toolset(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	// Step 4c: DigitalOcean Spaces client for file_read / file_write.
	spacesClient, err := storage.NewSpacesClient(storage.Config{
		Endpoint:  cfg.Spaces.Endpoint,
		Region:    cfg.Spaces.Region,
		Bucket:    cfg.Spaces.Bucket,
		AccessKey: cfg.Spaces.AccessKey,
		SecretKey: cfg.Spaces.SecretKey,
	})
	if err != nil {
		return nil, nil, mcpCloser, err
	}
	if spacesClient != nil {
		logger.Info("spaces client ready",
			"endpoint", cfg.Spaces.Endpoint,
			"region", cfg.Spaces.Region,
			"bucket", spacesClient.Bucket(),
		)
	} else {
		logger.Info("spaces client disabled (no CRAWBL_SPACES_* config); file_read/file_write unavailable")
	}

	// Step 4d: shared local tool slice.
	localTools, err := tools.BuildCommonTools(tools.CommonToolDeps{
		WorkspaceID:     cfg.WorkspaceID,
		SearXNGEndpoint: cfg.SearXNGEndpoint,
		Spaces:          spacesClient,
	})
	if err != nil {
		return nil, nil, mcpCloser, err
	}

	// Step 5: agent graph + ADK runner.
	r, err := runner.New(runner.BuildOptions{
		Model:          llm,
		MCPToolset:     mcpToolset,
		SessionService: sessionSvc,
		Blueprint:      blueprint,
		LocalTools:     localTools,
		Logger:         logger,
	})
	if err != nil {
		return nil, nil, mcpCloser, err
	}
	logger.Info("agent graph constructed", "root_agent", r.RootAgentName())

	// Step 6: gRPC server.
	srv, err := server.New(cfg, server.Deps{
		Runner: r,
		Logger: logger,
	})
	if err != nil {
		return nil, nil, mcpCloser, err
	}

	return r, srv, mcpCloser, nil
}

// serveUntilSignal starts the gRPC server and blocks until SIGINT/SIGTERM
// or a serve error, then performs graceful shutdown.
func serveUntilSignal(cfg config.Config, logger *slog.Logger, srv *server.Server) error {
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
			return err
		}
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), cfg.Startup.GracefulShutdownTimeout)
	defer stopCancel()
	_ = stopCtx
	srv.Shutdown()
	logger.Info("crawbl-agent-runtime stopped")
	return nil
}

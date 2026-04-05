// Package orchestrator provides the orchestrator HTTP server and migration subcommands.
package orchestrator

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/spf13/cobra"
	"github.com/zishang520/socket.io/v2/socket"

	orch "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	crawblmcp "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/mcp"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agenthistoryrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentpromptsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentsettingsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/conversationrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/toolsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/userrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workspacerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/socketio"
	authservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/authservice"
	agentservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/agentservice"
	chatservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/chatservice"
	integrationservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/integrationservice"
	workflowservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workflowservice"
	workspaceservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workspaceservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

const shutdownTimeout = 10 * time.Second

// NewOrchestratorCommand creates the "orchestrator" parent command.
// Running it directly starts the HTTP server; "migrate" is a subcommand.
func NewOrchestratorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Start the orchestrator HTTP API server",
		Long:  "Start the Crawbl orchestrator API server, realtime layer, runtime client, and embedded MCP server.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(cmd.Context())
		},
	}

	cmd.AddCommand(newMigrateCommand())

	return cmd
}

func runServer(ctx context.Context) error {
	logLevel := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	// Auto-migrate: run pending migrations on startup.
	// Migrations are embedded in the container image at /migrations/orchestrator.
	if err := autoMigrate(logger); err != nil {
		logger.Error("auto-migration failed", "error", err)
		return fmt.Errorf("auto-migration failed: %w", err)
	}

	db, userRepo, workspaceRepo, agentRepo, conversationRepo, messageRepo, cleanup := mustBuildRepos(logger)
	defer cleanup()

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}
	httpMiddleware := buildHTTPMiddleware()

	broadcaster, socketIOHandler, ioServer, cleanupRT := buildRealtime(logger, httpMiddleware)
	defer cleanupRT()

	workspaceService := workspaceservice.New(workspaceRepo, runtimeClient, logger)
	authService := authservice.New(userRepo, workspaceService, legalDocumentsFromEnv())
	toolsRepo := toolsrepo.New()
	agentSettingsRepo := agentsettingsrepo.New()
	agentPromptsRepo := agentpromptsrepo.New()
	agentHistoryRepo := agenthistoryrepo.New()
	artifactRepo := artifactrepo.New()

	chatService := chatservice.New(
		db,
		workspaceRepo, agentRepo, conversationRepo, messageRepo,
		toolsRepo, agentSettingsRepo, agentPromptsRepo, agentHistoryRepo,
		runtimeClient, broadcaster,
	)
	agentService := agentservice.New(
		workspaceRepo, agentRepo,
		toolsRepo, agentSettingsRepo, agentPromptsRepo, agentHistoryRepo,
		runtimeClient,
	)
	integrationService := integrationservice.New(logger)

	// Start background cleanup of orphaned pending messages.
	chatService.StartPendingMessageCleanup(ctx)

	// Register Socket.IO message.send handler now that services are available.
	// This breaks the circular dependency: Socket.IO server → broadcaster → chatService → message handler.
	if ioServer != nil {
		socketio.RegisterMessageHandler(ioServer, &socketio.Config{
			Logger:      logger,
			DB:          db,
			ChatService: chatService,
			AuthService: authService,
		})
	}

	mcpHandler := buildMCPHandler(logger, db, userRepo, workspaceRepo, agentRepo, conversationRepo, messageRepo, agentHistoryRepo, artifactRepo, runtimeClient, broadcaster)

	srv := server.NewServer(&server.Config{
		Port: envOrDefault("CRAWBL_SERVER_PORT", server.DefaultServerPort),
	}, &server.NewServerOpts{
		DB:                 db,
		Logger:             logger,
		AuthService:        authService,
		WorkspaceService:   workspaceService,
		ChatService:        chatService,
		AgentService:       agentService,
		HTTPMiddleware:     httpMiddleware,
		Broadcaster:        broadcaster,
		SocketIOHandler:    socketIOHandler,
		RuntimeClient:      runtimeClient,
		MCPHandler:         mcpHandler,
		IntegrationService: integrationService,
	})

	return srv.Run(ctx, shutdownTimeout)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildHTTPMiddleware() *httpserver.MiddlewareConfig {
	return &httpserver.MiddlewareConfig{
		Environment: envOrDefault("CRAWBL_ENVIRONMENT", httpserver.EnvironmentLocal),
		E2EToken:    os.Getenv("CRAWBL_E2E_TOKEN"),
	}
}

func mustBuildRepos(logger *slog.Logger) (
	*dbr.Connection,
	orchestratorrepo.UserRepo,
	orchestratorrepo.WorkspaceRepo,
	orchestratorrepo.AgentRepo,
	orchestratorrepo.ConversationRepo,
	orchestratorrepo.MessageRepo,
	func(),
) {
	logger.Info("configuring storage backend", slog.String("backend", "postgres"))
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	if err := database.EnsureSchema(dbConfig); err != nil {
		log.Fatal(err)
	}
	db, err := database.New(dbConfig)
	if err != nil {
		log.Fatal(err)
	}
	return db, userrepo.New(), workspacerepo.New(), agentrepo.New(), conversationrepo.New(), messagerepo.New(), func() {
		_ = db.Close()
	}
}

func buildRuntimeClient(logger *slog.Logger) (userswarmclient.Client, error) {
	cfg := userswarmclient.Config{
		Driver:          envOrDefault("CRAWBL_RUNTIME_DRIVER", userswarmclient.DriverFake),
		FakeReplyPrefix: envOrDefault("CRAWBL_RUNTIME_FAKE_REPLY_PREFIX", userswarmclient.DefaultFakeReplyPrefix),
		UserSwarm: userswarmclient.UserSwarmConfig{
			RuntimeNamespace:    envOrDefault("CRAWBL_RUNTIME_NAMESPACE", userswarmclient.DefaultRuntimeNamespace),
			Image:               strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_IMAGE")),
			ImagePullSecretName: strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_IMAGE_PULL_SECRET")),
			DefaultProvider:     envOrDefault("CRAWBL_RUNTIME_DEFAULT_PROVIDER", "openai"),
			DefaultModel:        envOrDefault("CRAWBL_RUNTIME_DEFAULT_MODEL", "gpt-5-mini"),
			EnvSecretName:       strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_ENV_SECRET_NAME")),
			MCPSigningKey:       strings.TrimSpace(os.Getenv("CRAWBL_MCP_SIGNING_KEY")),
			PollTimeout:         durationFromEnv("CRAWBL_RUNTIME_POLL_TIMEOUT", userswarmclient.DefaultPollTimeout),
			PollInterval:        durationFromEnv("CRAWBL_RUNTIME_POLL_INTERVAL", userswarmclient.DefaultPollInterval),
			Port:                int32FromEnv("CRAWBL_RUNTIME_PORT", userswarmclient.DefaultRuntimePort),
		},
	}

	// Accept the legacy driver string "userswarm" as an alias for
	// "crawbl-runtime" during the Phase 2B transition window so dev-cluster
	// ConfigMaps that still carry the old value keep working. The plan is
	// to flip CRAWBL_RUNTIME_DRIVER to "crawbl-runtime" in argocd-apps as
	// part of US-P2-010 and then this branch becomes dead code.
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	switch driver {
	case "", userswarmclient.DriverFake:
		logger.Info("configured fake runtime client")
		return userswarmclient.NewFakeClient(cfg), nil
	case userswarmclient.DriverCrawblRuntime, "userswarm":
		client, err := userswarmclient.NewUserSwarmClient(cfg)
		if err != nil {
			return nil, err
		}
		logger.Info("configured crawbl-runtime client (gRPC)", slog.String("namespace", cfg.UserSwarm.RuntimeNamespace))
		return client, nil
	default:
		return nil, fmt.Errorf("unsupported runtime driver %q", cfg.Driver)
	}
}

func legalDocumentsFromEnv() *orch.LegalDocuments {
	return &orch.LegalDocuments{
		TermsOfService:        envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE", "https://crawbl.com/terms"),
		PrivacyPolicy:         envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY", "https://crawbl.com/privacy"),
		TermsOfServiceVersion: envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE_VERSION", "v1"),
		PrivacyPolicyVersion:  envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY_VERSION", "v1"),
	}
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func int32FromEnv(key string, fallback int32) int32 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			return int32(n)
		}
	}
	return fallback
}

func buildMCPHandler(
	logger *slog.Logger,
	db *dbr.Connection,
	userRepo orchestratorrepo.UserRepo,
	workspaceRepo orchestratorrepo.WorkspaceRepo,
	agentRepo orchestratorrepo.AgentRepo,
	conversationRepo orchestratorrepo.ConversationRepo,
	messageRepo orchestratorrepo.MessageRepo,
	agentHistoryRepo orchestratorrepo.AgentHistoryRepo,
	artifactRepo artifactrepo.Repo,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
) http.Handler {
	signingKey := strings.TrimSpace(os.Getenv("CRAWBL_MCP_SIGNING_KEY"))
	if signingKey == "" {
		logger.Warn("MCP server disabled: CRAWBL_MCP_SIGNING_KEY not set")
		return nil
	}

	var fcm *firebase.FCMClient
	fcmProject := strings.TrimSpace(os.Getenv("CRAWBL_FCM_PROJECT_ID"))
	fcmSAPath := strings.TrimSpace(os.Getenv("CRAWBL_FCM_SERVICE_ACCOUNT_PATH"))
	if fcmProject != "" && fcmSAPath != "" {
		var err error
		fcm, err = firebase.NewFCMClient(fcmProject, fcmSAPath)
		if err != nil {
			logger.Error("failed to create FCM client, push notifications disabled", "error", err)
		} else {
			logger.Info("FCM push notifications enabled", "project", fcmProject)
		}
	}

	workflowRepo := workflowrepo.New()
	workflowSvc := workflowservice.New(db, workflowRepo, runtimeClient, broadcaster)

	handler := crawblmcp.NewHandler(&crawblmcp.Deps{
		DB:               db,
		Logger:           logger,
		UserRepo:         userRepo,
		WorkspaceRepo:    workspaceRepo,
		AgentRepo:        agentRepo,
		ConversationRepo: conversationRepo,
		MessageRepo:      messageRepo,
		AgentHistoryRepo: agentHistoryRepo,
		ArtifactRepo:     artifactRepo,
		SigningKey:       signingKey,
		FCM:              fcm,
		RuntimeClient:    runtimeClient,
		Broadcaster:      broadcaster,
		WorkflowRepo:     workflowRepo,
		WorkflowService:  workflowSvc,
	})
	logger.Info("MCP server enabled at /mcp/v1")
	return handler
}

func buildRealtime(logger *slog.Logger, middleware *httpserver.MiddlewareConfig) (realtime.Broadcaster, http.Handler, *socket.Server, func()) {
	addr := strings.TrimSpace(os.Getenv("CRAWBL_REDIS_ADDR"))
	if addr == "" {
		logger.Info("realtime disabled: CRAWBL_REDIS_ADDR not set")
		return realtime.NopBroadcaster{}, nil, nil, func() {}
	}

	redisCfg := redisclient.ConfigFromEnv("CRAWBL_")
	rc, err := redisclient.New(redisCfg)
	if err != nil {
		logger.Error("failed to connect to Redis, falling back to no realtime", "error", err)
		return realtime.NopBroadcaster{}, nil, nil, func() {}
	}
	logger.Info("redis connected", slog.String("addr", redisCfg.Addr))

	io := socketio.NewServer(&socketio.Config{
		Logger:      logger,
		RedisClient: redisclient.Unwrap(rc),
	})

	broadcaster := socketio.NewBroadcaster(io, logger)
	handler := socketio.Handler(io)

	cleanup := func() {
		io.Close(nil)
		_ = rc.Close()
	}

	logger.Info("realtime enabled: socket.io + redis")
	return broadcaster, handler, io, cleanup
}

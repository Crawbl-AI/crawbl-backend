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

	"riverqueue.com/riverui"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/background"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/graph"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/layers"
	orch "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agenthistoryrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentpromptsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentsettingsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/conversationrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/integrationconnrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/llmusagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/mcprepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/toolsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/userrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workspacerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server"
	crawblmcp "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/mcp"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/socketio"
	agentservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/agentservice"
	auditservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/auditservice"
	authservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/authservice"
	chatservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/chatservice"
	integrationservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/integrationservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"

	workflowservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workflowservice"
	workspaceservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workspaceservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/clickhouse"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/crawblnats"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	pkgriver "github.com/Crawbl-AI/crawbl-backend/internal/pkg/river"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/telemetry"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
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
	slog.SetDefault(logger)

	// Telemetry: wire the OpenTelemetry meter provider up to
	// VictoriaMetrics via the cluster's /opentelemetry/v1/metrics
	// endpoint. Disabled automatically when CRAWBL_OTEL_METRICS_ENDPOINT
	// is empty (local dev) so the same code path runs everywhere. Logs
	// stay on stdout and are scraped into VictoriaLogs by Fluent Bit —
	// we never double-ship them.
	telemetryShutdown, tErr := telemetry.Init(ctx, telemetry.ConfigFromEnv("orchestrator", os.Getenv("CRAWBL_VERSION")), logger)
	if tErr != nil {
		logger.Warn("telemetry init failed, continuing without metrics export", "error", tErr)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetryShutdown(shutdownCtx); err != nil {
			logger.Warn("telemetry shutdown returned error", "error", err)
		}
	}()

	// Auto-migrate: run pending migrations on startup.
	// Migrations are embedded in the container image at /migrations/orchestrator.
	if err := autoMigrate(logger); err != nil {
		logger.Error("auto-migration failed", "error", err)
		return fmt.Errorf("auto-migration failed: %w", err)
	}

	db, userRepo, workspaceRepo, agentRepo, conversationRepo, messageRepo, cleanup := mustBuildRepos(logger)
	defer cleanup()

	// Seed global catalogs (tools, models, tool categories, integration categories,
	// integration providers) from embedded data on every startup (idempotent).
	if err := seedCatalogs(ctx, db, logger); err != nil {
		return err
	}

	// Memory system — constructed unconditionally; embedder is optional.
	// When CRAWBL_EMBED_BASE_URL is empty the embedder and memoryStack remain
	// nil, and downstream services fall back to messages-only context.
	drawerRepo := drawer.NewPostgres()
	kgGraph := kg.NewPostgres()
	palaceGraph := graph.NewPostgres()
	classifier := extract.NewClassifier()

	var memoryStack layers.Stack
	var embedder embed.Embedder
	if baseURL := os.Getenv("CRAWBL_EMBED_BASE_URL"); baseURL != "" {
		embedder = embed.NewProvider(embed.ProviderConfig{
			BaseURL: baseURL,
			APIKey:  os.Getenv("CRAWBL_EMBED_API_KEY"),
			Model:   os.Getenv("CRAWBL_EMBED_MODEL"),
		})
		memoryStack = layers.NewStack(drawerRepo, embedder, kgGraph)
		logger.Info("memory stack enabled", slog.String("base_url", baseURL))
	} else {
		logger.Warn("memory stack disabled: CRAWBL_EMBED_BASE_URL not set — WakeUp context injection and semantic search will be unavailable")
	}

	// River schema migration — runs after app migrations, before HTTP server.
	// Fatal on error: River is load-bearing; a failed migration must block boot.
	if err := pkgriver.Migrate(ctx, db.DB); err != nil {
		logger.Error("river migration failed", "error", err)
		return fmt.Errorf("river migration failed: %w", err)
	}
	logger.Info("river migrations applied")

	clickhouseDB, err := clickhouse.Open(ctx, logger)
	if err != nil {
		return fmt.Errorf("clickhouse open: %w", err)
	}
	defer func() {
		if clickhouseDB != nil {
			_ = clickhouseDB.Close()
		}
	}()
	llmUsageRepo := llmusagerepo.New(clickhouseDB)

	// Build the memory-domain River config.
	riverCfg, err := background.NewConfig(background.Deps{
		DB:            db,
		DrawerRepo:    drawerRepo,
		KGGraph:       kgGraph,
		LLMClassifier: newLLMClassifierOrNil(),
		Embedder:      embedder,
		Logger:        logger,
	})
	if err != nil {
		logger.Error("river config failed", "error", err)
		return fmt.Errorf("river config: %w", err)
	}

	// Register the usage-write worker onto the same River client. This
	// replaces the standalone cmd/usage-writer binary that previously
	// consumed from NATS and wrote to ClickHouse.
	queue.RegisterUsageWriter(riverCfg.Workers, riverCfg.Queues, queue.UsageWriterDeps{
		Repo:   llmUsageRepo,
		Logger: logger,
	})

	// Construct the River client over the shared *sql.DB pool.
	riverClient, err := pkgriver.New(db.DB, riverCfg)
	if err != nil {
		logger.Error("river client construction failed", "error", err)
		return fmt.Errorf("river client: %w", err)
	}

	// Start the client — beyond this point, a defer will handle graceful shutdown.
	if err := riverClient.Start(ctx); err != nil {
		logger.Error("river client start failed", "error", err)
		return fmt.Errorf("river start: %w", err)
	}
	defer pkgriver.Shutdown(riverClient, logger)
	logger.Info("river client started", "queues", "memory_process,memory_maintain,usage_write")

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}
	httpMiddleware := buildHTTPMiddleware()

	broadcaster, socketIOHandler, ioServer, cleanupRT := buildRealtime(logger)
	defer cleanupRT()

	workspaceService := workspaceservice.New(workspaceRepo, runtimeClient, logger)
	authService := authservice.New(userRepo, workspaceService, legalDocumentsFromEnv())
	toolsRepo := toolsrepo.New()
	agentSettingsRepo := agentsettingsrepo.New()
	agentPromptsRepo := agentpromptsrepo.New()
	agentHistoryRepo := agenthistoryrepo.New()
	artifactRepo := artifactrepo.New()

	pricingCache := pricing.New(db, logger)
	pricingCache.Start(ctx, 5*time.Minute)

	// Connect to NATS for usage event publishing. Disabled when CRAWBL_NATS_URL is unset.
	natsCfg := crawblnats.DefaultConfig()
	natsCfg.URL = strings.TrimSpace(os.Getenv("CRAWBL_NATS_URL"))
	natsClient, natsErr := crawblnats.Connect(ctx, natsCfg, logger)
	if natsErr != nil {
		logger.Warn("NATS connect failed, usage publishing disabled", "error", natsErr)
	}
	defer func() {
		if natsClient != nil {
			_ = natsClient.Close()
		}
	}()
	usagePublisher := queue.NewUsagePublisher(riverClient, logger)
	memoryPublisher := queue.NewMemoryPublisher(natsClient, logger)

	chatService := chatservice.New(
		db,
		chatservice.Repos{
			Workspace:     workspaceRepo,
			Agent:         agentRepo,
			Conversation:  conversationRepo,
			Message:       messageRepo,
			Tools:         toolsRepo,
			AgentSettings: agentSettingsRepo,
			AgentPrompts:  agentPromptsRepo,
			AgentHistory:  agentHistoryRepo,
			Usage:         usagerepo.New(),
		},
		runtimeClient, broadcaster, memoryStack, pricingCache, usagePublisher, memoryPublisher,
		chatservice.MemoryDeps{
			DrawerRepo:  drawerRepo,
			Classifier:  classifier,
			Embedder:    embedder,
			RiverClient: riverClient,
		},
	)
	agentService := agentservice.New(
		agentservice.Repos{
			Workspace:     workspaceRepo,
			Agent:         agentRepo,
			Tools:         toolsRepo,
			AgentSettings: agentSettingsRepo,
			AgentPrompts:  agentPromptsRepo,
			AgentHistory:  agentHistoryRepo,
			Usage:         usagerepo.New(),
			Drawer:        drawerRepo,
		},
		runtimeClient,
	)
	integrationConnRepo := integrationconnrepo.New()
	integrationService := integrationservice.New(logger, integrationConnRepo)

	// Start memory auto-ingest worker (hot path).
	chatService.StartIngestWorker(ctx)

	// Start background cleanup of orphaned pending messages.
	// The done channel is closed when the goroutine exits, so we wait
	// for it on shutdown to avoid DB queries against a closed pool.
	cleanupDone := chatService.StartPendingMessageCleanup(ctx)
	defer func() { <-cleanupDone }()

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

	mcpHandler := buildMCPHandler(logger, db, workspaceRepo, agentRepo, conversationRepo, messageRepo, agentHistoryRepo, artifactRepo, runtimeClient, broadcaster, drawerRepo, kgGraph, palaceGraph, classifier, embedder, memoryStack)

	// River UI dashboard — host-gated, auth enforced at the Envoy Gateway layer.
	// Disabled when CRAWBL_RIVERUI_HOST is empty (feature flag off).
	var riverUIHandler http.Handler
	riverUIHost := strings.TrimSpace(os.Getenv("CRAWBL_RIVERUI_HOST"))
	if riverUIHost != "" {
		endpoints := riverui.NewEndpoints(riverClient, nil)
		ruiHandler, ruiErr := riverui.NewHandler(&riverui.HandlerOpts{
			Endpoints: endpoints,
			Logger:    logger,
		})
		if ruiErr != nil {
			logger.Error("riverui handler construction failed", "error", ruiErr)
			return fmt.Errorf("riverui handler: %w", ruiErr)
		}
		if ruiErr = ruiHandler.Start(ctx); ruiErr != nil {
			logger.Error("riverui handler start failed", "error", ruiErr)
			return fmt.Errorf("riverui start: %w", ruiErr)
		}
		riverUIHandler = ruiHandler
		logger.Info("riverui enabled", slog.String("host", riverUIHost))
	}

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
		MCPSigningKey:      strings.TrimSpace(os.Getenv("CRAWBL_MCP_SIGNING_KEY")),
		RiverUIHandler:     riverUIHandler,
		RiverUIHost:        riverUIHost,
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

// newLLMClassifierOrNil constructs an LLM-backed drawer classifier using the
// same env vars as the standalone memory-process job binary. Returns nil when
// the required env vars are absent so the River worker degrades gracefully
// (the periodic sweep will still run but skip LLM classification steps).
// Phase 9 will add these vars to the orchestrator Helm values so they are
// always present in the cluster environment.
//
// Required env vars (same as memory-process pod):
//   - CRAWBL_LLM_BASE_URL  (falls back to CRAWBL_EMBED_BASE_URL)
//   - CRAWBL_LLM_API_KEY   (falls back to CRAWBL_EMBED_API_KEY)
//   - CRAWBL_CLASSIFY_MODEL
func newLLMClassifierOrNil() extract.LLMClassifier {
	llmBaseURL := strings.TrimSpace(os.Getenv("CRAWBL_LLM_BASE_URL"))
	if llmBaseURL == "" {
		llmBaseURL = strings.TrimSpace(os.Getenv("CRAWBL_EMBED_BASE_URL"))
	}
	if llmBaseURL == "" {
		return nil
	}
	llmAPIKey := strings.TrimSpace(os.Getenv("CRAWBL_LLM_API_KEY"))
	if llmAPIKey == "" {
		llmAPIKey = strings.TrimSpace(os.Getenv("CRAWBL_EMBED_API_KEY"))
	}
	return extract.NewLLMClassifier(extract.LLMClassifierConfig{
		BaseURL: llmBaseURL,
		APIKey:  llmAPIKey,
		Model:   strings.TrimSpace(os.Getenv("CRAWBL_CLASSIFY_MODEL")),
	})
}

func buildMCPHandler(
	logger *slog.Logger,
	db *dbr.Connection,
	workspaceRepo orchestratorrepo.WorkspaceRepo,
	agentRepo orchestratorrepo.AgentRepo,
	conversationRepo orchestratorrepo.ConversationRepo,
	messageRepo orchestratorrepo.MessageRepo,
	agentHistoryRepo orchestratorrepo.AgentHistoryRepo,
	artifactRepo artifactrepo.Repo,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
	drawerRepo drawer.Repo,
	kgGraph kg.Graph,
	palaceGraph graph.PalaceGraph,
	classifier extract.Classifier,
	embedder embed.Embedder,
	memoryStack layers.Stack,
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

	wfRepo := workflowrepo.New()
	workflowSvc := workflowservice.New(db, wfRepo, runtimeClient, broadcaster)

	auditRepo := auditrepo.New()
	auditSvc := auditservice.New(auditRepo)

	mcpRepo := mcprepo.New()
	mcpSvc := mcpservice.New(
		mcpservice.Repos{
			MCP:          mcpRepo,
			Workspace:    workspaceRepo,
			Conversation: conversationRepo,
			Agent:        agentRepo,
			AgentHistory: agentHistoryRepo,
			Message:      messageRepo,
			Artifact:     artifactRepo,
			Workflow:     wfRepo,
		},
		mcpservice.Infra{
			Logger:        logger,
			FCM:           fcm,
			RuntimeClient: runtimeClient,
			Broadcaster:   broadcaster,
			WorkflowExec:  workflowSvc,
		},
		memoryStack,
	)

	handler := crawblmcp.NewHandler(&crawblmcp.Deps{
		DB:           db,
		Logger:       logger,
		SigningKey:   signingKey,
		MCPService:   mcpSvc,
		AuditService: auditSvc,
		DrawerRepo:   drawerRepo,
		KG:           kgGraph,
		MemoryStack:  memoryStack,
		PalaceGraph:  palaceGraph,
		Classifier:   classifier,
		Embedder:     embedder,
	})
	logger.Info("MCP server enabled at /mcp/v1")
	return handler
}

func buildRealtime(logger *slog.Logger) (realtime.Broadcaster, http.Handler, *socket.Server, func()) {
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

// seedCatalogs upserts all reference catalogs into the database on startup.
// Covers tools, models, tool categories, integration categories, and
// integration providers. Idempotent — safe to run on every boot.
func seedCatalogs(ctx context.Context, db *dbr.Connection, logger *slog.Logger) error {
	sess := db.NewSession(nil)

	// 1. Tools — uses the existing repo Seed method (dbr builder pattern).
	catalog := agentruntimetools.DefaultCatalog()
	toolRows := make([]orchestratorrepo.ToolRow, len(catalog))
	for i, t := range catalog {
		toolRows[i] = orchestratorrepo.ToolRow{
			Name:        t.Name,
			DisplayName: t.DisplayName,
			Description: t.Description,
			Category:    string(t.Category),
			IconURL:     t.IconURL,
			SortOrder:   i,
			CreatedAt:   time.Now(),
		}
	}
	repo := toolsrepo.New()
	if mErr := repo.Seed(ctx, sess, toolRows); mErr != nil {
		logger.Error("tool catalog seed failed", "error", mErr.Error())
		return fmt.Errorf("tool catalog seed: %s", mErr.Error())
	}

	// 2. Models
	for i, m := range seed.AvailableModels() {
		var existing orchestratorrepo.ModelRow
		err := sess.Select("id").From("models").Where("id = ?", m.ID).LoadOneContext(ctx, &existing)
		if err != nil && !database.IsRecordNotFoundError(err) {
			return fmt.Errorf("seed model %q: %w", m.ID, err)
		}
		if existing.ID != "" {
			_, err = sess.Update("models").
				Set("name", m.Name).
				Set("description", m.Description).
				Set("sort_order", i).
				Where("id = ?", m.ID).
				ExecContext(ctx)
		} else {
			_, err = sess.InsertInto("models").
				Pair("id", m.ID).
				Pair("name", m.Name).
				Pair("description", m.Description).
				Pair("sort_order", i).
				Pair("created_at", time.Now()).
				ExecContext(ctx)
		}
		if err != nil {
			return fmt.Errorf("seed model %q: %w", m.ID, err)
		}
	}

	// 3. Tool categories
	for i, c := range agentruntimetools.ToolCategories() {
		catID := string(c.ID)
		var existing orchestratorrepo.ToolCategoryRow
		err := sess.Select("id").From("tool_categories").Where("id = ?", catID).LoadOneContext(ctx, &existing)
		if err != nil && !database.IsRecordNotFoundError(err) {
			return fmt.Errorf("seed tool category %q: %w", catID, err)
		}
		if existing.ID != "" {
			_, err = sess.Update("tool_categories").
				Set("name", c.Name).
				Set("image_url", c.ImageURL).
				Set("sort_order", i).
				Where("id = ?", catID).
				ExecContext(ctx)
		} else {
			_, err = sess.InsertInto("tool_categories").
				Pair("id", catID).
				Pair("name", c.Name).
				Pair("image_url", c.ImageURL).
				Pair("sort_order", i).
				Pair("created_at", time.Now()).
				ExecContext(ctx)
		}
		if err != nil {
			return fmt.Errorf("seed tool category %q: %w", catID, err)
		}
	}

	// 4. Integration categories
	for i, c := range seed.IntegrationCategories() {
		var existing orchestratorrepo.IntegrationCategoryRow
		err := sess.Select("id").From("integration_categories").Where("id = ?", c.ID).LoadOneContext(ctx, &existing)
		if err != nil && !database.IsRecordNotFoundError(err) {
			return fmt.Errorf("seed integration category %q: %w", c.ID, err)
		}
		if existing.ID != "" {
			_, err = sess.Update("integration_categories").
				Set("name", c.Name).
				Set("image_url", c.ImageURL).
				Set("sort_order", i).
				Where("id = ?", c.ID).
				ExecContext(ctx)
		} else {
			_, err = sess.InsertInto("integration_categories").
				Pair("id", c.ID).
				Pair("name", c.Name).
				Pair("image_url", c.ImageURL).
				Pair("sort_order", i).
				Pair("created_at", time.Now()).
				ExecContext(ctx)
		}
		if err != nil {
			return fmt.Errorf("seed integration category %q: %w", c.ID, err)
		}
	}

	// 5. Integration providers
	for i, p := range seed.IntegrationProviders() {
		var existing orchestratorrepo.IntegrationProviderRow
		err := sess.Select("provider").From("integration_providers").Where("provider = ?", p.Provider).LoadOneContext(ctx, &existing)
		if err != nil && !database.IsRecordNotFoundError(err) {
			return fmt.Errorf("seed integration provider %q: %w", p.Provider, err)
		}
		if existing.Provider != "" {
			_, err = sess.Update("integration_providers").
				Set("name", p.Name).
				Set("description", p.Description).
				Set("icon_url", p.IconURL).
				Set("category_id", p.CategoryID).
				Set("is_enabled", p.IsEnabled).
				Set("sort_order", i).
				Where("provider = ?", p.Provider).
				ExecContext(ctx)
		} else {
			_, err = sess.InsertInto("integration_providers").
				Pair("provider", p.Provider).
				Pair("name", p.Name).
				Pair("description", p.Description).
				Pair("icon_url", p.IconURL).
				Pair("category_id", p.CategoryID).
				Pair("is_enabled", p.IsEnabled).
				Pair("sort_order", i).
				Pair("created_at", time.Now()).
				ExecContext(ctx)
		}
		if err != nil {
			return fmt.Errorf("seed integration provider %q: %w", p.Provider, err)
		}
	}

	// 6. Usage plans
	for _, p := range seed.UsagePlans() {
		var existing struct {
			PlanID string `db:"plan_id"`
		}
		err := sess.Select("plan_id").From("usage_plans").
			Where("plan_id = ?", p.PlanID).
			LoadOneContext(ctx, &existing)
		if err != nil && !database.IsRecordNotFoundError(err) {
			return fmt.Errorf("seed usage plan %q: %w", p.PlanID, err)
		}
		if existing.PlanID != "" {
			_, err = sess.Update("usage_plans").
				Set("name", p.Name).
				Set("monthly_token_limit", p.MonthlyTokenLimit).
				Set("daily_request_limit", p.DailyRequestLimit).
				Set("max_tokens_per_request", p.MaxTokensPerRequest).
				Set("updated_at", time.Now()).
				Where("plan_id = ?", p.PlanID).
				ExecContext(ctx)
		} else {
			_, err = sess.InsertInto("usage_plans").
				Pair("plan_id", p.PlanID).
				Pair("name", p.Name).
				Pair("monthly_token_limit", p.MonthlyTokenLimit).
				Pair("daily_request_limit", p.DailyRequestLimit).
				Pair("max_tokens_per_request", p.MaxTokensPerRequest).
				Pair("created_at", time.Now()).
				Pair("updated_at", time.Now()).
				ExecContext(ctx)
		}
		if err != nil {
			return fmt.Errorf("seed usage plan %q: %w", p.PlanID, err)
		}
	}

	// 7. Model pricing (bootstrap — CronJob is the real source of truth)
	for _, p := range seed.ModelPricing() {
		var existing struct {
			Model string `db:"model"`
		}
		err := sess.Select("model").From("model_pricing").
			Where("provider = ? AND model = ? AND region = ?", p.Provider, p.Model, p.Region).
			OrderBy("effective_at DESC").
			Limit(1).
			LoadOneContext(ctx, &existing)
		if err != nil && !database.IsRecordNotFoundError(err) {
			return fmt.Errorf("seed model pricing %q: %w", p.Model, err)
		}
		if existing.Model != "" {
			continue // Already has pricing — don't overwrite CronJob data
		}
		_, err = sess.InsertInto("model_pricing").
			Pair("provider", p.Provider).
			Pair("model", p.Model).
			Pair("region", p.Region).
			Pair("input_cost_per_token", p.InputCostPerToken).
			Pair("output_cost_per_token", p.OutputCostPerToken).
			Pair("cached_cost_per_token", p.CachedCostPerToken).
			Pair("source", p.Source).
			Pair("effective_at", time.Now()).
			Pair("created_at", time.Now()).
			ExecContext(ctx)
		if err != nil {
			return fmt.Errorf("seed model pricing %q: %w", p.Model, err)
		}
	}

	logger.Info("catalogs seeded",
		slog.Int("tools", len(catalog)),
		slog.Int("models", len(seed.AvailableModels())),
		slog.Int("tool_categories", len(agentruntimetools.ToolCategories())),
		slog.Int("integration_categories", len(seed.IntegrationCategories())),
		slog.Int("integration_providers", len(seed.IntegrationProviders())),
		slog.Int("usage_plans", len(seed.UsagePlans())),
		slog.Int("model_pricing", len(seed.ModelPricing())),
	)
	return nil
}

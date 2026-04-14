// Package orchestrator provides the orchestrator HTTP server and migration subcommands.
package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
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

	orch "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/centroidrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/identityrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/kgrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/palacegraphrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
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
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/toolsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagequotarepo"
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

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/middleware"
	workflowservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workflowservice"
	workspaceservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workspaceservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/clickhouse"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/crawblnats"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	pkgriver "github.com/Crawbl-AI/crawbl-backend/internal/pkg/river"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/telemetry"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

const shutdownTimeout = 10 * time.Second

// noopCleanup is a cleanup closure that does nothing. Used when a subsystem
// is disabled and no resources were acquired that need releasing.
var noopCleanup = func() {}

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
	cmd.AddCommand(newAPICommand())
	cmd.AddCommand(newMCPCommand())
	cmd.AddCommand(newWorkerCommand())

	return cmd
}

// logLevelFromEnv parses the LOG_LEVEL env var into an slog.Level. The
// returned level defaults to Info for unrecognised or empty values.
func logLevelFromEnv() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func runServer(ctx context.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevelFromEnv()}))
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

	db, repos, cleanup := mustBuildRepos(logger)
	defer cleanup()
	userRepo := repos.User
	workspaceRepo := repos.Workspace
	agentRepo := repos.Agent
	conversationRepo := repos.Conversation
	messageRepo := repos.Message

	// Shared Redis client — reused by realtime (socket.io adapter) and by
	// the palace-graph cache. Nil is safe everywhere: palacegraphrepo
	// degrades to pass-through, buildRealtime falls back to NopBroadcaster.
	redisClient, cleanupRedis := buildSharedRedis(logger)
	defer cleanupRedis()

	// Memory system — constructed unconditionally; embedder is optional.
	// When CRAWBL_EMBED_BASE_URL is empty the embedder and memoryStack remain
	// nil, and downstream services fall back to messages-only context.
	// The concrete *Postgres structs are narrowed to the wiring-layer
	// interfaces declared in ports.go; downstream services narrow further
	// via their own consumer-side ports.
	var drawerRepo mcpDrawerRepoRaw = drawerrepo.NewPostgres()
	var kgRepo mcpKGRepoRaw = kgrepo.NewPostgres()
	var palaceGraphRepo mcpPalaceGraphRepoRaw = palacegraphrepo.NewPostgres(redisClient, logger)
	var identityRepo mcpIdentityRepoRaw = identityrepo.NewPostgres()
	var centroidRepo mcpCentroidRepoRaw = centroidrepo.NewPostgres()
	classifier := extract.NewClassifier()

	memoryStack, embedder := buildMemoryStack(logger, drawerRepo, identityRepo)

	// River schema migration — runs after app migrations, before HTTP server.
	// Fatal on error: River is load-bearing; a failed migration must block boot.
	if err := pkgriver.Migrate(ctx, db.DB); err != nil {
		logger.Error("river migration failed", "error", err)
		return fmt.Errorf("river migration failed: %w", err)
	}
	logger.Info("river migrations applied")

	clickhouseDB, err := openClickhouseForServer(ctx, logger)
	if err != nil {
		return err
	}
	defer func() {
		if clickhouseDB != nil {
			_ = clickhouseDB.Close()
		}
	}()
	llmUsageRepo := llmusagerepo.New(clickhouseDB)

	natsClient := connectNATSForMemory(ctx, logger)
	defer func() {
		if natsClient != nil {
			_ = natsClient.Close()
		}
	}()
	memoryPublisher := queue.NewMemoryPublisher(natsClient, logger)

	pricingCache := pricing.New(db, modelpricingrepo.New(), logger)
	pricingCache.Start(ctx)

	// Build the single river.Config covering every background job,
	// periodic sweep, and cron the orchestrator owns. Auto-ingest is
	// NOT on this list — it runs in-process under
	// internal/orchestrator/memory/autoingest so the chat-turn hot
	// path never writes to river_job.
	riverClient, err := buildAndStartRiverClient(ctx, logger, queue.Deps{
		DB:               db,
		Logger:           logger,
		DrawerRepo:       drawerRepo,
		KGRepo:           kgRepo,
		CentroidRepo:     centroidRepo,
		LLMClassifier:    newLLMClassifierOrNil(),
		Embedder:         embedder,
		MessageRepo:      messageRepo,
		ModelPricingRepo: modelpricingrepo.New(),
		PricingCache:     pricingCache,
		LLMUsageRepo:     llmUsageRepo,
	})
	if err != nil {
		return err
	}
	defer pkgriver.Shutdown(riverClient, logger)

	ingestPool, err := buildAutoIngestPool(logger, autoingest.Deps{
		DB:              db,
		DrawerRepo:      drawerRepo,
		CentroidRepo:    centroidRepo,
		Classifier:      classifier,
		Embedder:        embedder,
		MemoryPublisher: memoryPublisher,
		Logger:          logger,
	})
	if err != nil {
		return err
	}
	defer shutdownAutoIngestPool(ingestPool, logger)

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}

	workspaceService := workspaceservice.MustNew(workspaceRepo, runtimeClient, logger)
	authService := authservice.MustNew(userRepo, workspaceService, legalDocumentsFromEnv(), usagequotarepo.New())

	httpMiddleware := buildHTTPMiddleware()
	broadcaster, socketIOHandler, ioServer, cleanupRT := buildRealtime(logger, redisClient, db, workspaceRepo, authService)
	defer cleanupRT()
	toolsRepo := toolsrepo.New()
	agentSettingsRepo := agentsettingsrepo.New()
	agentPromptsRepo := agentpromptsrepo.New()
	agentHistoryRepo := agenthistoryrepo.New()
	artifactRepo := artifactrepo.New()

	usagePublisher := queue.NewUsagePublisher(riverClient, logger)

	chatService := chatservice.MustNew(chatservice.NewServiceOpts{
		DB: db,
		Repos: chatservice.Repos{
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
		RuntimeClient:  runtimeClient,
		Broadcaster:    broadcaster,
		MemoryStack:    memoryStack,
		PricingCache:   pricingCache,
		UsagePublisher: usagePublisher,
		IngestPool:     ingestPool,
	})
	agentService := agentservice.MustNew(
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
	integrationService := integrationservice.MustNew(logger, integrationConnRepo)

	// Register Socket.IO message.send handler now that services are available.
	// This breaks the circular dependency: Socket.IO server → broadcaster → chatService → message handler.
	if ioServer != nil {
		socketio.RegisterMessageHandler(ioServer, &socketio.Config{
			Logger:           logger,
			DB:               db,
			ChatService:      chatService,
			AuthService:      authService,
			WorkspaceService: workspaceService,
			ShutdownCtx:      ctx,
		})
	}

	mcpHandler := buildMCPHandler(mcpHandlerDeps{
		Ctx:              ctx,
		Logger:           logger,
		DB:               db,
		WorkspaceRepo:    workspaceRepo,
		AgentRepo:        agentRepo,
		ConversationRepo: conversationRepo,
		MessageRepo:      messageRepo,
		AgentHistoryRepo: agentHistoryRepo,
		ArtifactRepo:     artifactRepo,
		RuntimeClient:    runtimeClient,
		Broadcaster:      broadcaster,
		DrawerRepo:       drawerRepo,
		KGRepo:           kgRepo,
		PalaceGraphRepo:  palaceGraphRepo,
		IdentityRepo:     identityRepo,
		Classifier:       classifier,
		Embedder:         embedder,
		MemoryStack:      memoryStack,
	})

	// River UI dashboard — host-gated, auth enforced at the Envoy Gateway layer.
	// Disabled when CRAWBL_RIVERUI_HOST is empty (feature flag off).
	riverUIHost := strings.TrimSpace(os.Getenv("CRAWBL_RIVERUI_HOST"))
	riverUIHandler, err := buildRiverUIHandler(ctx, logger, riverClient, riverUIHost)
	if err != nil {
		return err
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

// buildAndStartRiverClient builds the River config, constructs the client
// over db.DB, and starts it. Any step that fails returns an error wrapped
// with the failure stage so the caller can surface it uniformly.
func buildAndStartRiverClient(ctx context.Context, logger *slog.Logger, deps queue.Deps) (*pkgriver.Client, error) {
	riverCfg, err := queue.NewConfig(deps)
	if err != nil {
		logger.Error("river config failed", "error", err)
		return nil, fmt.Errorf("river config: %w", err)
	}
	riverClient, err := pkgriver.New(deps.DB.DB, riverCfg)
	if err != nil {
		logger.Error("river client construction failed", "error", err)
		return nil, fmt.Errorf("river client: %w", err)
	}
	if err := riverClient.Start(ctx); err != nil {
		logger.Error("river client start failed", "error", err)
		return nil, fmt.Errorf("river start: %w", err)
	}
	logger.Info("river client started", "queues", "memory_process,memory_maintain,memory_enrich,memory_centroid,usage_write,pricing_refresh,message_cleanup")
	return riverClient, nil
}

// buildAutoIngestPool wires the in-process auto-ingest service sized by
// CRAWBL_AUTOINGEST_WORKERS / _CAPACITY env vars.
func buildAutoIngestPool(_ *slog.Logger, deps autoingest.Deps) (autoingest.Service, error) {
	ingestWorkers, _ := strconv.Atoi(os.Getenv("CRAWBL_AUTOINGEST_WORKERS"))
	ingestCapacity, _ := strconv.Atoi(os.Getenv("CRAWBL_AUTOINGEST_CAPACITY"))
	pool, err := autoingest.NewService(deps, autoingest.Config{
		Workers:   ingestWorkers,
		QueueSize: ingestCapacity,
	})
	if err != nil {
		return nil, fmt.Errorf("memory.autoingest: %w", err)
	}
	return pool, nil
}

// shutdownAutoIngestPool drains the auto-ingest pool with a bounded
// timeout, logging any shutdown-timeout warnings but never returning an
// error since the caller is already on its way out.
func shutdownAutoIngestPool(pool autoingest.Service, logger *slog.Logger) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.Shutdown(shutdownCtx); err != nil {
		logger.Warn("memory.autoingest: shutdown timeout", "error", err)
	}
}

// buildRiverUIHandler constructs the River UI handler when the feature
// flag (CRAWBL_RIVERUI_HOST) is set. Returns (nil, nil) when disabled so
// the caller can pass a nil handler straight through to server.Config.
func buildRiverUIHandler(ctx context.Context, logger *slog.Logger, riverClient *pkgriver.Client, riverUIHost string) (http.Handler, error) {
	if riverUIHost == "" {
		return nil, nil
	}
	endpoints := riverui.NewEndpoints(riverClient, nil)
	ruiHandler, err := riverui.NewHandler(&riverui.HandlerOpts{
		Endpoints: endpoints,
		Logger:    logger,
	})
	if err != nil {
		logger.Error("riverui handler construction failed", "error", err)
		return nil, fmt.Errorf("riverui handler: %w", err)
	}
	if err := ruiHandler.Start(ctx); err != nil {
		logger.Error("riverui handler start failed", "error", err)
		return nil, fmt.Errorf("riverui start: %w", err)
	}
	logger.Info("riverui enabled", slog.String("host", riverUIHost))
	return ruiHandler, nil
}

// openClickhouseForServer opens the ClickHouse connection for the
// orchestrator server. A failure to open is fatal; returning nil is
// permitted when ClickHouse is not configured.
func openClickhouseForServer(ctx context.Context, logger *slog.Logger) (*sql.DB, error) {
	clickhouseDB, err := clickhouse.Open(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	return clickhouseDB, nil
}

// connectNATSForMemory connects to NATS for memory fan-out events. On
// failure the returned client is nil — downstream publishers handle
// that case by no-oping so local dev does not require NATS.
func connectNATSForMemory(ctx context.Context, logger *slog.Logger) *crawblnats.Client {
	cfg := crawblnats.DefaultConfig()
	cfg.URL = strings.TrimSpace(os.Getenv("CRAWBL_NATS_URL"))
	client, err := crawblnats.Connect(ctx, cfg, logger)
	if err != nil {
		logger.Warn("NATS connect failed, memory publishing disabled", "error", err)
		return nil
	}
	return client
}

// buildMemoryStack constructs the memory.Stack and its embedder when
// CRAWBL_EMBED_BASE_URL is configured. Returns (nil, nil) otherwise so
// downstream services fall back to messages-only context.
func buildMemoryStack(logger *slog.Logger, drawerRepo mcpDrawerRepoRaw, identityRepo mcpIdentityRepoRaw) (layers.Stack, embed.Embedder) {
	baseURL := os.Getenv("CRAWBL_EMBED_BASE_URL")
	if baseURL == "" {
		logger.Warn("memory stack disabled: CRAWBL_EMBED_BASE_URL not set — WakeUp context injection and semantic search will be unavailable")
		return nil, nil
	}
	embedder := embed.NewProvider(embed.ProviderConfig{
		BaseURL: baseURL,
		APIKey:  os.Getenv("CRAWBL_EMBED_API_KEY"),
		Model:   os.Getenv("CRAWBL_EMBED_MODEL"),
	})
	stack := layers.NewStack(drawerRepo, identityRepo, embedder)
	logger.Info("memory stack enabled", slog.String("base_url", baseURL))
	return stack, embedder
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildHTTPMiddleware() *middleware.MiddlewareConfig {
	return &middleware.MiddlewareConfig{
		Environment: envOrDefault("CRAWBL_ENVIRONMENT", middleware.EnvironmentLocal),
		E2EToken:    os.Getenv("CRAWBL_E2E_TOKEN"),
	}
}

// coreRepoWiring is declared at the wiring layer (cmd) where these repos
// are constructed. Fields are the narrow consumer-side interfaces needed
// by the various orchestrator wiring callsites (buildRealtime,
// buildMCPHandler, chatservice, etc.). Keeping the field types as
// interfaces avoids cross-package exposure of concrete repo structs.
type coreRepoWiring struct {
	User         coreUserRepo
	Workspace    coreWorkspaceRepo
	Agent        coreAgentRepo
	Conversation coreConversationRepo
	Message      coreMessageRepo
}

func mustBuildRepos(logger *slog.Logger) (*dbr.Connection, coreRepoWiring, func()) {
	logger.Info("configuring storage backend", slog.String("backend", "postgres"))
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	if err := database.EnsureSchema(dbConfig); err != nil {
		// panic instead of log.Fatal so that deferred cleanup (telemetry flush) runs.
		panic(err)
	}
	db, err := database.New(dbConfig)
	if err != nil {
		// panic instead of log.Fatal so that deferred cleanup (telemetry flush) runs.
		panic(err)
	}
	return db, coreRepoWiring{
			User:         userrepo.New(),
			Workspace:    workspacerepo.New(),
			Agent:        agentrepo.New(),
			Conversation: conversationrepo.New(),
			Message:      messagerepo.New(),
		}, func() {
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

// mcpHandlerDeps groups all dependencies required to build the MCP HTTP handler.
// Separating them from positional parameters keeps the constructor readable and
// satisfies the CLAUDE.md max-5-params rule.
type mcpHandlerDeps struct {
	Ctx              context.Context
	Logger           *slog.Logger
	DB               *dbr.Connection
	WorkspaceRepo    coreWorkspaceRepo
	AgentRepo        coreAgentRepo
	ConversationRepo coreConversationRepo
	MessageRepo      coreMessageRepo
	AgentHistoryRepo mcpAgentHistoryCreator
	ArtifactRepo     artifactrepo.Repo
	RuntimeClient    userswarmclient.Client
	Broadcaster      realtime.Broadcaster
	DrawerRepo       mcpDrawerRepoRaw
	KGRepo           mcpKGRepoRaw
	PalaceGraphRepo  mcpPalaceGraphRepoRaw
	IdentityRepo     mcpIdentityRepoRaw
	Classifier       extract.Classifier
	Embedder         embed.Embedder
	MemoryStack      layers.Stack
}

func buildMCPHandler(deps mcpHandlerDeps) http.Handler {
	ctx := deps.Ctx
	logger := deps.Logger
	db := deps.DB
	workspaceRepo := deps.WorkspaceRepo
	agentRepo := deps.AgentRepo
	conversationRepo := deps.ConversationRepo
	messageRepo := deps.MessageRepo
	agentHistoryRepo := deps.AgentHistoryRepo
	artifactRepo := deps.ArtifactRepo
	runtimeClient := deps.RuntimeClient
	broadcaster := deps.Broadcaster
	drawerRepo := deps.DrawerRepo
	kgRepo := deps.KGRepo
	palaceGraphRepo := deps.PalaceGraphRepo
	identityRepo := deps.IdentityRepo
	classifier := deps.Classifier
	embedder := deps.Embedder
	memoryStack := deps.MemoryStack
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
	workflowSvc := workflowservice.MustNew(db, wfRepo, runtimeClient, broadcaster)

	auditRepo := auditrepo.New()
	auditSvc := auditservice.MustNew(auditRepo)

	mcpRepo := mcprepo.New()
	mcpSvc := mcpservice.MustNew(
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
			ShutdownCtx:   ctx,
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
		KG:           kgRepo,
		MemoryStack:  memoryStack,
		PalaceGraph:  palaceGraphRepo,
		IdentityRepo: identityRepo,
		Classifier:   classifier,
		Embedder:     embedder,
	})
	logger.Info("MCP server enabled at /mcp/v1")
	return handler
}

// buildSharedRedis creates the single Redis client used by realtime and the
// palace-graph cache. Returns (nil, noop) when CRAWBL_REDIS_ADDR is unset or
// the ping fails — callers are expected to handle a nil client gracefully.
func buildSharedRedis(logger *slog.Logger) (redisclient.Client, func()) {
	addr := strings.TrimSpace(os.Getenv("CRAWBL_REDIS_ADDR"))
	if addr == "" {
		logger.Info("redis disabled: CRAWBL_REDIS_ADDR not set")
		return nil, noopCleanup
	}
	redisCfg := redisclient.ConfigFromEnv("CRAWBL_")
	rc, err := redisclient.New(redisCfg)
	if err != nil {
		logger.Error("failed to connect to Redis, continuing without it", "error", err)
		return nil, noopCleanup
	}
	logger.Info("redis connected", slog.String("addr", redisCfg.Addr))
	return rc, func() { _ = rc.Close() }
}

// buildRealtime constructs the socket.io broadcaster on top of the shared
// Redis client. A nil client disables realtime entirely and returns a
// NopBroadcaster so downstream services remain functional.
//
// db, workspaceRepo, and authService are forwarded to the Socket.IO server so
// it can verify workspace ownership before joining rooms on workspace.subscribe
// events. authService resolves the Firebase subject to an internal user.ID.
func buildRealtime(logger *slog.Logger, rc redisclient.Client, db *dbr.Connection, workspaceRepo coreWorkspaceRepo, authService *authservice.Service) (realtime.Broadcaster, http.Handler, *socket.Server, func()) {
	if rc == nil {
		logger.Info("realtime disabled: no redis client")
		return realtime.NopBroadcaster{}, nil, nil, noopCleanup
	}

	io := socketio.NewServer(&socketio.Config{
		Logger:        logger,
		RedisClient:   redisclient.Unwrap(rc),
		DB:            db,
		WorkspaceRepo: workspaceRepo,
		AuthService:   authService,
	})

	broadcaster := socketio.NewBroadcaster(io, logger)
	handler := socketio.Handler(io)

	cleanup := func() {
		io.Close(nil)
	}

	logger.Info("realtime enabled: socket.io + redis")
	return broadcaster, handler, io, cleanup
}

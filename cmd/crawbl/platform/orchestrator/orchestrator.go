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
var noopCleanup = func() {
	// intentionally empty — lifecycle hook reserved for future use
}

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

// resolveLogLevel maps the LOG_LEVEL environment variable to a slog.Level.
// Unrecognised values default to Info.
func resolveLogLevel() slog.Level {
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: resolveLogLevel()}))
	slog.SetDefault(logger)

	telemetryShutdown, tErr := telemetry.Init(ctx, telemetry.ConfigFromEnv("orchestrator", os.Getenv("CRAWBL_VERSION")), logger)
	if tErr != nil {
		logger.Warn("telemetry init failed, continuing without metrics export", "error", tErr)
	}
	defer shutdownTelemetry(telemetryShutdown, logger)

	db, repos, cleanup := mustBuildRepos(logger)
	defer cleanup()

	redisClient, cleanupRedis := buildSharedRedis(logger)
	defer cleanupRedis()

	memRepos := buildMemoryRepos(redisClient, logger)
	classifier := extract.NewClassifier()
	memoryStack, embedder := buildMemoryStack(logger, memRepos.drawer, memRepos.identity)

	if err := pkgriver.Migrate(ctx, db.DB); err != nil {
		return fmt.Errorf("river migration failed: %w", err)
	}
	logger.Info("river migrations applied")

	clickhouseDB, err := clickhouse.Open(ctx, logger)
	if err != nil {
		return fmt.Errorf("clickhouse open: %w", err)
	}
	defer closeIfNonNil(clickhouseDB)
	llmUsageRepo := llmusagerepo.New(clickhouseDB)

	natsClient, cleanupNATS := buildNATS(ctx, logger)
	defer cleanupNATS()
	memoryPublisher := queue.NewMemoryPublisher(natsClient, logger)

	pricingCache := pricing.New(db, modelpricingrepo.New(), logger)
	pricingCache.Start(ctx)

	riverClient, err := buildAndStartRiver(riverOpts{
		ctx:          ctx,
		logger:       logger,
		db:           db,
		mem:          memRepos,
		embedder:     embedder,
		messageRepo:  repos.Message,
		pricingCache: pricingCache,
		llmUsageRepo: llmUsageRepo,
	})
	if err != nil {
		return err
	}
	defer pkgriver.Shutdown(riverClient, logger)

	ingestPool, err := buildIngestPool(db, memRepos, classifier, embedder, memoryPublisher, logger)
	if err != nil {
		return fmt.Errorf("memory.autoingest: %w", err)
	}
	defer shutdownIngestPool(ingestPool, logger)

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}

	workspaceService := workspaceservice.MustNew(repos.Workspace, runtimeClient, logger)
	authService := authservice.MustNew(repos.User, workspaceService, legalDocumentsFromEnv(), usagequotarepo.New())

	broadcaster, socketIOHandler, ioServer, cleanupRT := buildRealtime(logger, redisClient, db, repos.Workspace, authService)
	defer cleanupRT()

	toolsRepo := toolsrepo.New()
	agentSettingsRepo := agentsettingsrepo.New()
	agentPromptsRepo := agentpromptsrepo.New()
	agentHistoryRepo := agenthistoryrepo.New()
	artifactRepo := artifactrepo.New()
	usagePublisher := queue.NewUsagePublisher(riverClient, logger)

	chatService := chatservice.MustNew(chatservice.Deps{
		DB: db,
		Repos: chatservice.Repos{
			Workspace:     repos.Workspace,
			Agent:         repos.Agent,
			Conversation:  repos.Conversation,
			Message:       repos.Message,
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
			Workspace:     repos.Workspace,
			Agent:         repos.Agent,
			Tools:         toolsRepo,
			AgentSettings: agentSettingsRepo,
			AgentPrompts:  agentPromptsRepo,
			AgentHistory:  agentHistoryRepo,
			Usage:         usagerepo.New(),
			Drawer:        memRepos.drawer,
		},
		runtimeClient,
	)
	integrationConnRepo := integrationconnrepo.New()
	integrationService := integrationservice.MustNew(logger, integrationConnRepo)

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
		WorkspaceRepo:    repos.Workspace,
		AgentRepo:        repos.Agent,
		ConversationRepo: repos.Conversation,
		MessageRepo:      repos.Message,
		AgentHistoryRepo: agentHistoryRepo,
		ArtifactRepo:     artifactRepo,
		RuntimeClient:    runtimeClient,
		Broadcaster:      broadcaster,
		DrawerRepo:       memRepos.drawer,
		KGRepo:           memRepos.kg,
		PalaceGraphRepo:  memRepos.palaceGraph,
		IdentityRepo:     memRepos.identity,
		Classifier:       classifier,
		Embedder:         embedder,
		MemoryStack:      memoryStack,
	})

	riverUIHandler, riverUIHost, err := buildRiverUI(ctx, logger, riverClient)
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
		HTTPMiddleware:     buildHTTPMiddleware(),
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

// memoryRepoBundle groups the memory-system repos constructed at the wiring
// layer. Keeps runServer's local variable count manageable.
type memoryRepoBundle struct {
	drawer      mcpDrawerRepoRaw
	kg          mcpKGRepoRaw
	palaceGraph mcpPalaceGraphRepoRaw
	identity    mcpIdentityRepoRaw
	centroid    mcpCentroidRepoRaw
}

// buildMemoryRepos constructs every memory-layer repo. The concrete *Postgres
// structs are narrowed to the wiring-layer interfaces declared in ports.go.
func buildMemoryRepos(redisClient redisclient.Client, logger *slog.Logger) memoryRepoBundle {
	return memoryRepoBundle{
		drawer:      drawerrepo.NewPostgres(),
		kg:          kgrepo.NewPostgres(),
		palaceGraph: palacegraphrepo.NewPostgres(redisClient, logger),
		identity:    identityrepo.NewPostgres(),
		centroid:    centroidrepo.NewPostgres(),
	}
}

// shutdownTelemetry gracefully shuts down the telemetry provider.
func shutdownTelemetry(shutdown func(context.Context) error, logger *slog.Logger) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		logger.Warn("telemetry shutdown returned error", "error", err)
	}
}

// closeIfNonNil closes a *sql.DB if it is not nil.
func closeIfNonNil(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}

// buildNATS connects to the NATS server for memory fan-out events. Returns a
// nil client (safe for downstream) and a no-op cleanup when the connection
// fails or the URL is unset.
func buildNATS(ctx context.Context, logger *slog.Logger) (*crawblnats.Client, func()) {
	natsCfg := crawblnats.DefaultConfig()
	natsCfg.URL = strings.TrimSpace(os.Getenv("CRAWBL_NATS_URL"))
	natsClient, natsErr := crawblnats.Connect(ctx, natsCfg, logger)
	if natsErr != nil {
		logger.Warn("NATS connect failed, memory publishing disabled", "error", natsErr)
		return nil, noopCleanup
	}
	return natsClient, func() { _ = natsClient.Close() }
}

// riverOpts groups the dependencies needed to build and start River.
type riverOpts struct {
	ctx          context.Context
	logger       *slog.Logger
	db           *dbr.Connection
	mem          memoryRepoBundle
	embedder     embed.Embedder
	messageRepo  coreMessageRepo
	pricingCache *pricing.Cache
	llmUsageRepo llmusagerepo.Repo
}

// buildAndStartRiver creates the River config, constructs the client, runs
// schema migrations, and starts the background workers.
func buildAndStartRiver(opts riverOpts) (*pkgriver.Client, error) {
	ctx := opts.ctx
	logger := opts.logger
	db := opts.db
	mem := opts.mem
	embedder := opts.embedder
	messageRepo := opts.messageRepo
	pricingCache := opts.pricingCache
	llmUsageRepo := opts.llmUsageRepo
	riverCfg, err := queue.NewConfig(queue.Deps{
		DB:               db,
		Logger:           logger,
		DrawerRepo:       mem.drawer,
		KGRepo:           mem.kg,
		CentroidRepo:     mem.centroid,
		LLMClassifier:    newLLMClassifierOrNil(),
		Embedder:         embedder,
		MessageRepo:      messageRepo,
		ModelPricingRepo: modelpricingrepo.New(),
		PricingCache:     pricingCache,
		LLMUsageRepo:     llmUsageRepo,
	})
	if err != nil {
		return nil, fmt.Errorf("river config: %w", err)
	}

	riverClient, err := pkgriver.New(db.DB, riverCfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}

	if err := riverClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("river start: %w", err)
	}
	logger.Info("river client started", "queues", "memory_process,memory_maintain,memory_enrich,memory_centroid,usage_write,pricing_refresh,message_cleanup")
	return riverClient, nil
}

// buildIngestPool constructs the in-process auto-ingest worker pool.
func buildIngestPool(
	db *dbr.Connection,
	mem memoryRepoBundle,
	classifier extract.Classifier,
	embedder embed.Embedder,
	memoryPublisher *queue.MemoryPublisher,
	logger *slog.Logger,
) (autoingest.Service, error) {
	ingestWorkers, _ := strconv.Atoi(os.Getenv("CRAWBL_AUTOINGEST_WORKERS"))
	ingestCapacity, _ := strconv.Atoi(os.Getenv("CRAWBL_AUTOINGEST_CAPACITY"))
	return autoingest.NewService(autoingest.Deps{
		DB:              db,
		DrawerRepo:      mem.drawer,
		CentroidRepo:    mem.centroid,
		Classifier:      classifier,
		Embedder:        embedder,
		MemoryPublisher: memoryPublisher,
		Logger:          logger,
	}, autoingest.Config{
		Workers:   ingestWorkers,
		QueueSize: ingestCapacity,
	})
}

// shutdownIngestPool gracefully shuts down the auto-ingest pool.
func shutdownIngestPool(pool autoingest.Service, logger *slog.Logger) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.Shutdown(shutdownCtx); err != nil {
		logger.Warn("memory.autoingest: shutdown timeout", "error", err)
	}
}

// buildRiverUI constructs the River UI dashboard handler when enabled via
// CRAWBL_RIVERUI_HOST. Returns (nil, "", nil) when disabled.
func buildRiverUI(ctx context.Context, logger *slog.Logger, riverClient *pkgriver.Client) (http.Handler, string, error) {
	riverUIHost := strings.TrimSpace(os.Getenv("CRAWBL_RIVERUI_HOST"))
	if riverUIHost == "" {
		return nil, "", nil
	}
	endpoints := riverui.NewEndpoints(riverClient, nil)
	ruiHandler, ruiErr := riverui.NewHandler(&riverui.HandlerOpts{
		Endpoints: endpoints,
		Logger:    logger,
	})
	if ruiErr != nil {
		return nil, "", fmt.Errorf("riverui handler: %w", ruiErr)
	}
	if ruiErr = ruiHandler.Start(ctx); ruiErr != nil {
		return nil, "", fmt.Errorf("riverui start: %w", ruiErr)
	}
	logger.Info("riverui enabled", slog.String("host", riverUIHost))
	return ruiHandler, riverUIHost, nil
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

// buildMemoryStack constructs the embedder and memory stack when
// CRAWBL_EMBED_BASE_URL is set. Returns (nil, nil) when the env var is absent
// so downstream services degrade gracefully to messages-only context.
func buildMemoryStack(logger *slog.Logger, drawerRepo mcpDrawerRepoRaw, identityRepo mcpIdentityRepoRaw) (layers.Stack, embed.Embedder) {
	baseURL := strings.TrimSpace(os.Getenv("CRAWBL_EMBED_BASE_URL"))
	if baseURL == "" {
		logger.Warn("memory stack disabled: CRAWBL_EMBED_BASE_URL not set — WakeUp context injection and semantic search will be unavailable")
		return nil, nil
	}
	embedder := embed.NewProvider(embed.ProviderConfig{
		BaseURL: baseURL,
		APIKey:  os.Getenv("CRAWBL_EMBED_API_KEY"),
		Model:   os.Getenv("CRAWBL_EMBED_MODEL"),
	})
	logger.Info("memory stack enabled", slog.String("base_url", baseURL))
	return layers.NewStack(drawerRepo, identityRepo, embedder), embedder
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

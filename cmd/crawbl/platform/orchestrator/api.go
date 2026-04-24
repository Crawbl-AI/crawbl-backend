package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/centroidrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/identityrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agenthistoryrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentpromptsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentsettingsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/integrationconnrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/toolsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagequotarepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/socketio"
	agentservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/agentservice"
	authservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/authservice"
	chatservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/chatservice"
	integrationservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/integrationservice"
	workspaceservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workspaceservice"
)

func newAPICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "api",
		Short: "Start the REST API + Socket.IO server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAPI(cmd.Context())
		},
	}
}

// runAPI starts the mobile-facing REST API and Socket.IO server. It
// initialises everything the HTTP layer needs (auth, chat, agents,
// integrations, realtime, autoingest) but does NOT run River workers,
// the MCP handler, or connect to ClickHouse.
func runAPI(ctx context.Context) error {
	logger, telemetryCleanup := initLogging(ctx, "orchestrator-api")
	defer telemetryCleanup()

	db, repos, cleanup := mustBuildRepos(logger)
	defer cleanup()
	userRepo := repos.User
	workspaceRepo := repos.Workspace
	agentRepo := repos.Agent
	conversationRepo := repos.Conversation
	messageRepo := repos.Message

	redisClient, cleanupRedis := buildSharedRedis(logger)
	defer cleanupRedis()

	// Memory system — embedder is optional. Only the repos needed by
	// autoingest, layers.Stack, and agentservice are constructed here.
	var drawerRepo mcpDrawerRepoRaw = drawerrepo.NewPostgres()
	var identityRepo mcpIdentityRepoRaw = identityrepo.NewPostgres()
	var centroidRepo mcpCentroidRepoRaw = centroidrepo.NewPostgres()
	classifier := extract.NewClassifier()

	memoryStack, embedder := buildMemoryStack(logger, drawerRepo, identityRepo)

	// NATS client for memory fan-out events (needed by auto-ingest pool).
	natsCfg := queue.DefaultNATSConfig()
	natsCfg.URL = strings.TrimSpace(os.Getenv("CRAWBL_NATS_URL"))
	natsClient, natsErr := queue.ConnectNATS(ctx, natsCfg, logger)
	if natsErr != nil {
		logger.Warn("NATS connect failed, memory publishing disabled", "error", natsErr)
	}
	defer func() {
		if natsClient != nil {
			_ = natsClient.Close()
		}
	}()
	memoryPublisher := queue.NewMemoryPublisher(natsClient, logger)

	pricingCache := queue.NewPricingCache(db, modelpricingrepo.New(), logger)
	pricingCache.Start(ctx)

	// River client — API only publishes usage jobs, does not process them.
	// The client is NOT started (no Start call) because Start requires
	// Queues+Workers config. Insert/InsertTx work without Start.
	riverClient, err := queue.NewRiverClient(db.DB, nil)
	if err != nil {
		logger.Error("river client construction failed", "error", err)
		return fmt.Errorf("river client: %w", err)
	}
	logger.Info("river client ready (publish-only)")

	// In-process auto-ingest pool (decision C4 — runs in the API process).
	ingestWorkers, _ := strconv.Atoi(os.Getenv("CRAWBL_AUTOINGEST_WORKERS"))
	ingestCapacity, _ := strconv.Atoi(os.Getenv("CRAWBL_AUTOINGEST_CAPACITY"))
	ingestPool, err := autoingest.NewService(autoingest.Deps{
		DB:              db,
		DrawerRepo:      drawerRepo,
		CentroidRepo:    centroidRepo,
		Classifier:      classifier,
		Embedder:        embedder,
		MemoryPublisher: memoryPublisher,
		Logger:          logger,
	}, autoingest.Config{
		Workers:   ingestWorkers,
		QueueSize: ingestCapacity,
	})
	if err != nil {
		return fmt.Errorf("memory.autoingest: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ingestPool.Shutdown(shutdownCtx); err != nil {
			logger.Warn("memory.autoingest: shutdown timeout", "error", err)
		}
	}()

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}
	httpMiddleware := buildHTTPMiddleware()

	workspaceService := workspaceservice.MustNew(workspaceRepo, runtimeClient, logger)
	authService := authservice.MustNew(userRepo, workspaceService, legalDocumentsFromEnv(), usagequotarepo.New())

	broadcaster, socketIOHandler, ioServer, cleanupRT := buildRealtime(ctx, logger, redisClient, db, workspaceRepo, authService)
	defer cleanupRT()

	toolsRepo := toolsrepo.New()
	agentSettingsRepo := agentsettingsrepo.New()
	agentPromptsRepo := agentpromptsrepo.New()
	agentHistoryRepo := agenthistoryrepo.New()

	usagePublisher := queue.NewUsagePublisher(riverClient, logger)

	chatService := chatservice.MustNew(chatservice.Deps{
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

	// Register Socket.IO message.send handler.
	if ioServer != nil {
		socketio.RegisterMessageHandler(ioServer, &socketio.Config{
			Logger:           logger,
			DB:               db,
			ChatService:      chatService,
			AuthService:      authService,
			WorkspaceService: workspaceService,
		}, ctx)
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
		MCPHandler:         nil,
		IntegrationService: integrationService,
		MCPSigningKey:      strings.TrimSpace(os.Getenv("CRAWBL_MCP_SIGNING_KEY")),
	})

	logger.Info("starting API server", slog.String("port", envOrDefault("CRAWBL_SERVER_PORT", server.DefaultServerPort)))
	return srv.Run(ctx, shutdownTimeout)
}

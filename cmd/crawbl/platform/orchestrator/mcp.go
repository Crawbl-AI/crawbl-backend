package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/identityrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/kgrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/palacegraphrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agenthistoryrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/mcpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/telemetry"
)

func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCP(cmd.Context())
		},
	}
}

// runMCP starts the agent-facing MCP server on a dedicated port. It
// initialises only the repos and services the MCP handler needs (memory
// repos, embedder, runtime client) and does NOT run Socket.IO, River
// workers, auth/chat/agent services, NATS, ClickHouse, or autoingest.
func runMCP(ctx context.Context) error {
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

	telemetryShutdown, tErr := telemetry.Init(ctx, telemetry.ConfigFromEnv("orchestrator-mcp", os.Getenv("CRAWBL_VERSION")), logger)
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
	workspaceRepo := repos.Workspace
	agentRepo := repos.Agent
	conversationRepo := repos.Conversation
	messageRepo := repos.Message

	redisClient, cleanupRedis := buildSharedRedis(logger)
	defer cleanupRedis()

	// Memory repos — all needed by buildMCPHandler.
	var drawerRepo mcpDrawerRepoRaw = drawerrepo.NewPostgres()
	var kgRepo mcpKGRepoRaw = kgrepo.NewPostgres()
	var palaceGraphRepo mcpPalaceGraphRepoRaw = palacegraphrepo.NewPostgres(redisClient, logger)
	var identityRepo mcpIdentityRepoRaw = identityrepo.NewPostgres()
	classifier := extract.NewClassifier()

	var memoryStack layers.Stack
	var embedder embed.Embedder
	if baseURL := os.Getenv("CRAWBL_EMBED_BASE_URL"); baseURL != "" {
		embedder = embed.NewProvider(embed.ProviderConfig{
			BaseURL: baseURL,
			APIKey:  os.Getenv("CRAWBL_EMBED_API_KEY"),
			Model:   os.Getenv("CRAWBL_EMBED_MODEL"),
		})
		memoryStack = layers.NewStack(drawerRepo, identityRepo, embedder)
		logger.Info("memory stack enabled", slog.String("base_url", baseURL))
	} else {
		logger.Warn("memory stack disabled: CRAWBL_EMBED_BASE_URL not set")
	}

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}

	agentHistoryRepo := agenthistoryrepo.New()
	artifactRepo := artifactrepo.New()

	// The MCP handler needs a broadcaster for push notifications; the MCP
	// process does not run Socket.IO, so we use the NopBroadcaster.
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
		Broadcaster:      realtime.NopBroadcaster{},
		DrawerRepo:       drawerRepo,
		KGRepo:           kgRepo,
		PalaceGraphRepo:  palaceGraphRepo,
		IdentityRepo:     identityRepo,
		Classifier:       classifier,
		Embedder:         embedder,
		MemoryStack:      memoryStack,
	})
	if mcpHandler == nil {
		return fmt.Errorf("MCP handler not created: CRAWBL_MCP_SIGNING_KEY is required")
	}

	mcpSrv := mcpserver.New(&mcpserver.Config{
		Port: envOrDefault("CRAWBL_MCP_SERVER_PORT", mcpserver.DefaultPort),
	}, mcpHandler, logger)

	return mcpSrv.Run(ctx, shutdownTimeout)
}

package orchestrator

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/identityrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/kgrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/palacegraphrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agenthistoryrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/mcpserver"
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
	logger, telemetryCleanup := initLogging(ctx, "orchestrator-mcp")
	defer telemetryCleanup()

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

	memoryStack, embedder := buildMemoryStack(logger, drawerRepo, identityRepo)

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}

	agentHistoryRepo := agenthistoryrepo.New()
	artifactRepo := artifactrepo.New()

	// The MCP handler needs a broadcaster for push notifications; the MCP
	// process does not run Socket.IO, so we use the NopBroadcaster.
	mcpHandler := buildMCPHandler(ctx, mcpHandlerDeps{
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

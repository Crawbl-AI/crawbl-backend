package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/httputil"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/centroidrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/kgrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/llmusagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
)

func newWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Start the River background job processor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorker(cmd.Context())
		},
	}
}

// runWorker starts the River background job processor with a minimal
// health endpoint. It initialises only the repos and infrastructure
// that background jobs need (memory repos, embedder, ClickHouse, NATS,
// pricing cache) and does NOT run Redis, Socket.IO, auth/chat/agent
// services, the MCP handler, the runtime client, or autoingest.
func runWorker(ctx context.Context) error {
	logger, telemetryCleanup := initLogging(ctx, "orchestrator-worker")
	defer telemetryCleanup()

	db, repos, cleanup := mustBuildRepos(logger)
	defer cleanup()
	messageRepo := repos.Message

	// Memory repos needed by River workers.
	var drawerRepo mcpDrawerRepoRaw = drawerrepo.NewPostgres()
	var kgRepo mcpKGRepoRaw = kgrepo.NewPostgres()
	var centroidRepo mcpCentroidRepoRaw = centroidrepo.NewPostgres()

	var embedder embed.Embedder
	if baseURL := os.Getenv("CRAWBL_EMBED_BASE_URL"); baseURL != "" {
		embedder = embed.NewProvider(embed.ProviderConfig{
			BaseURL: baseURL,
			APIKey:  os.Getenv("CRAWBL_EMBED_API_KEY"),
			Model:   os.Getenv("CRAWBL_EMBED_MODEL"),
		})
		logger.Info("embedder enabled", slog.String("base_url", baseURL))
	} else {
		logger.Warn("embedder disabled: CRAWBL_EMBED_BASE_URL not set")
	}

	// River schema migration — runs before starting workers.
	if err := queue.MigrateRiver(ctx, db.DB); err != nil {
		logger.Error("river migration failed", "error", err)
		return fmt.Errorf("river migration: %w", err)
	}
	logger.Info("river migrations applied")

	clickhouseDB, err := queue.OpenClickhouse(ctx, logger)
	if err != nil {
		return fmt.Errorf("clickhouse open: %w", err)
	}
	defer func() {
		if clickhouseDB != nil {
			_ = clickhouseDB.Close()
		}
	}()
	llmUsageRepo := llmusagerepo.New(clickhouseDB)

	pricingCache := queue.NewPricingCache(db, modelpricingrepo.New(), logger)
	pricingCache.Start(ctx)

	// Build River worker configuration with all background jobs.
	riverCfg, err := queue.NewConfig(queue.Deps{
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
		logger.Error("river config failed", "error", err)
		return fmt.Errorf("river config: %w", err)
	}

	riverClient, err := queue.NewRiverClient(db.DB, riverCfg)
	if err != nil {
		logger.Error("river client construction failed", "error", err)
		return fmt.Errorf("river client: %w", err)
	}
	if err := riverClient.Start(ctx); err != nil {
		logger.Error("river client start failed", "error", err)
		return fmt.Errorf("river start: %w", err)
	}
	defer queue.ShutdownRiver(riverClient, logger)
	logger.Info("river client started", "queues", "memory_process,memory_maintain,memory_enrich,memory_centroid,usage_write,pricing_refresh,message_cleanup")

	healthSrv := httputil.NewHealthServer(&httputil.HealthConfig{
		Port: envOrDefault("CRAWBL_WORKER_HEALTH_PORT", httputil.DefaultHealthPort),
	}, logger)

	return healthSrv.Run(ctx, shutdownTimeout)
}

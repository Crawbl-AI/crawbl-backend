package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/jobs"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("memory-process: fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dbCfg := database.ConfigFromEnv("CRAWBL_")
	db, err := database.New(dbCfg)
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer func() { _ = db.Close() }()

	baseURL := os.Getenv("CRAWBL_EMBED_BASE_URL")
	if baseURL == "" {
		return fmt.Errorf("CRAWBL_EMBED_BASE_URL is required")
	}
	embedder := embed.NewProvider(embed.ProviderConfig{
		BaseURL: baseURL,
		APIKey:  os.Getenv("CRAWBL_EMBED_API_KEY"),
		Model:   os.Getenv("CRAWBL_EMBED_MODEL"),
	})

	llmBaseURL := os.Getenv("CRAWBL_LLM_BASE_URL")
	if llmBaseURL == "" {
		llmBaseURL = baseURL
	}
	llmAPIKey := os.Getenv("CRAWBL_LLM_API_KEY")
	if llmAPIKey == "" {
		llmAPIKey = os.Getenv("CRAWBL_EMBED_API_KEY")
	}
	llmClassifier := extract.NewLLMClassifier(extract.LLMClassifierConfig{
		BaseURL: llmBaseURL,
		APIKey:  llmAPIKey,
		Model:   os.Getenv("CRAWBL_CLASSIFY_MODEL"),
	})

	result, err := jobs.RunProcess(ctx, jobs.ProcessDeps{
		DB:            db,
		DrawerRepo:    drawer.NewPostgres(),
		KGGraph:       kg.NewPostgres(),
		LLMClassifier: llmClassifier,
		Embedder:      embedder,
	})
	if err != nil {
		return fmt.Errorf("processing failed: %w", err)
	}

	slog.Info("memory-process: complete",
		slog.Int("processed", result.Processed),
		slog.Int("failed", result.Failed),
	)
	return nil
}

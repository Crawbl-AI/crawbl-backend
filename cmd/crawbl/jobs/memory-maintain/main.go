package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/jobs"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("memory-maintain: fatal", "error", err)
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

	deps := jobs.MaintainDeps{
		DB:         db,
		DrawerRepo: drawer.NewPostgres(),
	}

	result, err := jobs.RunMaintain(ctx, deps)
	if err != nil {
		return fmt.Errorf("maintenance failed: %w", err)
	}

	slog.Info("memory-maintain: complete",
		slog.Int("workspaces", result.Workspaces),
		slog.Int("decayed", result.Decayed),
		slog.Int("pruned", result.Pruned),
	)
	return nil
}

// Package queue owns every River-backed background job, periodic schedule,
// and outbound event publisher used by the orchestrator.
package queue

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

// NewRiverClient constructs a River client bound to the given *sql.DB via
// the riverdatabasesql driver.
//
// A nil cfg is allowed for publish-only callers. River requires a non-nil
// config, so we substitute an empty one in that case.
func NewRiverClient(db *sql.DB, cfg *RiverConfig) (*RiverClient, error) {
	if cfg == nil {
		cfg = &river.Config{}
	}
	client, err := river.NewClient(riverdatabasesql.New(db), cfg)
	if err != nil {
		return nil, fmt.Errorf("new river client: %w", err)
	}
	return client, nil
}

// MigrateRiver applies River's own schema migrations (river_job, river_leader,
// river_migration tables). Idempotent — safe to run on every boot.
func MigrateRiver(ctx context.Context, db *sql.DB) error {
	migrator, err := rivermigrate.New(riverdatabasesql.New(db), nil)
	if err != nil {
		return fmt.Errorf("new river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}
	return nil
}

// ShutdownRiver performs the River-recommended three-phase graceful shutdown.
// Safe to call with a nil client (no-op).
func ShutdownRiver(client *RiverClient, logger *slog.Logger) {
	if client == nil {
		return
	}

	softCtx, softCancel := context.WithTimeout(context.Background(), defaultSoftStopTimeout)
	defer softCancel()
	if err := client.Stop(softCtx); err != nil {
		logger.Warn("river soft stop exceeded deadline, escalating to cancel", "error", err)
		hardCtx, hardCancel := context.WithTimeout(context.Background(), defaultHardStopTimeout)
		defer hardCancel()
		if err := client.StopAndCancel(hardCtx); err != nil {
			logger.Error("river force stop failed", "error", err)
			return
		}
	}
	logger.Info("river client stopped")
}

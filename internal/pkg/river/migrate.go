package river

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

// Migrate applies River's own schema migrations (river_job, river_leader,
// river_migration tables). Idempotent — safe to run on every boot. Call
// before New / Client.Start at orchestrator startup.
func Migrate(ctx context.Context, db *sql.DB) error {
	migrator, err := rivermigrate.New(riverdatabasesql.New(db), nil)
	if err != nil {
		return fmt.Errorf("new river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}
	return nil
}

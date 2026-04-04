package orchestrator

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const defaultServiceName = "orchestrator"

func newMigrateCommand() *cobra.Command {
	var serviceName string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations for the orchestrator",
		Long:  "Run the orchestrator database migrations for the selected service migration set.",
		RunE: func(_ *cobra.Command, _ []string) error {
			dbConfig := database.ConfigFromEnv("CRAWBL_")
			if err := database.EnsureSchema(dbConfig); err != nil {
				return err
			}

			migrationRunner, err := migrate.New(
				"file://./migrations/"+serviceName,
				database.BuildDSN(dbConfig, true),
			)
			if err != nil {
				return err
			}

			if err := migrationRunner.Up(); err != nil && err != migrate.ErrNoChange {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&serviceName, "svc", defaultServiceName, "Migration service name")

	return cmd
}

// autoMigrate runs pending database migrations on server startup.
// Uses golang-migrate with the file source pointing to the migrations directory.
// In containers, migrations are at /migrations/orchestrator.
// Locally, they're at ./migrations/orchestrator.
//
// When CRAWBL_MIGRATE_FRESH=true (dev environments), migrations are dropped and
// re-applied from scratch on every deploy. This ensures schema changes in existing
// migration files are always picked up without manual intervention.
func autoMigrate(logger *slog.Logger) error {
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	if err := database.EnsureSchema(dbConfig); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	// Try container path first, then local path.
	migrationPath := "/migrations/orchestrator"
	if _, err := os.Stat(migrationPath); os.IsNotExist(err) {
		migrationPath = "./migrations/orchestrator"
	}
	if _, err := os.Stat(migrationPath); os.IsNotExist(err) {
		logger.Warn("migrations directory not found, skipping auto-migrate")
		return nil
	}

	m, err := migrate.New(
		"file://"+migrationPath,
		database.BuildDSN(dbConfig, true),
	)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	// Fresh mode: drop everything and re-apply from scratch.
	// Useful for dev environments where migration files are modified in-place.
	if os.Getenv("CRAWBL_MIGRATE_FRESH") == "true" {
		logger.Info("fresh migration mode: dropping and re-applying all migrations")
		if err := m.Drop(); err != nil {
			return fmt.Errorf("drop migrations: %w", err)
		}
		// Drop removes the schema_migrations table too, so we need to re-ensure schema
		// and re-create the migrator.
		if err := database.EnsureSchema(dbConfig); err != nil {
			return fmt.Errorf("re-ensure schema after drop: %w", err)
		}
		m, err = migrate.New(
			"file://"+migrationPath,
			database.BuildDSN(dbConfig, true),
		)
		if err != nil {
			return fmt.Errorf("re-create migrator after drop: %w", err)
		}
		defer m.Close()
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	logger.Info("database migrations applied successfully")
	return nil
}

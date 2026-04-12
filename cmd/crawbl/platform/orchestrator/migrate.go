package orchestrator

import (
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	orchestratormigrations "github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator"
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

			srcDriver, err := iofs.New(orchestratormigrations.FS, ".")
			if err != nil {
				return fmt.Errorf("create migration source: %w", err)
			}

			migrationRunner, err := migrate.NewWithSourceInstance("iofs", srcDriver, database.BuildDSN(dbConfig, true))
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
// Migrations are embedded into the binary via go:embed in the
// migrations/orchestrator package.
func autoMigrate(logger *slog.Logger) error {
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	if err := database.EnsureSchema(dbConfig); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	srcDriver, err := iofs.New(orchestratormigrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", srcDriver, database.BuildDSN(dbConfig, true))
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			slog.Warn("migrator: source close error", "error", srcErr.Error())
		}
		if dbErr != nil {
			slog.Warn("migrator: db close error", "error", dbErr.Error())
		}
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	logger.Info("database migrations applied successfully")
	return nil
}

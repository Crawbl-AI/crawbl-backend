package main

import (
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// defaultServiceName is the default migration service name used when
// no --svc flag is provided. It corresponds to the "orchestrator" migrations
// directory under ./migrations/.
const defaultServiceName = "orchestrator"

// newMigrateCommand creates the "migrate" subcommand for running database migrations.
// The command supports a --svc flag to specify which service's migrations to run,
// defaulting to "orchestrator". Migration files are loaded from ./migrations/{service}/.
// The command applies all pending migrations and succeeds silently if no changes
// are needed (ErrNoChange).
func newMigrateCommand() *cobra.Command {
	var serviceName string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
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

	cmd.Flags().StringVar(&serviceName, "svc", defaultServiceName, "migration service name")

	return cmd
}

package dev

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newStartCommand() *cobra.Command {
	var (
		clean        bool
		databaseOnly bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the local development stack",
		Long: `Start PostgreSQL, run database migrations, and launch the local orchestrator.

Use this command when you want a working local API with the standard Docker
Compose workflow. Pass --database-only to leave only PostgreSQL running for
host-side orchestrator development. Pass --clean to wipe the database and
start fresh.`,
		Example: `  crawbl dev start                  # Start the full local stack
  crawbl dev start --database-only  # Start only PostgreSQL + migrations
  crawbl dev start --clean          # Wipe database and start fresh`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(clean, databaseOnly)
		},
	}

	cmd.Flags().BoolVar(&clean, "clean", false, "Wipe the database volume before starting")
	cmd.Flags().BoolVar(&databaseOnly, "database-only", false, "Start PostgreSQL and run migrations without launching the orchestrator")
	return cmd
}

func runStart(clean, databaseOnly bool) error {
	// Ensure .env exists.
	ensureEnvFile()

	// Stop any running containers first.
	out.Step(style.Stopping, "Stopping existing containers...")
	_ = shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "down", "--remove-orphans")

	// If --clean, remove the Postgres volume.
	if clean {
		out.Step(style.Delete, "Removing the database volume...")
		_ = shellCmd("docker", "compose", "down", "-v")
	}

	// Start Postgres.
	out.Step(style.Database, "Starting PostgreSQL...")
	if err := shellCmd("docker", "compose", "--profile", "database", "up", "-d"); err != nil {
		return fmt.Errorf("failed to start Postgres: %w", err)
	}

	// Wait for Postgres to be ready.
	out.Step(style.Waiting, "Waiting for Postgres to become ready...")
	for i := 0; i < 30; i++ {
		if silentCmd("docker", "compose", "exec", "-T", "postgresdb", "pg_isready", "-h", "postgresdb") == nil {
			out.Step(style.Ready, "Postgres is ready")
			break
		}
		if i == 29 {
			return fmt.Errorf("Postgres did not become ready in 30 seconds")
		}
	}

	// Run migrations.
	out.Step(style.Migrate, "Running database migrations...")
	if err := shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "build", "migrations"); err != nil {
		return fmt.Errorf("migration build failed: %w", err)
	}
	if err := shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "run", "--rm", "migrations"); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	if databaseOnly {
		out.Ln()
		out.Success("PostgreSQL is running")
		out.Step(style.Tip, "Run the orchestrator on your host: ./crawbl platform orchestrator")
		out.Step(style.Tip, "Stop: ./crawbl dev stop")
		return nil
	}

	// Start the orchestrator.
	out.Step(style.Running, "Starting the orchestrator...")
	if err := shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "up", "-d", "--build", "--remove-orphans"); err != nil {
		return fmt.Errorf("failed to start orchestrator: %w", err)
	}

	out.Ln()
	out.Success("Server is running")
	out.Step(style.URL, "API: http://localhost:7171/v1/health")
	out.Step(style.Tip, "Stop: ./crawbl dev stop")
	return nil
}

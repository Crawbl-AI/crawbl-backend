package dev

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStartCommand() *cobra.Command {
	var clean bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the local development stack (Postgres + orchestrator)",
		Long: `Starts PostgreSQL, runs migrations, and launches the orchestrator.
Use --clean to wipe the database and start fresh.`,
		Example: `  crawbl dev start          # Normal start
  crawbl dev start --clean  # Wipe database and start fresh`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(clean)
		},
	}

	cmd.Flags().BoolVar(&clean, "clean", false, "Wipe the database volume before starting")
	return cmd
}

func runStart(clean bool) error {
	// Ensure .env exists.
	ensureEnvFile()

	// Stop any running containers first.
	fmt.Println("⏹️  Stopping existing containers...")
	_ = shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "down", "--remove-orphans")

	// If --clean, remove the Postgres volume.
	if clean {
		fmt.Println("🗑️  Removing database volume...")
		_ = shellCmd("docker", "compose", "down", "-v")
	}

	// Start Postgres.
	fmt.Println("🐘 Starting PostgreSQL...")
	if err := shellCmd("docker", "compose", "--profile", "database", "up", "-d"); err != nil {
		return fmt.Errorf("failed to start Postgres: %w", err)
	}

	// Wait for Postgres to be ready.
	fmt.Print("   Waiting for Postgres")
	for i := 0; i < 30; i++ {
		if silentCmd("docker", "compose", "exec", "-T", "postgresdb", "pg_isready", "-h", "postgresdb") == nil {
			fmt.Println(" ✅")
			break
		}
		fmt.Print(".")
		if i == 29 {
			fmt.Println(" ❌")
			return fmt.Errorf("Postgres did not become ready in 30 seconds")
		}
	}

	// Run migrations.
	fmt.Println("🔄 Running migrations...")
	if err := shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "build", "migrations"); err != nil {
		return fmt.Errorf("migration build failed: %w", err)
	}
	if err := shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "run", "--rm", "migrations"); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}

	// Start the orchestrator.
	fmt.Println("🚀 Starting orchestrator...")
	if err := shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "up", "-d", "--build", "--remove-orphans"); err != nil {
		return fmt.Errorf("failed to start orchestrator: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ Stack is running!")
	fmt.Println("   API: http://localhost:7171/v1/health")
	fmt.Println("   Stop: crawbl dev stop")
	return nil
}

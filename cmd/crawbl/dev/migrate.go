package dev

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations against local Postgres",
		Long:  "Build and run the migration container against the local PostgreSQL instance used for development.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ensureEnvFile()
			out.Step(style.Migrate, "Running database migrations...")
			if err := shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "build", "migrations"); err != nil {
				return err
			}
			return shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "run", "--rm", "migrations")
		},
	}
}

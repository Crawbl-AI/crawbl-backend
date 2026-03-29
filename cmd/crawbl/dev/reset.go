package dev

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Stop the stack and wipe all local data",
		Long:  "Stop the local stack, remove the Postgres data volume, and clear local state so you can start fresh.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out.Step(style.Stopping, "Stopping the local development stack...")
			_ = shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "down", "--remove-orphans")
			out.Step(style.Delete, "Removing the database volume...")
			_ = shellCmd("docker", "volume", "rm", "-f", "crawbl-backend_db-data")
			out.Success("Reset complete")
			out.Step(style.Tip, "Run 'crawbl dev start' to recreate the stack")
			return nil
		},
	}
}

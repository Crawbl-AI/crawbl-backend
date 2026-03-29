package dev

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Stop containers and wipe the database",
		Long:  "Stops all containers, removes the Postgres data volume, and clears all local state. Run 'crawbl dev start' to recreate everything.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("⏹️  Stopping containers...")
			_ = shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "down", "--remove-orphans")
			fmt.Println("🗑️  Removing database volume...")
			_ = shellCmd("docker", "volume", "rm", "-f", "crawbl-backend_db-data")
			fmt.Println("✅ Reset complete. Run 'crawbl dev start' to recreate.")
			return nil
		},
	}
}

package dev

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ensureEnvFile()
			fmt.Println("🔄 Running migrations...")
			if err := shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "build", "migrations"); err != nil {
				return err
			}
			return shellCmd("docker", "compose", "--profile", "database", "--profile", "migration", "run", "--rm", "migrations")
		},
	}
}

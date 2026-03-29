// Package dev provides local development commands that replace the Makefile.
// All commands assume you're in the crawbl-backend root directory.
package dev

import (
	"github.com/spf13/cobra"
)

// NewDevCommand creates the `crawbl dev` command group.
func NewDevCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Local development commands",
		Long:  "Start, stop, and manage the local development environment. Replaces the old Makefile targets.",
	}

	cmd.AddCommand(
		newStartCommand(),
		newStopCommand(),
		newResetCommand(),
		newMigrateCommand(),
		newFmtCommand(),
		newLintCommand(),
		newVerifyCommand(),
	)

	return cmd
}
